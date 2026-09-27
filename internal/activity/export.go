package activity

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

// The three ways out of the record: a table for a person, CSV for a
// spreadsheet, JSON for a program. CSV and JSON carry everything the record
// knows; the table leaves out what does not fit on a line.

// runColumns are the CSV's columns for runs, in order.
var runColumns = []string{
	"run", "session", "agent", "project", "checkout", "branch", "worktree", "for",
	"space", "started", "ended", "running", "reason", "duration_seconds",
	"files_changed", "files", "commits",
	"input_tokens", "output_tokens", "cache_write_tokens", "cache_read_tokens", "models",
	"cost_usd", "cost_complete", "violations", "pr",
}

// WriteRunsCSV writes runs as CSV, one row each.
func WriteRunsCSV(w io.Writer, runs []*Run, now time.Time) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(runColumns); err != nil {
		return err
	}
	for _, r := range runs {
		t := r.Usage.Total()
		var violations []string
		for _, v := range r.Violations {
			violations = append(violations, v.Policy)
		}
		pr := ""
		if r.PR != nil {
			pr = r.PR.URL
		}
		row := []string{
			r.ID, r.Session, r.Agent, r.Project, r.Checkout, r.Branch, strconv.FormatBool(r.Worktree), r.For,
			r.Space, stamp(r.Started), stamp(r.Ended), strconv.FormatBool(r.Running), r.Reason,
			strconv.FormatInt(int64(r.Duration(now).Seconds()), 10),
			strconv.Itoa(len(r.Files)), strings.Join(r.Files, ";"), strconv.Itoa(r.Commits),
			strconv.FormatInt(t.Input, 10), strconv.FormatInt(t.Output, 10),
			strconv.FormatInt(t.CacheWrite, 10), strconv.FormatInt(t.CacheRead, 10),
			strings.Join(r.Usage.Models(), ";"),
			strconv.FormatFloat(r.Cost, 'f', 4, 64), strconv.FormatBool(r.CostComplete),
			strings.Join(violations, ";"), pr,
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// WriteTotalsCSV writes a summary as CSV.
func WriteTotalsCSV(w io.Writer, by string, totals []Total) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{by, "runs", "running", "duration_seconds", "files_changed", "tokens", "cost_usd", "cost_complete", "violations"}); err != nil {
		return err
	}
	for _, t := range totals {
		if err := cw.Write([]string{
			t.Key, strconv.Itoa(t.Runs), strconv.Itoa(t.Running),
			strconv.FormatInt(int64(t.Duration), 10), strconv.Itoa(t.Files),
			strconv.FormatInt(t.Tokens, 10), strconv.FormatFloat(t.Cost, 'f', 4, 64),
			strconv.FormatBool(t.CostComplete), strconv.Itoa(t.Violations),
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// WriteViolationsCSV writes policy violations and refusals as CSV.
func WriteViolationsCSV(w io.Writer, vs []Violation) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"time", "outcome", "policy", "agent", "project", "branch", "worktree", "run", "message"}); err != nil {
		return err
	}
	for _, v := range vs {
		if err := cw.Write([]string{
			stamp(v.Time), outcome(v), v.Policy, v.Agent, v.Project, v.Branch,
			strconv.FormatBool(v.Worktree), v.Run, v.Message,
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// WriteJSON writes anything as indented JSON.
func WriteJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// WriteRunsTable writes runs for a person to read, the newest first.
func WriteRunsTable(w io.Writer, runs []*Run, now time.Time) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "STARTED\tTOOK\tAGENT\tPROJECT\tBRANCH\tFOR\tFILES\tTOKENS\tCOST\tSTATUS\tPR")
	var tokens int64
	var cost float64
	complete := true
	for i := len(runs) - 1; i >= 0; i-- {
		r := runs[i]
		t := r.Usage.Total().Total()
		tokens += t
		cost += r.Cost
		complete = complete && r.CostComplete
		status := r.Reason
		if r.Running {
			status = "running"
		}
		if len(r.Violations) > 0 {
			status += " · policy"
		}
		pr := "—"
		if r.PR != nil {
			pr = fmt.Sprintf("#%d %s", r.PR.Number, r.PR.State)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%d\t%s\t%s\t%s\t%s\n",
			r.Started.Local().Format("2006-01-02 15:04"), Took(r.Duration(now), r),
			r.Agent, projectName(r), dash(r.Branch), dash(r.For), len(r.Files),
			Count(t), Money(r.Cost, r.CostComplete), status, pr)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "\n%d runs · %s tokens · %s\n", len(runs), Count(tokens), Money(cost, complete))
	return err
}

// WriteTotalsTable writes a summary for a person to read.
func WriteTotalsTable(w io.Writer, by string, totals []Total) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "%s\tRUNS\tRUNNING\tTIME\tFILES\tTOKENS\tCOST\tVIOLATIONS\n", strings.ToUpper(by))
	for _, t := range totals {
		key := t.Key
		if by == "project" {
			key = filepath.Base(key)
		}
		fmt.Fprintf(tw, "%s\t%d\t%d\t%s\t%d\t%s\t%s\t%d\n", key, t.Runs, t.Running,
			roundDuration(time.Duration(t.Duration)*time.Second), t.Files, Count(t.Tokens),
			Money(t.Cost, t.CostComplete), t.Violations)
	}
	return tw.Flush()
}

// WriteViolationsTable writes violations and refusals for a person to read.
func WriteViolationsTable(w io.Writer, vs []Violation) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "WHEN\tOUTCOME\tAGENT\tPROJECT\tBRANCH\tWHY")
	for i := len(vs) - 1; i >= 0; i-- {
		v := vs[i]
		project := "—"
		if v.Project != "" {
			project = filepath.Base(v.Project)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", v.Time.Local().Format("2006-01-02 15:04"),
			outcome(v), dash(v.Agent), project, dash(v.Branch), v.Message)
	}
	return tw.Flush()
}

func outcome(v Violation) string {
	if v.Refused {
		return "refused"
	}
	return "ran"
}

func projectName(r *Run) string {
	switch {
	case r.Project != "":
		return filepath.Base(r.Project)
	case r.Dir != "":
		return r.Dir
	}
	return "—"
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// Took is how long a run went on, or "?" for one whose end is not known.
func Took(d time.Duration, r *Run) string {
	if !r.Running && r.Ended.IsZero() {
		return "?"
	}
	return roundDuration(d)
}

func roundDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

// Count is a number of tokens, short: 950, 12.3k, 4.1M.
func Count(n int64) string {
	switch {
	case n < 1000:
		return strconv.FormatInt(n, 10)
	case n < 1_000_000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return fmt.Sprintf("%.1fM", float64(n)/1e6)
}

// Money is a cost in dollars, marked as a lower bound when some model in it
// had no price.
func Money(usd float64, complete bool) string {
	s := fmt.Sprintf("$%.2f", usd)
	if !complete {
		s = "≥" + s
	}
	return s
}
