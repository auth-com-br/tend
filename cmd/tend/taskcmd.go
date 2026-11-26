package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/auth-com-br/tend/internal/api"
)

// `tend task`, the session's planner from a shell: the automation socket's
// task.* with a shell face, as `tend context` is context.*. A task given an
// agent and a time is started by the server at that time, attached or not.
func runTask(args []string) error {
	usage := func(w io.Writer) {
		fmt.Fprint(w,
			"usage: tend task <command> [options]\n\n"+
				"  list                     the tasks not done (-all: every one)\n"+
				"  add [flags] <title…>     a new task; -agent and -at start an agent for it\n"+
				"  show <id>                one task, every field\n"+
				"  done <id>                mark a task done\n"+
				"  rm <id>                  delete a task (its agent, if any, keeps running)\n\n"+
				"a time is 2026-11-27 09:00, 2026-11-27, 09:00 (the next one), +30m, or\n"+
				"RFC 3339.\n\n")
	}
	if len(args) == 0 {
		usage(os.Stderr)
		return errors.New("no command given")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list", "ls":
		fs := flag.NewFlagSet("task list", flag.ExitOnError)
		name := sessionFlag(fs)
		all := fs.Bool("all", false, "list the tasks done too")
		asJSON := fs.Bool("json", false, "print the tasks as JSON")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		out, err := apiCall(*name, api.MethodTaskList, nil, false)
		if err != nil {
			return err
		}
		tasks, _ := out["tasks"].([]any)
		if *asJSON {
			return printJSON(tasks)
		}
		w := newTable("ID", "STATUS", "DUE", "START", "AGENT", "TITLE")
		shown := 0
		for _, raw := range tasks {
			t, _ := raw.(map[string]any)
			if !*all && text(t["status"]) == "done" {
				continue
			}
			start := shortTime(text(t["start_at"]))
			if text(t["error"]) != "" {
				start += " (failed)"
			} else if text(t["pane_id"]) != "" {
				start += " → " + text(t["pane_id"])
			}
			w.row(fmt.Sprint(t["task_id"]), text(t["status"]), shortTime(text(t["due"])), start,
				text(t["agent"]), text(t["title"]))
			shown++
		}
		if shown == 0 {
			fmt.Fprintf(os.Stderr, "%s no tasks\n", tag())
			return nil
		}
		return w.flush()

	case "add":
		return runTaskAdd(rest)

	case "show", "done", "rm":
		fs := flag.NewFlagSet("task "+sub, flag.ExitOnError)
		name := sessionFlag(fs)
		if err := fs.Parse(hoistFlags(rest, map[string]bool{"s": true})); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: tend task %s <id>", sub)
		}
		id, err := strconv.ParseUint(strings.TrimPrefix(fs.Arg(0), "#"), 10, 64)
		if err != nil || id == 0 {
			return fmt.Errorf("%q is not a task id", fs.Arg(0))
		}
		switch sub {
		case "done":
			_, err := apiCall(*name, api.MethodTaskUpdate, map[string]any{"task_id": id, "status": "done"}, false)
			return err
		case "rm":
			_, err := apiCall(*name, api.MethodTaskDelete, map[string]any{"task_id": id}, false)
			return err
		}
		out, err := apiCall(*name, api.MethodTaskList, nil, false)
		if err != nil {
			return err
		}
		tasks, _ := out["tasks"].([]any)
		for _, raw := range tasks {
			if t, _ := raw.(map[string]any); fmt.Sprint(t["task_id"]) == strconv.FormatUint(id, 10) {
				return printJSON(t)
			}
		}
		return fmt.Errorf("task %d not found", id)

	case "-h", "--help", "help":
		usage(os.Stdout)
		return nil
	}
	usage(os.Stderr)
	return fmt.Errorf("unknown command %q", sub)
}

func runTaskAdd(args []string) error {
	fs := flag.NewFlagSet("task add", flag.ExitOnError)
	name := sessionFlag(fs)
	notes := fs.String("notes", "", "what the task is about")
	due := fs.String("due", "", "when it is to be done by")
	at := fs.String("at", "", "when to start its agent (needs -agent)")
	agentName := fs.String("agent", "", "the agent to start for it, as claude or codex")
	prompt := fs.String("prompt", "", "the first message for the agent")
	space := fs.String("space", "", "the space it belongs to and its agent starts in (w_3); none: a space of its own")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), "usage: tend task add [flags] <title…>\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(hoistFlags(args, map[string]bool{"s": true, "notes": true, "due": true,
		"at": true, "agent": true, "prompt": true, "space": true})); err != nil {
		return err
	}
	title := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if title == "" {
		return errors.New("usage: tend task add [flags] <title…>")
	}
	if *at != "" && *agentName == "" {
		// Accepted by the server, and nothing would start: say so here.
		return errors.New("-at starts an agent: name it with -agent")
	}
	params := map[string]any{"title": title}
	now := time.Now()
	for _, f := range []struct {
		key, value string
	}{{"due", *due}, {"start_at", *at}} {
		if f.value == "" {
			continue
		}
		t, err := parseWhen(f.value, now)
		if err != nil {
			return err
		}
		params[f.key] = t.Format(time.RFC3339)
	}
	for key, v := range map[string]string{"notes": *notes, "agent": *agentName, "prompt": *prompt, "workspace_id": *space} {
		if v != "" {
			params[key] = v
		}
	}
	out, err := apiCall(*name, api.MethodTaskCreate, params, false)
	if err != nil {
		return err
	}
	t, _ := out["task"].(map[string]any)
	line := fmt.Sprintf("%s added task %v: %s", tag(), t["task_id"], text(t["title"]))
	if s := text(t["start_at"]); s != "" {
		line += fmt.Sprintf(" (%s at %s)", text(t["agent"]), shortTime(s))
	}
	fmt.Fprintln(os.Stderr, line)
	return nil
}

// parseWhen reads a time the way a person types one: a date and a time, a
// date (its midnight), a time of day (the next one, so 09:00 typed at ten is
// tomorrow's), a duration from now (+30m), or RFC 3339. Local time
// throughout, which is the clock the person is reading.
func parseWhen(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if rest, ok := strings.CutPrefix(s, "+"); ok {
		d, err := time.ParseDuration(rest)
		if err != nil {
			return time.Time{}, fmt.Errorf("%q is not a duration (+30m, +2h)", s)
		}
		return now.Add(d), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	for _, layout := range []string{"2006-01-02 15:04", "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, now.Location()); err == nil {
			return t, nil
		}
	}
	if t, err := time.ParseInLocation("15:04", s, now.Location()); err == nil {
		day := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
		if !day.After(now) {
			day = day.AddDate(0, 0, 1)
		}
		return day, nil
	}
	return time.Time{}, fmt.Errorf("%q is not a time: try 2026-11-27 09:00, 09:00 or +30m", s)
}

// shortTime is an RFC 3339 stamp as a list shows it.
func shortTime(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return t.Local().Format("2006-01-02 15:04")
}
