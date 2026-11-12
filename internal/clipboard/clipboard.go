// Package clipboard puts text where the user's paste will find it.
//
// Which place that is depends on where the terminal is. Running locally, the
// clipboard belongs to the desktop tend is running on and a tool like wl-copy
// or xclip owns it. Over ssh it belongs to the machine in front of the user,
// which tend cannot reach at all — but the terminal there can, if it is
// willing to accept an escape sequence saying so.
//
// Guessing wrong is worse than either: a local tool over ssh copies to a
// desktop nobody is looking at and reports success.
package clipboard

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// ErrNoTool means there is nowhere to put it.
var ErrNoTool = errors.New("clipboard: no way to reach a clipboard")

// copyTimeout bounds a tool that hangs. wl-copy in particular stays alive to
// serve the selection, and waiting on it would hang the client.
const copyTimeout = 2 * time.Second

// Remote reports whether the terminal is on another machine, which decides
// whether a local tool would be copying to the right desktop.
func Remote() bool {
	return os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != ""
}

// tool is one way to reach the clipboard.
type tool struct {
	name string
	args []string
	// needs is an environment variable that must be set for the tool to have
	// a display to talk to. Empty means it always applies.
	needs string
}

// tools are tried in order, the most specific display server first.
func tools() []tool {
	if runtime.GOOS == "darwin" {
		return []tool{{name: "pbcopy"}}
	}
	return []tool{
		{name: "wl-copy", needs: "WAYLAND_DISPLAY"},
		{name: "xclip", args: []string{"-selection", "clipboard"}, needs: "DISPLAY"},
		{name: "xsel", args: []string{"--clipboard", "--input"}, needs: "DISPLAY"},
	}
}

// Copy writes text to the system clipboard and reports which tool took it.
//
// It does not fall back to the escape sequence: the caller sends that anyway,
// because a terminal that accepts it is the only thing that can be right in
// both places.
func Copy(text string) (string, error) {
	if text == "" {
		return "", nil
	}
	if Remote() {
		// The desktop this process can reach is not the one the user is
		// looking at. Saying so beats copying somewhere useless.
		return "", ErrNoTool
	}

	var firstErr error
	for _, t := range tools() {
		if t.needs != "" && os.Getenv(t.needs) == "" {
			continue
		}
		path, err := exec.LookPath(t.name)
		if err != nil {
			continue
		}
		if err := run(path, t.args, text); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		return t.name, nil
	}
	if firstErr != nil {
		return "", firstErr
	}
	return "", ErrNoTool
}

// primaryTools set the X11/Wayland primary selection — what a middle click
// pastes — which macOS does not have.
func primaryTools() []tool {
	if runtime.GOOS == "darwin" {
		return nil
	}
	return []tool{
		{name: "wl-copy", args: []string{"--primary"}, needs: "WAYLAND_DISPLAY"},
		{name: "xclip", args: []string{"-selection", "primary"}, needs: "DISPLAY"},
		{name: "xsel", args: []string{"--primary", "--input"}, needs: "DISPLAY"},
	}
}

// CopyPrimary writes text to the primary selection, as selecting text does
// in every other Linux program, and reports which tool took it. Like Copy,
// it does nothing useful over ssh, and says so.
func CopyPrimary(text string) (string, error) {
	if text == "" {
		return "", nil
	}
	if Remote() {
		return "", ErrNoTool
	}
	var firstErr error
	for _, t := range primaryTools() {
		if t.needs != "" && os.Getenv(t.needs) == "" {
			continue
		}
		path, err := exec.LookPath(t.name)
		if err != nil {
			continue
		}
		if err := run(path, t.args, text); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		return t.name, nil
	}
	if firstErr != nil {
		return "", firstErr
	}
	return "", ErrNoTool
}

// run feeds text to a tool and waits for it to take it.
//
// The pipe is closed before waiting, because that is how these tools know the
// input has ended, and some of them do not exit until it does.
func run(path string, args []string, text string) error {
	cmd := exec.Command(path, args...)
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Start(); err != nil {
		return err
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(copyTimeout):
		// wl-copy stays alive to serve the selection, which is correct for it
		// and would hang this. It has the text by now; letting it go is the
		// whole point of handing it over.
		return nil
	}
}

// readers are the tools that read the clipboard back, in the order Copy
// writes it, so a paste reads where the last copy went.
func readers() []tool {
	if runtime.GOOS == "darwin" {
		return []tool{{name: "pbpaste"}}
	}
	return []tool{
		{name: "wl-paste", args: []string{"--no-newline"}, needs: "WAYLAND_DISPLAY"},
		{name: "xclip", args: []string{"-selection", "clipboard", "-o"}, needs: "DISPLAY"},
		{name: "xsel", args: []string{"--clipboard", "--output"}, needs: "DISPLAY"},
	}
}

// Paste reads the system clipboard and reports which tool read it.
//
// Over ssh it is ErrNoTool, for the reason Copy is: the clipboard this
// process could read belongs to a desktop nobody is looking at, and the one
// in front of the user cannot be read from here — terminals take OSC 52
// writes, and almost none answer reads.
func Paste() (string, string, error) {
	if Remote() {
		return "", "", ErrNoTool
	}
	var firstErr error
	for _, t := range readers() {
		if t.needs != "" && os.Getenv(t.needs) == "" {
			continue
		}
		path, err := exec.LookPath(t.name)
		if err != nil {
			continue
		}
		cmd := exec.Command(path, t.args...)
		var out strings.Builder
		cmd.Stdout = &out
		if err := cmd.Start(); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case err := <-done:
			if err != nil {
				// An empty clipboard is an error to wl-paste and xclip; it
				// is "nothing to paste", which the caller says.
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			return out.String(), t.name, nil
		case <-time.After(copyTimeout):
			_ = cmd.Process.Kill()
			if firstErr == nil {
				firstErr = errors.New("clipboard: " + t.name + " did not answer")
			}
		}
	}
	if firstErr != nil {
		return "", "", firstErr
	}
	return "", "", ErrNoTool
}
