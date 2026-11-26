package core

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/auth-com-br/tend/internal/transport"
)

// apiCall makes one call on the session's automation socket (internal/api),
// for what the window needs that the client protocol does not carry:
// worktrees, which the TUI reaches the same way (cmd/tend's apiCall). The
// protocol is a line of JSON each way.
func (s *Session) apiCall(method string, params map[string]any) (map[string]any, error) {
	path, err := transport.APISocketPath(s.name)
	if err != nil {
		return nil, err
	}
	conn, err := net.Dial("unix", path)
	if err != nil {
		return nil, fmt.Errorf("the session's automation socket: %w", err)
	}
	defer conn.Close()
	// A worktree add on a large repository takes seconds; a server that
	// stopped answering must still not hang the action for ever.
	_ = conn.SetDeadline(time.Now().Add(2 * time.Minute))
	req := map[string]any{"id": "desktop", "method": method}
	if len(params) > 0 {
		req["params"] = params
	}
	line, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Write(append(line, '\n')); err != nil {
		return nil, err
	}
	reply, err := bufio.NewReaderSize(conn, 1<<16).ReadString('\n')
	if err != nil {
		return nil, err
	}
	var resp struct {
		Result map[string]any `json:"result"`
		Error  *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(reply), &resp); err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, errors.New(resp.Error.Message)
	}
	return resp.Result, nil
}
