package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/auth-com-br/tend/internal/activity"
	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/github"
	"github.com/auth-com-br/tend/internal/server"
	"github.com/auth-com-br/tend/internal/transport"
)

// `tend activity`: what agents did, from the record the session servers keep
// (internal/activity). It reads the file itself and needs no server, so a
// report can be made on a machine whose sessions are all stopped, or by a
// script with no terminal.
func runActivity(args []string) error {
	fs := flag.NewFlagSet("activity", flag.ExitOnError)
	project := fs.String("project", "", "only this project: a directory in it (\".\" for the one you are in)")
	agentName := fs.String("agent", "", "only this agent (claude, codex, …)")
	since := fs.String("since", "", "only what ran since then: 7d, 12h, or a date as 2006-01-02")
	by := fs.String("by", "run", "one line per run, or a total per project or agent: run, project, agent")
	format := fs.String("format", "table", "table, csv or json")
	violations := fs.Bool("violations", false, "list the agents that ran against a policy, and the ones tend refused to start")
	prs := fs.Bool("prs", false, "look up the pull request each branch became (gh, or GitLab's API)")
	out := fs.String("o", "", "write to this file rather than to the terminal")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(),
			"usage: tend activity [options]\n\n"+
				"what agents did: when each started and stopped, in which project and on\n"+
				"which branch, what it was working on, which files changed and what its\n"+
				"conversation spent. costs are estimates from the models' public API\n"+
				"prices; [activity.prices] in the settings file replaces them.\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(hoistFlags(args, map[string]bool{
		"project": true, "agent": true, "since": true, "by": true, "format": true,
		"violations": false, "prs": false, "o": true,
	})); err != nil {
		return err
	}
	switch *format {
	case "table", "csv", "json":
	default:
		return fmt.Errorf("-format is table, csv or json, not %q", *format)
	}
	switch *by {
	case "run", "project", "agent":
	default:
		return fmt.Errorf("-by is run, project or agent, not %q", *by)
	}

	now := time.Now()
	filter := activity.Filter{Agent: *agentName}
	if *since != "" {
		t, err := parseSince(*since, now)
		if err != nil {
			return err
		}
		filter.Since = t
	}
	if *project != "" {
		dir, err := filepath.Abs(*project)
		if err != nil {
			return err
		}
		// A directory anywhere in the repository names the whole project,
		// its worktrees included.
		if p := activity.Inspect(dir); p.Project != "" {
			dir = p.Project
		}
		filter.Project = dir
	}

	stateDir, err := transport.StateDir()
	if err != nil {
		return err
	}
	recs, err := activity.Read(activity.Path(stateDir))
	if err != nil {
		return err
	}
	all, refused := activity.Fold(recs)

	var w io.Writer = os.Stdout
	if *out != "" {
		f, err := os.OpenFile(*out, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		defer f.Close()
		w = f
	}

	if *violations {
		return writeViolations(w, *format, all, refused, filter)
	}

	var runs []*activity.Run
	for _, r := range all {
		if filter.Keep(r) {
			runs = append(runs, r)
		}
	}
	activity.Measure(runs, server.DefaultTranscripts)
	cfg, _ := config.LoadLenient()
	activity.Price(runs, cfg.Activity.Prices)
	if *prs {
		lookUpPRs(runs)
	}

	if *by != "run" {
		key := activity.ByProject
		if *by == "agent" {
			key = activity.ByAgent
		}
		totals := activity.Summarize(runs, key, now)
		switch *format {
		case "csv":
			return activity.WriteTotalsCSV(w, *by, totals)
		case "json":
			return activity.WriteJSON(w, map[string]any{"by": *by, "totals": totals})
		}
		if len(totals) == 0 {
			return emptyActivity(w)
		}
		return activity.WriteTotalsTable(w, *by, totals)
	}

	switch *format {
	case "csv":
		return activity.WriteRunsCSV(w, runs, now)
	case "json":
		if runs == nil {
			runs = []*activity.Run{}
		}
		return activity.WriteJSON(w, map[string]any{"runs": runs})
	}
	if len(runs) == 0 {
		return emptyActivity(w)
	}
	return activity.WriteRunsTable(w, runs, now)
}

// emptyActivity says there is nothing, and why there might be nothing.
func emptyActivity(w io.Writer) error {
	_, err := fmt.Fprintln(w, "no agent activity recorded yet. the session server writes it down as agents\n"+
		"start and stop ([activity] record in the settings file turns it off).")
	return err
}

// writeViolations lists the policy violations and refusals the filter keeps.
func writeViolations(w io.Writer, format string, runs []*activity.Run, refused []activity.Violation, f activity.Filter) error {
	var vs []activity.Violation
	for _, r := range runs {
		if f.Keep(r) {
			vs = append(vs, r.Violations...)
		}
	}
	for _, v := range refused {
		r := &activity.Run{Agent: v.Agent, Place: v.Place, Started: v.Time, Ended: v.Time}
		if f.Keep(r) {
			vs = append(vs, v)
		}
	}
	// In the order they happened: runs and refusals were gathered apart.
	sortViolations(vs)
	switch format {
	case "csv":
		return activity.WriteViolationsCSV(w, vs)
	case "json":
		if vs == nil {
			vs = []activity.Violation{}
		}
		return activity.WriteJSON(w, map[string]any{"violations": vs})
	}
	if len(vs) == 0 {
		_, err := fmt.Fprintln(w, "no agent has run against a policy, and none was refused.")
		return err
	}
	return activity.WriteViolationsTable(w, vs)
}

func sortViolations(vs []activity.Violation) {
	for i := 1; i < len(vs); i++ {
		for j := i; j > 0 && vs[j].Time.Before(vs[j-1].Time); j-- {
			vs[j], vs[j-1] = vs[j-1], vs[j]
		}
	}
}

// parseSince reads -since: a number of days or hours back, or a date.
func parseSince(s string, now time.Time) (time.Time, error) {
	if n, ok := strings.CutSuffix(s, "d"); ok {
		if days, err := strconv.Atoi(n); err == nil && days >= 0 {
			return now.AddDate(0, 0, -days), nil
		}
	}
	if d, err := time.ParseDuration(s); err == nil && d >= 0 {
		return now.Add(-d), nil
	}
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("-since is 7d, 12h or a date as 2006-01-02, not %q", s)
}

// lookUpPRs asks the forge what each run's branch became. One question per
// branch, not per run: an issue worked on in three sittings is one branch.
func lookUpPRs(runs []*activity.Run) {
	type key struct{ project, branch string }
	found := map[key]*activity.PR{}
	asked := map[key]bool{}
	for _, r := range runs {
		if r.Project == "" || r.Branch == "" || !r.Worktree && isMainBranch(r.Branch) {
			continue
		}
		k := key{r.Project, r.Branch}
		if !asked[k] {
			asked[k] = true
			found[k] = prFor(r.Project, r.Branch)
		}
		r.PR = found[k]
	}
}

// isMainBranch is a branch whose pull requests are not the run's: work done
// on main is not "what main became".
func isMainBranch(b string) bool { return b == "main" || b == "master" }

func prFor(project, branch string) *activity.PR {
	repos, err := github.ReposFor(project)
	if err != nil || len(repos) == 0 {
		return nil
	}
	prs, err := github.PRsForBranch(repos[0], branch)
	if err != nil || len(prs) == 0 {
		if err != nil && !errors.Is(err, github.ErrNoRepo) {
			fmt.Fprintf(os.Stderr, "tend: pull requests for %s: %v\n", branch, err)
		}
		return nil
	}
	return &activity.PR{Number: prs[0].Number, URL: prs[0].URL, State: prs[0].State}
}
