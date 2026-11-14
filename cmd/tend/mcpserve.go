package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/auth-com-br/tend/internal/api"
	"github.com/auth-com-br/tend/internal/mcpserve"
	"github.com/auth-com-br/tend/internal/transport"
)

// `tend mcp serve` is tend's own MCP server, on stdio, for an agent's MCP
// settings to name (`claude mcp add tend -- tend mcp serve`). In a tend pane
// it talks to the session the pane is in; elsewhere, to -s's.
func runMCPServe(args []string) error {
	fs := flag.NewFlagSet("mcp serve", flag.ExitOnError)
	name := fs.String("s", "", "session name (default: the session of the pane it runs in, else \"default\")")
	if err := fs.Parse(args); err != nil {
		return err
	}
	socket := os.Getenv(api.EnvSocketPath)
	caller := os.Getenv(api.EnvPaneID)
	if *name != "" || socket == "" {
		path, err := transport.APISocketPath(*name)
		if err != nil {
			return err
		}
		if path != socket {
			// Another session than the pane's: the pane is not in it.
			caller = ""
		}
		socket = path
	}
	srv := &mcpserve.Server{API: socketCaller(socket), Caller: caller, Version: version}
	return srv.Serve(os.Stdin, os.Stdout)
}

// socketCaller makes each call on a connection of its own, so a wait that
// lasts minutes holds up nothing else the agent asks meanwhile.
type socketCaller string

func (path socketCaller) Call(method string, params map[string]any) (map[string]any, error) {
	conn, err := net.DialTimeout("unix", string(path), 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("the tend session is not answering: %w", err)
	}
	defer conn.Close()
	limit := apiTimeout
	if ms, ok := params["timeout_ms"].(int64); ok {
		// A wait is bounded by its own timeout, and the server's answer
		// after it.
		limit = time.Duration(ms)*time.Millisecond + apiTimeout
	}
	_ = conn.SetDeadline(time.Now().Add(limit))
	req := map[string]any{"id": "mcp", "method": method}
	if params != nil {
		req["params"] = params
	}
	line, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Write(append(line, '\n')); err != nil {
		return nil, err
	}
	reply, err := bufio.NewReaderSize(conn, 1<<20).ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	var out struct {
		Result map[string]any `json:"result"`
		Error  *api.ErrorBody `json:"error"`
	}
	if err := json.Unmarshal(reply, &out); err != nil {
		return nil, err
	}
	if out.Error != nil {
		return nil, errors.New(out.Error.Message)
	}
	return out.Result, nil
}
