package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/auth-com-br/tend/internal/tools"
	"github.com/auth-com-br/tend/internal/transport"
	"golang.org/x/term"
)

// Getting tend onto the other machine: herdr's remote attach
// (`remote/attach.rs` prepare_remote_herdr, ensure_remote_server_ready). A
// remote session needs tend there, and the same build as here, or the two
// ends disagree about the protocol. So before attaching, the far side is
// asked what it has; a tend of this very build is used wherever it is, and
// if there is none this binary is copied to ~/.local/bin/tend there, when the
// machines are the same kind, after asking. A server already running there
// is then handed to the new build, keeping its programs.

// remoteInstallSuffix is where tend puts itself on another machine, herdr's
// place for its own.
const remoteInstallSuffix = ".local/bin/tend"

// remoteProbe is the far side's answer: what machine it is, and each tend
// it has, with the build each one says it is.
type remoteProbe struct {
	Platform string
	Tends    []remoteTend
	// Gzip says the far side can unpack gzip, which halves what a slow link
	// has to carry.
	Gzip bool
}

type remoteTend struct {
	Path, Version string
}

// probeScript asks the far side for both in one round trip.
const probeScript = `uname -sm
command -v gzip >/dev/null 2>&1 && echo gzip
for c in "$(command -v tend 2>/dev/null)" "$HOME/` + remoteInstallSuffix + `"; do
  if [ -n "$c" ] && [ -x "$c" ]; then printf 'tend\t%s\t%s\n' "$c" "$("$c" version 2>/dev/null)"; fi
done`

// parseProbe reads the probe's output.
func parseProbe(out string) remoteProbe {
	var p remoteProbe
	sc := bufio.NewScanner(strings.NewReader(out))
	first := true
	seen := map[string]bool{}
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if first {
			p.Platform, first = line, false
			continue
		}
		if line == "gzip" {
			p.Gzip = true
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 3 || fields[0] != "tend" || seen[fields[1]] {
			continue
		}
		seen[fields[1]] = true
		p.Tends = append(p.Tends, remoteTend{Path: fields[1], Version: strings.TrimPrefix(fields[2], "tend ")})
	}
	return p
}

// localPlatform is this machine as uname -sm says it, to compare with the far
// side's: a binary can be copied only to the same kind of machine.
func localPlatform() string {
	osName := map[string]string{"linux": "Linux", "darwin": "Darwin", "freebsd": "FreeBSD"}[runtime.GOOS]
	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64", "386": "i686"}[runtime.GOARCH]
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		arch = "arm64" // what macOS's uname says
	}
	return osName + " " + arch
}

// remotePlan is what to do about the far side.
type remotePlan struct {
	use     string // the tend to run there, or "" to install
	install bool
	warning string
}

// planRemote decides, from the probe, what to run there.
func planRemote(p remoteProbe, build, here string) (remotePlan, error) {
	for _, t := range p.Tends {
		if t.Version == build {
			return remotePlan{use: t.Path}, nil
		}
	}
	if p.Platform == here {
		return remotePlan{install: true}, nil
	}
	if len(p.Tends) > 0 {
		t := p.Tends[0]
		return remotePlan{use: t.Path, warning: fmt.Sprintf(
			"the far side has tend %s at %s and this is %s; this build cannot be copied to a %s machine, so it is used as it is",
			t.Version, t.Path, build, p.Platform)}, nil
	}
	return remotePlan{}, fmt.Errorf("tend is not installed there, and this build (%s) cannot be copied to a %s machine; install tend there first", here, p.Platform)
}

// prepareRemote makes sure host has this build of tend, and points the
// bridge at it. It only runs over plain ssh: TEND_SSH is a wrapper whose
// shell this cannot assume.
func prepareRemote(host, session string) error {
	if os.Getenv(transport.RemoteCommandEnv) != "" {
		return nil
	}
	out, err := transport.RemoteShell(host, probeScript, nil)
	if err != nil {
		return err
	}
	probe := parseProbe(string(out))
	plan, err := planRemote(probe, version, localPlatform())
	if err != nil {
		return err
	}
	if plan.warning != "" {
		fmt.Fprintf(os.Stderr, "%s %s\n", tag(), plan.warning)
	}
	if !plan.install {
		transport.RemoteTend = shellQuote(plan.use)
		return nil
	}

	// herdr's question and default: yes unless told no, and no means no
	// session rather than one on a build that does not match.
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("tend %s is not installed on %s; run from an interactive terminal to approve installing it", version, host)
	}
	fmt.Fprintf(os.Stderr, "%s tend %s is not installed on %s (%s).\n", tag(), version, host, localPlatform())
	fmt.Fprintf(os.Stderr, "%s install this binary to ~/%s there? [Y/n] ", tag(), remoteInstallSuffix)
	var answer string
	_, _ = fmt.Fscanln(os.Stdin, &answer)
	if a := strings.ToLower(strings.TrimSpace(answer)); a != "" && a != "y" && a != "yes" {
		return fmt.Errorf("remote tend installation cancelled")
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(self)
	if err != nil {
		return err
	}
	// Run once under its temporary name before it replaces anything: a
	// binary that does not run there — built against a newer libc than the
	// far side has, say — must not take the place of one that does.
	unpack := "cat"
	payload := data
	if probe.Gzip {
		var z bytes.Buffer
		w, _ := gzip.NewWriterLevel(&z, gzip.BestCompression)
		_, _ = w.Write(data)
		_ = w.Close()
		payload, unpack = z.Bytes(), "gzip -dc"
	}
	// Said before and counted during: over a slow link this takes a minute,
	// and a terminal saying nothing for a minute looks hung.
	fmt.Fprintf(os.Stderr, "%s copying %s to %s\n", tag(), sizeLabel(len(payload)), host)
	progress := &countingReader{r: bytes.NewReader(payload), total: len(payload)}
	stop := progress.report()
	defer stop()
	script := `set -eu
d="$HOME/.local/bin"; mkdir -p "$d"
t="$d/tend.tmp.$$"; ` + unpack + ` > "$t"; chmod 755 "$t"
if [ "$("$t" version 2>/dev/null)" != "tend ` + version + `" ]; then
  rm -f "$t"; echo "this build does not run on this machine" >&2; exit 1
fi
mv "$t" "$d/tend"
printf '%s' "$d/tend"`
	path, err := transport.RemoteShell(host, script, progress)
	stop()
	if err != nil {
		return fmt.Errorf("copying tend to %s: %w", host, err)
	}
	transport.RemoteTend = shellQuote(string(path))
	fmt.Fprintf(os.Stderr, "%s installed tend %s at %s:%s\n", tag(), version, host, path)

	// A server already running there is of another build. Handed to this
	// one, as herdr hands off an outdated remote server, it keeps its
	// programs; one too old to hand off is left, and the session says so.
	if out, err := transport.RemoteShell(host, transport.RemoteTend+" handoff -s "+shellQuote(session)+" 2>&1 || true", nil); err == nil {
		if msg := strings.TrimSpace(string(out)); msg != "" && !strings.Contains(msg, "no session") {
			fmt.Fprintf(os.Stderr, "%s %s\n", tag(), msg)
		}
	}
	return nil
}

// shellQuote quotes a word for the far side's shell.
var shellQuote = tools.ShellQuote

// countingReader counts what has been sent and says so every second, on one
// line that it rewrites.
type countingReader struct {
	r     io.Reader
	total int
	sent  atomic.Int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.sent.Add(int64(n))
	return n, err
}

// report prints progress until the returned function is called, which is
// safe to call twice.
func (c *countingReader) report() func() {
	done := make(chan struct{})
	var once sync.Once
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-done:
				fmt.Fprint(os.Stderr, "\r\x1b[K")
				return
			case <-t.C:
				sent := c.sent.Load()
				fmt.Fprintf(os.Stderr, "\r\x1b[K%s %s of %s (%d%%)", tag(),
					sizeLabel(int(sent)), sizeLabel(c.total), sent*100/int64(max(c.total, 1)))
			}
		}
	}()
	return func() { once.Do(func() { close(done) }) }
}

func sizeLabel(n int) string {
	return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
}
