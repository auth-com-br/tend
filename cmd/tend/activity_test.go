package main

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestPoliciesAndTheActivityRecordEndToEnd is the governance story through
// the real binary and real git: a policy in the settings file, an agent tend
// refuses to start because of it, one started by hand that breaks it anyway
// and is written down, and `tend activity` reporting both and the run as a
// table, CSV and JSON. If it regresses, a team that set "no agents on main"
// has a rule nothing enforces, or a record nobody can read.
func TestPoliciesAndTheActivityRecordEndToEnd(t *testing.T) {
	runtimeDir, stateDir, configDir := t.TempDir(), t.TempDir(), t.TempDir()
	cfg := filepath.Join(configDir, "tend.toml")
	if err := os.WriteFile(cfg, []byte("[policy]\nprotected_branches = [\"main\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	t.Setenv("TEND_STATE_DIR", stateDir)
	t.Setenv("TEND_CONFIG", cfg)
	bin := buildBinary(t)

	repo := filepath.Join(t.TempDir(), "shop")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitEnv := append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"commit", "-q", "--allow-empty", "-m", "first"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir, cmd.Env = repo, gitEnv
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "TEND_STATE_DIR="+stateDir,
		"TEND_CONFIG="+cfg, "SHELL=/bin/sh")
	run := func(args ...string) (string, error) {
		cmd := exec.Command(bin, args...)
		cmd.Env, cmd.Dir = env, repo
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	if out, err := run("activity"); err != nil || !strings.Contains(out, "no agent activity recorded yet") {
		t.Fatalf("activity before anything ran = %q, %v", out, err)
	}

	if out, err := run("new", "-s", "gov", "--", "/bin/sh", "-c", "sleep 60"); err != nil {
		t.Fatalf("tend new: %v\n%s", err, out)
	}
	t.Cleanup(func() { stopSession(t, "gov") })

	// tend is asked to start an agent on main, and will not.
	out, err := run("new", "-s", "gov", "-agent", "claude", "-cwd", repo, "--", "/bin/sh", "-c", "sleep 60")
	if err == nil || !strings.Contains(out, "protected branch") {
		t.Fatalf("an agent on main = %q, %v; want a refusal naming the policy", out, err)
	}

	// Somebody starts one by hand in the shell that is there: tend did not
	// start it, so it is reported rather than refused.
	if out, err := run("api", "-s", "gov", "pane.report_agent",
		`{"pane_id":"p_1","source":"test","agent":"claude","state":"working","seq":1}`); err != nil {
		t.Fatalf("report: %v\n%s", err, out)
	}

	var violations struct {
		Violations []struct {
			Policy  string `json:"policy"`
			Refused bool   `json:"refused"`
			Branch  string `json:"branch"`
		} `json:"violations"`
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		out, err = run("activity", "-violations", "-format", "json")
		if err != nil {
			t.Fatalf("activity -violations: %v\n%s", err, out)
		}
		if json.Unmarshal([]byte(out), &violations) == nil && len(violations.Violations) == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("violations = %s; want the refusal and the agent that ran", out)
		}
		time.Sleep(100 * time.Millisecond)
	}
	refused := 0
	for _, v := range violations.Violations {
		if v.Policy != "protected_branch" || v.Branch != "main" {
			t.Errorf("violation = %+v", v)
		}
		if v.Refused {
			refused++
		}
	}
	if refused != 1 {
		t.Errorf("%d refusals, want 1", refused)
	}

	// The run, while it goes on: in CSV, one row with the project and the
	// branch, and in the table for the project that directory is in.
	out, err = run("activity", "-format", "csv")
	if err != nil {
		t.Fatalf("activity -format csv: %v\n%s", err, out)
	}
	rows, err := csv.NewReader(strings.NewReader(out)).ReadAll()
	if err != nil || len(rows) != 2 {
		t.Fatalf("csv = %q, %v", out, err)
	}
	row := map[string]string{}
	for i, col := range rows[0] {
		row[col] = rows[1][i]
	}
	if row["agent"] != "claude" || row["branch"] != "main" || row["running"] != "true" || row["violations"] != "protected_branch" {
		t.Errorf("csv row = %v", row)
	}
	if out, err := run("activity", "-project", "."); err != nil || !strings.Contains(out, "shop") || !strings.Contains(out, "running") {
		t.Errorf("activity -project . = %q, %v", out, err)
	}
	if out, err := run("activity", "-project", t.TempDir()); err != nil || !strings.Contains(out, "no agent activity") {
		t.Errorf("activity for another project = %q, %v", out, err)
	}
	if out, err := run("activity", "-by", "agent"); err != nil || !strings.Contains(out, "claude") {
		t.Errorf("activity -by agent = %q, %v", out, err)
	}
}
