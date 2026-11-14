package policy

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// The guard is tend's answer to agents run without asking (Claude Code's
// --dangerously-skip-permissions, Codex's full-auto) deleting what they
// should not: before an agent runs a shell command, its PreToolUse hook asks
// tend (`tend guard`), and a command that destroys something outside the
// work — rm -r outside the project, a force push, a dropped database, a
// Docker volume — is refused or put to the person, as [policy] guard says.
// herdr has nothing like it (issue #58).
//
// It reads a command the way a shell splits it — quotes, `&&`, `;`, `|`,
// a `sudo` or `VAR=x` in front — and judges each simple command. It is a
// net for the common accident, not a sandbox: a command built at run time
// (`eval`, a script the agent wrote) is not seen through, and that is said
// where the guard is described.

// Guard modes.
const (
	GuardOff  = "off"
	GuardAsk  = "ask"
	GuardDeny = "deny"
)

// Guard rules, as the activity record names them.
const (
	RuleRmOutside   = "guard_rm_outside"
	RuleForcePush   = "guard_force_push"
	RuleDiscardWork = "guard_discard_work"
	RuleDatabase    = "guard_database"
	RuleDocker      = "guard_docker"
	RuleDisk        = "guard_disk"
	RuleCustom      = "guard_custom"
)

// Guard is [policy]'s guard settings.
type Guard struct {
	// Mode is off (the default: tend never stopped a command before and
	// does not start without being asked), ask (the agent asks the person)
	// or deny (the agent is told no, and why).
	Mode string `toml:"guard"`
	// Deny are regular expressions of commands that are refused as well;
	// Allow, of commands let through whatever the rules say.
	Deny  []string `toml:"guard_deny"`
	Allow []string `toml:"guard_allow"`
}

// Check validates a guard's settings.
func (g Guard) Check() error {
	switch g.Mode {
	case "", GuardOff, GuardAsk, GuardDeny:
	default:
		return fmt.Errorf("policy.guard is %q; use \"off\", \"ask\" or \"deny\"", g.Mode)
	}
	for _, list := range [][]string{g.Deny, g.Allow} {
		for _, p := range list {
			if _, err := regexp.Compile(p); err != nil {
				return fmt.Errorf("policy guard pattern %q: %w", p, err)
			}
		}
	}
	return nil
}

// On is whether the guard judges anything.
func (g Guard) On() bool { return g.Mode == GuardAsk || g.Mode == GuardDeny }

// Danger is one thing a command would destroy.
type Danger struct {
	Rule    string
	Message string
}

// Where is what a command is judged against: the project it runs in (the
// repository's top, else the directory), where it runs, and home.
type Where struct {
	Project, Cwd, Home string
	// Temp are directories anything may be deleted under.
	Temp []string
}

// Judge says what a command line would destroy. None is nothing found.
func (g Guard) Judge(line string, w Where) []Danger {
	for _, p := range g.Allow {
		if re, err := regexp.Compile(p); err == nil && re.MatchString(line) {
			return nil
		}
	}
	var out []Danger
	for _, p := range g.Deny {
		if re, err := regexp.Compile(p); err == nil && re.MatchString(line) {
			out = append(out, Danger{RuleCustom, "matches your guard_deny pattern " + p})
		}
	}
	// What a database is told is often in a string or a heredoc, which the
	// words do not show; it is looked for in the whole line.
	if m := sqlDestroy.FindString(line); m != "" {
		out = append(out, Danger{RuleDatabase, "it would destroy database data (" + strings.ToUpper(strings.Join(strings.Fields(m), " ")) + ")"})
	}
	return append(out, judgeLine(line, w, 0)...)
}

// judgeLine judges each simple command of a line, and the script a shell
// is handed (bash -c "…") as a line of its own.
func judgeLine(line string, w Where, depth int) []Danger {
	var out []Danger
	for _, words := range simpleCommands(line) {
		if script, ok := shellScript(program(words)); ok && depth < 4 {
			out = append(out, judgeLine(script, w, depth+1)...)
			continue
		}
		if d, ok := judgeWords(words, w); ok {
			out = append(out, d)
		}
	}
	return out
}

// shellScript is the script of `sh -c script` and its kin.
func shellScript(words []string) (string, bool) {
	if len(words) < 3 {
		return "", false
	}
	switch filepath.Base(words[0]) {
	case "sh", "bash", "zsh", "dash", "ksh", "fish":
	default:
		return "", false
	}
	for i, a := range words[1:] {
		if strings.HasPrefix(a, "-") && strings.Contains(a, "c") && i+2 < len(words) {
			return words[i+2], true
		}
	}
	return "", false
}

var sqlDestroy = regexp.MustCompile(`(?i)\b(drop\s+(database|schema|table)|truncate\s+(table\s+)?\w|flushall|flushdb|dropdatabase\s*\()`)

// simpleCommands splits a line into its commands, each as its words, the way
// a shell does: quotes kept together, and ; && || | & and newlines between.
// A `$( … )` is read as words of the command around it, which is enough to
// see an rm inside one.
func simpleCommands(line string) [][]string {
	var (
		out   [][]string
		words []string
		cur   strings.Builder
		in    bool // a word has begun
		quote byte
	)
	endWord := func() {
		if in {
			words = append(words, cur.String())
			cur.Reset()
			in = false
		}
	}
	endCommand := func() {
		endWord()
		if len(words) > 0 {
			out = append(out, words)
		}
		words = nil
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			} else if c == '\\' && quote == '"' && i+1 < len(line) {
				i++
				cur.WriteByte(line[i])
			} else {
				cur.WriteByte(c)
			}
		case c == '\'' || c == '"':
			quote, in = c, true
		case c == '\\' && i+1 < len(line):
			i++
			if line[i] != '\n' {
				cur.WriteByte(line[i])
				in = true
			}
		case c == ' ' || c == '\t':
			endWord()
		case c == ';' || c == '\n' || c == '&' || c == '|' || c == '(' || c == ')' || c == '`':
			endCommand()
		case c == '$' && i+1 < len(line) && line[i+1] == '(':
			endCommand()
			i++
		default:
			cur.WriteByte(c)
			in = true
		}
	}
	endCommand()
	return out
}

// program strips what runs a command rather than being it — sudo, env,
// VAR=x, command, nice, time, nohup, xargs — and returns the rest.
func program(words []string) []string {
	for len(words) > 0 {
		w := words[0]
		switch {
		case strings.Contains(w, "=") && !strings.HasPrefix(w, "-") && !strings.HasPrefix(w, "="):
			words = words[1:]
		case w == "sudo" || w == "doas" || w == "env" || w == "command" || w == "nice" ||
			w == "time" || w == "nohup" || w == "exec" || w == "xargs":
			words = words[1:]
			// Their own flags (sudo -u root, xargs -0, nice -n 5).
			for len(words) > 0 && strings.HasPrefix(words[0], "-") {
				flag := words[0]
				words = words[1:]
				if (flag == "-u" || flag == "-n" || flag == "-g") && len(words) > 0 {
					words = words[1:]
				}
			}
		default:
			return words
		}
	}
	return nil
}

func judgeWords(all []string, w Where) (Danger, bool) {
	words := program(all)
	if len(words) == 0 {
		return Danger{}, false
	}
	name := filepath.Base(words[0])
	args := words[1:]
	switch name {
	case "rm":
		return judgeRm(args, w)
	case "git":
		return judgeGit(args)
	case "docker", "podman":
		return judgeDocker(args)
	case "dropdb":
		return Danger{RuleDatabase, "dropdb deletes a database"}, true
	case "mkfs", "wipefs", "shred", "fdisk", "parted", "sgdisk":
		return Danger{RuleDisk, name + " destroys what is on a disk"}, true
	case "dd":
		for _, a := range args {
			if strings.HasPrefix(a, "of=/dev/") && !strings.HasPrefix(a, "of=/dev/null") {
				return Danger{RuleDisk, "dd would write over a device (" + a + ")"}, true
			}
		}
	case "chmod", "chown":
		if hasFlag(args, "R", "recursive") {
			for _, t := range targets(args) {
				if p := resolve(t, w); isRootish(p, w) {
					return Danger{RuleRmOutside, fmt.Sprintf("%s -R on %s changes everything under it", name, t)}, true
				}
			}
		}
	}
	if strings.HasPrefix(name, "mkfs.") {
		return Danger{RuleDisk, name + " destroys what is on a disk"}, true
	}
	return Danger{}, false
}

// hasFlag is whether args carry a short flag (in any cluster: -rf) or its
// long form.
func hasFlag(args []string, short, long string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == "--"+long {
			return true
		}
		if strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.Contains(a[1:], short) {
			return true
		}
	}
	return false
}

// targets are the arguments that are not flags.
func targets(args []string) []string {
	var out []string
	flags := true
	for _, a := range args {
		if flags && a == "--" {
			flags = false
			continue
		}
		if flags && strings.HasPrefix(a, "-") && a != "-" {
			continue
		}
		out = append(out, a)
	}
	return out
}

// resolve makes a target absolute: ~ and $HOME are home, a relative one is
// under where the command runs.
func resolve(t string, w Where) string {
	switch {
	case t == "~" || t == "$HOME" || t == "${HOME}":
		return w.Home
	case strings.HasPrefix(t, "~/"):
		return filepath.Join(w.Home, t[2:])
	case strings.HasPrefix(t, "$HOME/"):
		return filepath.Join(w.Home, t[6:])
	case strings.HasPrefix(t, "${HOME}/"):
		return filepath.Join(w.Home, t[8:])
	case filepath.IsAbs(t):
		return filepath.Clean(t)
	}
	return filepath.Join(w.Cwd, t)
}

// isRootish is a directory whose loss is everything: /, home, or one of the
// system's own top directories.
func isRootish(p string, w Where) bool {
	p = strings.TrimSuffix(filepath.Clean(strings.TrimSuffix(p, "/*")), "/")
	if p == "" || p == "/" || p == filepath.Clean(w.Home) {
		return true
	}
	return filepath.Dir(p) == "/"
}

func within(p, dir string) bool {
	if dir == "" {
		return false
	}
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

func judgeRm(args []string, w Where) (Danger, bool) {
	if !hasFlag(args, "r", "recursive") && !hasFlag(args, "R", "recursive") {
		// Single files are what an agent deletes all day, and git has them.
		return Danger{}, false
	}
	for _, t := range targets(args) {
		p := resolve(t, w)
		if isRootish(p, w) {
			return Danger{RuleRmOutside, fmt.Sprintf("rm -r %s deletes %s", t, describe(p, w))}, true
		}
		if within(p, w.Project) && filepath.Clean(p) != filepath.Clean(w.Project) {
			continue
		}
		temp := false
		for _, d := range w.Temp {
			temp = temp || within(p, d)
		}
		if temp {
			continue
		}
		if filepath.Clean(p) == filepath.Clean(w.Project) {
			return Danger{RuleRmOutside, fmt.Sprintf("rm -r %s deletes the whole project", t)}, true
		}
		return Danger{RuleRmOutside, fmt.Sprintf("rm -r %s deletes %s, outside the project (%s)", t, p, w.Project)}, true
	}
	return Danger{}, false
}

func describe(p string, w Where) string {
	switch filepath.Clean(p) {
	case "/":
		return "everything"
	case filepath.Clean(w.Home):
		return "your home directory"
	}
	return p
}

func judgeGit(args []string) (Danger, bool) {
	// git's own flags before the subcommand: -C dir, -c key=value.
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		if (args[0] == "-C" || args[0] == "-c") && len(args) > 1 {
			args = args[1:]
		}
		args = args[1:]
	}
	if len(args) == 0 {
		return Danger{}, false
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "push":
		for _, a := range rest {
			switch {
			case a == "--force" || a == "-f" || (strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.Contains(a, "f")):
				return Danger{RuleForcePush, "a force push rewrites the remote's history (--force-with-lease is let through)"}, true
			case a == "--mirror":
				return Danger{RuleForcePush, "push --mirror makes the remote match here, deleting what it has and this does not"}, true
			case a == "--delete" || a == "-d":
				return Danger{RuleForcePush, "push --delete deletes a branch on the remote"}, true
			case strings.HasPrefix(a, "+"):
				return Danger{RuleForcePush, "a +refspec is a force push of " + a[1:]}, true
			case strings.HasPrefix(a, ":") && len(a) > 1:
				return Danger{RuleForcePush, "pushing " + a + " deletes " + a[1:] + " on the remote"}, true
			}
		}
	case "reset":
		if contains(rest, "--hard") {
			return Danger{RuleDiscardWork, "git reset --hard throws away uncommitted work"}, true
		}
	case "clean":
		if hasFlag(rest, "f", "force") {
			return Danger{RuleDiscardWork, "git clean -f deletes files git does not track, which no commit has"}, true
		}
	case "checkout", "restore":
		for _, t := range targets(rest) {
			if t == "." && (sub == "restore" || contains(rest, "--")) {
				return Danger{RuleDiscardWork, "git " + sub + " . throws away every uncommitted change"}, true
			}
		}
	case "branch":
		if contains(rest, "-D") || (hasFlag(rest, "d", "delete") && hasFlag(rest, "f", "force")) {
			return Danger{RuleDiscardWork, "git branch -D deletes a branch whether or not it was merged"}, true
		}
	case "stash":
		if len(rest) > 0 && (rest[0] == "clear" || rest[0] == "drop") {
			return Danger{RuleDiscardWork, "git stash " + rest[0] + " throws stashed work away"}, true
		}
	}
	return Danger{}, false
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func judgeDocker(args []string) (Danger, bool) {
	t := targets(args)
	join := strings.Join(t, " ")
	switch {
	case strings.HasPrefix(join, "volume rm") || strings.HasPrefix(join, "volume prune"):
		return Danger{RuleDocker, "docker " + strings.Join(t[:2], " ") + " deletes the data a container keeps — a database's, often"}, true
	case strings.HasPrefix(join, "system prune"):
		return Danger{RuleDocker, "docker system prune deletes stopped containers, and with --volumes their data"}, true
	case (strings.HasPrefix(join, "compose down") || strings.HasPrefix(join, "compose rm")) && (contains(args, "-v") || contains(args, "--volumes")):
		return Danger{RuleDocker, "docker compose down -v deletes the project's volumes and what is in them"}, true
	case (len(t) > 0 && (t[0] == "rm" || t[0] == "rmi")) || strings.HasPrefix(join, "container rm"):
		if contains(args, "-f") || contains(args, "--force") || contains(args, "-v") {
			return Danger{RuleDocker, "docker rm -f removes a running container"}, true
		}
	}
	return Danger{}, false
}
