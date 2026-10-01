package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"

	"github.com/auth-com-br/tend/internal/proto"
)

// `tend mcp`: the MCP manager's view from the command line — every agent's
// MCP servers, and a test of each. It asks the session's server, which is
// where the agents and their files are; changing them is the manager's
// (prefix+M), where a switch can be seen before it is pressed.

const mcpUsage = `usage: tend mcp list [-s session]             the agents' MCP servers
       tend mcp test [-s session] [server...]   test them (all, or those named)`

func runMCP(args []string) error {
	if len(args) == 0 {
		return errors.New(mcpUsage)
	}
	fs := flag.NewFlagSet("mcp "+args[0], flag.ExitOnError)
	name := sessionFlag(fs)
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	c, err := connect(*name, nil)
	if err != nil {
		return err
	}
	defer c.Close()
	if !c.Supports(proto.MethodMCPList) {
		return fmt.Errorf("session %q's server is older than the MCP manager; %s moves it to this build", *name, handoffCommand(*name))
	}
	list, err := c.MCPList()
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		printMCPList(list)
		return nil
	case "test":
		return testMCP(c.MCPTest, list, fs.Args())
	}
	return errors.New(mcpUsage)
}

func printMCPList(list proto.MCPListResult) {
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "SERVER\tAGENT\tSTATE\tTRANSPORT\tTARGET")
	for _, e := range list.Entries {
		state := "on"
		if !e.Enabled {
			state = "off"
			if e.Via != "" {
				state += " (" + e.Via + ")"
			}
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", e.Name, e.Agent, state, e.Transport, mcpTarget(e))
	}
	w.Flush()
	for _, a := range list.Agents {
		if a.Error != "" {
			fmt.Fprintf(os.Stderr, "%s: %s\n", a.Name, a.Error)
		}
	}
}

// mcpTarget is what a server runs or reaches, as it can be shown.
func mcpTarget(e proto.MCPEntry) string {
	if e.URL != "" {
		return e.URL
	}
	return strings.TrimSpace(e.Command + " " + strings.Join(e.Args, " "))
}

// testMCP tests every server named — all of them when none is — once per
// distinct definition, four at a time.
func testMCP(test func(agent, name string) (proto.MCPTestResult, error), list proto.MCPListResult, names []string) error {
	want := map[string]bool{}
	for _, n := range names {
		want[strings.ToLower(n)] = true
	}
	type job struct {
		e   proto.MCPEntry
		res proto.MCPTestResult
		err error
	}
	seen := map[string]bool{}
	var jobs []*job
	for _, e := range list.Entries {
		key := strings.ToLower(e.Name)
		if len(want) > 0 && !want[key] {
			continue
		}
		if seen[key+"\x00"+e.Fingerprint] {
			continue // the same server in another agent: tested once
		}
		seen[key+"\x00"+e.Fingerprint] = true
		jobs = append(jobs, &job{e: e})
	}
	if len(jobs) == 0 {
		return errors.New("no such server")
	}
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			j.res, j.err = test(j.e.Agent, j.e.Name)
		}()
	}
	wg.Wait()
	sort.SliceStable(jobs, func(a, b int) bool { return strings.ToLower(jobs[a].e.Name) < strings.ToLower(jobs[b].e.Name) })
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "SERVER\tAS IN\tRESULT")
	failed := 0
	for _, j := range jobs {
		fmt.Fprintf(w, "%s\t%s\t%s\n", j.e.Name, j.e.Agent, mcpResultText(j.res, j.err))
		if j.err != nil || (j.res.Status != "ok" && j.res.Status != "auth") {
			failed++
		}
	}
	w.Flush()
	if failed > 0 {
		return fmt.Errorf("%d of %d did not answer", failed, len(jobs))
	}
	return nil
}

// mcpResultText is a test's result on one line.
func mcpResultText(r proto.MCPTestResult, err error) string {
	switch {
	case err != nil:
		return "✗ " + refusalText(err)
	case r.Status == "ok":
		s := fmt.Sprintf("ok %dms, %d tools", r.Millis, r.Tools)
		if r.Server != "" {
			s += " (" + strings.TrimSpace(r.Server+" "+r.Version) + ")"
		}
		return s
	case r.Status == "auth":
		return "login needed — " + r.Error
	}
	s := "✗ " + r.Error
	if r.Stderr != "" {
		s += " — " + r.Stderr
	}
	return s
}
