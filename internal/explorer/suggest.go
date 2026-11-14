package explorer

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// The ✧ commit message is herdr-sidebar's (`suggest.rs`): the local `claude`
// CLI is asked to summarise the diff in one line, and when it is missing,
// slow or fails, a line naming the files stands in. It drafts; the message
// goes into the commit box for the user to read before enter commits it.

// maxSuggestDiff caps the diff sent: a huge one only slows the answer, and
// the file list already names everything that changed.
const maxSuggestDiff = 16 * 1024

// suggestTimeout is how long claude is given before the fallback is used.
const suggestTimeout = 60 * time.Second

const suggestPrompt = "Write a git commit message for the diff on stdin: one imperative " +
	"subject line under 72 characters, no quotes, no trailing period. " +
	"Reply with ONLY the message line."

// suggestCommand is the program asked, a variable so a test can name a
// stand-in.
var suggestCommand = "claude"

// DiffForMessage is what to describe: the staged change, or when nothing is
// staged the unstaged one, and the files it touches — the untracked ones
// when nothing tracked changed. herdr-sidebar's diff_for_message.
func (g Git) DiffForMessage() (diff string, files []string, err error) {
	if g.JJ {
		// jj has one thing to describe: the working-copy commit.
		if diff, err = g.jjSuggestInput(); err != nil {
			return "", nil, err
		}
		st, err := g.jjStatus()
		if err != nil {
			return "", nil, err
		}
		for _, c := range st.Changes {
			files = append(files, c.Path)
		}
		return diff, files, nil
	}
	staged, err := g.run("diff", "--cached", "--stat", "--patch")
	if err != nil {
		return "", nil, err
	}
	names := []string{"diff", "--cached", "--name-only"}
	diff = string(staged)
	if strings.TrimSpace(diff) == "" {
		unstaged, err := g.run("diff", "--stat", "--patch")
		if err != nil {
			return "", nil, err
		}
		diff, names = string(unstaged), []string{"diff", "--name-only"}
	}
	out, err := g.run(names...)
	if err != nil {
		return "", nil, err
	}
	files = nonEmptyLines(string(out))
	if len(files) == 0 {
		others, err := g.run("ls-files", "--others", "--exclude-standard")
		if err != nil {
			return "", nil, err
		}
		files = nonEmptyLines(string(others))
	}
	return diff, files, nil
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// SuggestMessage drafts a commit message for a diff.
func SuggestMessage(ctx context.Context, diff string, files []string) string {
	if msg, ok := askClaude(ctx, diff); ok {
		return msg
	}
	return fallbackMessage(files)
}

func askClaude(ctx context.Context, diff string) (string, bool) {
	if len(diff) > maxSuggestDiff {
		cut := maxSuggestDiff
		for cut > 0 && !isRuneStart(diff[cut]) {
			cut--
		}
		diff = diff[:cut] + "\n[diff truncated]"
	}
	ctx, cancel := context.WithTimeout(ctx, suggestTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, suggestCommand, "-p", "--model", "haiku", "--strict-mcp-config", suggestPrompt)
	cmd.Stdin = strings.NewReader(diff)
	cmd.Env = withoutSessionEnv(os.Environ())
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", false
	}
	return cleanReply(out.String())
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

// withoutSessionEnv drops what tells a program it is in a tend pane. claude
// run to draft a message is not an agent in the panel's pane, and with
// tend's hooks installed it would report itself as one, turning the files
// panel into a "claude" in the agent list.
func withoutSessionEnv(env []string) []string {
	out := env[:0:0]
	for _, kv := range env {
		if !strings.HasPrefix(kv, "TEND_") {
			out = append(out, kv)
		}
	}
	return out
}

// cleanReply is the reply's line, stripped of the quotes and fences models
// add despite being told not to. Start-up noise can come before it, so the
// last usable line is taken and warning-looking ones are skipped.
func cleanReply(raw string) (string, bool) {
	lines := strings.Split(raw, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		lower := strings.ToLower(l)
		if l == "" || strings.HasPrefix(l, "```") || strings.Contains(lower, "warn") || strings.Contains(lower, "error") {
			continue
		}
		l = strings.TrimSpace(strings.TrimRight(strings.Trim(l, "\"'`"), "."))
		if l != "" {
			return l, true
		}
	}
	return "", false
}

// fallbackMessage names the files: enough to save retyping, honest about
// what it knows.
func fallbackMessage(files []string) string {
	switch len(files) {
	case 0:
		return "Update"
	case 1:
		return "Update " + filepath.Base(files[0])
	}
	return "Update " + filepath.Base(files[0]) + " and " + strconv.Itoa(len(files)-1) + " more"
}

// startSuggest drafts a commit message in the background and puts it in the
// commit box.
func (m *Model) startSuggest() {
	if m.git.Top == "" {
		return
	}
	if m.jobRunning || m.job != nil {
		m.say("busy; try again in a moment", false)
		return
	}
	diff, files, err := m.git.DiffForMessage()
	if err != nil {
		m.fail(err)
		return
	}
	if strings.TrimSpace(diff) == "" && len(files) == 0 {
		m.say("no changes to describe", true)
		return
	}
	m.job = func() func(*Model) {
		msg := SuggestMessage(context.Background(), diff, files)
		return func(m *Model) {
			m.mode, m.input = modeCommit, msg
			m.say("", false)
		}
	}
	m.say("✧ drafting a commit message…", false)
}
