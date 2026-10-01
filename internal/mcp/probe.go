package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// A test of a server is the start of a conversation an agent would have
// with it: initialize, the initialized notification, and the list of its
// tools. A server that answers that is one an agent can use; anything less
// is said as what went wrong. It is MCP spoken here, with the standard
// library, rather than an agent's own health check: `claude mcp list` starts
// every server to check one, and the others check only some transports.

// ProtocolVersion is the MCP version tend asks for; a server answers with
// the one it speaks.
const ProtocolVersion = "2025-06-18"

// ProbeTimeout bounds one test: npx fetching a package the first time is
// the slow case, and a test is not worth more than this.
const ProbeTimeout = 20 * time.Second

// ProbeResult is what a test found.
type ProbeResult struct {
	// Status is "ok", "auth" (the server wants a login, which an agent
	// does through OAuth; not a fault), "timeout" or "error".
	Status   string
	Elapsed  time.Duration
	Tools    int
	Server   string
	Version  string
	Protocol string
	// Error is what went wrong, with every secret of the definition taken
	// out of it.
	Error string
	// Stderr is the end of what a stdio server wrote to stderr, which is
	// usually where it says why it would not start.
	Stderr string
}

var errAuth = errors.New("the server asks for a login")

// errNotHTTP is a URL that does not speak streamable HTTP, tried again as
// SSE: Cursor and OpenCode keep both under a plain url and tell them apart
// the same way.
var errNotHTTP = errors.New("not streamable HTTP")

// Probe tests a server.
func Probe(ctx context.Context, d Def, client string) ProbeResult {
	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	start := time.Now()
	res := ProbeResult{}
	expanded, err := expandDef(d)
	if err != nil {
		res.Status, res.Error = "error", err.Error()
		return res
	}
	err = probe(ctx, expanded, client, &res)
	if errors.Is(err, errNotHTTP) && expanded.Transport == HTTP {
		expanded.Transport = SSE
		err = probe(ctx, expanded, client, &res)
	}
	res.Elapsed = time.Since(start)
	switch {
	case err == nil:
		res.Status = "ok"
	case errors.Is(err, errAuth):
		res.Status, res.Error = "auth", "the server asks for a login (the agent signs in to it)"
	case ctx.Err() != nil:
		res.Status, res.Error = "timeout", fmt.Sprintf("no answer in %s", ProbeTimeout)
	default:
		res.Status, res.Error = "error", err.Error()
	}
	res.Error = redact(res.Error, d)
	res.Stderr = redact(res.Stderr, d)
	return res
}

// conn is one transport's side of a JSON-RPC conversation.
type conn interface {
	call(ctx context.Context, method string, params any) (json.RawMessage, error)
	notify(ctx context.Context, method string, params any) error
	close()
}

func probe(ctx context.Context, d Def, client string, res *ProbeResult) error {
	var c conn
	var err error
	switch d.Transport {
	case Stdio:
		c, err = dialStdio(ctx, d, res)
	case SSE:
		c, err = dialSSE(ctx, d)
	default:
		c, err = &httpConn{d: d, client: &http.Client{}}, nil
	}
	if err != nil {
		return err
	}
	defer c.close()

	raw, err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "tend", "version": client},
	})
	if err != nil {
		return err
	}
	var init struct {
		ProtocolVersion string                     `json:"protocolVersion"`
		Capabilities    map[string]json.RawMessage `json:"capabilities"`
		ServerInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(raw, &init); err != nil {
		return fmt.Errorf("its answer to initialize is not MCP: %w", err)
	}
	res.Protocol, res.Server, res.Version = init.ProtocolVersion, init.ServerInfo.Name, init.ServerInfo.Version
	if hc, ok := c.(*httpConn); ok {
		hc.protocol = init.ProtocolVersion
	}
	if err := c.notify(ctx, "notifications/initialized", nil); err != nil {
		return err
	}
	if _, ok := init.Capabilities["tools"]; !ok {
		return nil // a server of prompts or resources only has no tools to count
	}
	cursor := ""
	for page := 0; page < 5; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := c.call(ctx, "tools/list", params)
		if err != nil {
			return fmt.Errorf("listing its tools: %w", err)
		}
		var list struct {
			Tools      []json.RawMessage `json:"tools"`
			NextCursor string            `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &list); err != nil {
			return fmt.Errorf("its tool list is not MCP: %w", err)
		}
		res.Tools += len(list.Tools)
		if list.NextCursor == "" {
			break
		}
		cursor = list.NextCursor
	}
	return nil
}

// message is any JSON-RPC message: a request (method and id), a
// notification (method alone) or a response (id, and result or error).
type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  any             `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func request(id int, method string, params any) message {
	m := message{JSONRPC: "2.0", Method: method, Params: params}
	if id > 0 {
		m.ID = json.RawMessage(fmt.Sprint(id))
	}
	return m
}

// answer is what a response carries: its result, or its error as one.
func (m message) answer() (json.RawMessage, error) {
	if m.Error != nil {
		return nil, fmt.Errorf("the server answered with error %d: %s", m.Error.Code, m.Error.Message)
	}
	return m.Result, nil
}

// --- stdio --------------------------------------------------------------

type stdioConn struct {
	cmd   *exec.Cmd
	in    io.WriteCloser
	msgs  chan message
	ended chan struct{}
	// quit lets the reader go when nobody is reading any more.
	quit chan struct{}
	errs *tail
	res  *ProbeResult
	mu   sync.Mutex
	next int
}

// tail keeps the end of what a process writes, for saying why it ended.
type tail struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > 4096 {
		t.buf = t.buf[len(t.buf)-4096:]
	}
	return len(p), nil
}

// errorLine is a line of stderr that says what went wrong.
var errorLine = regexp.MustCompile(`(?i)error|fatal|required|cannot|can't|not found|denied|refused|missing|invalid`)

// last is the line of stderr that says most about why the server ended:
// the last that reads like an error — Node ends a crash with its version,
// which says nothing — else the last line.
func (t *tail) last() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	lines := strings.Split(strings.TrimSpace(string(t.buf)), "\n")
	s := strings.TrimSpace(lines[len(lines)-1])
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); errorLine.MatchString(l) && !strings.Contains(l, "A complete log") {
			s = l
			break
		}
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

func dialStdio(ctx context.Context, d Def, res *ProbeResult) (conn, error) {
	if d.Command == "" {
		return nil, errors.New("it has no command to start")
	}
	cmd := exec.Command(d.Command, d.Args...)
	cmd.Env = os.Environ()
	for k, v := range d.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if home, err := os.UserHomeDir(); err == nil {
		cmd.Dir = home // where an agent started from a terminal would be
	}
	setGroup(cmd)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	errs := &tail{}
	cmd.Stderr = errs
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("could not start %s: %w", d.Command, err)
	}
	c := &stdioConn{cmd: cmd, in: in, msgs: make(chan message, 16), ended: make(chan struct{}),
		quit: make(chan struct{}), errs: errs, res: res}
	go func() {
		defer close(c.ended)
		r := bufio.NewReaderSize(out, 64*1024)
		for {
			// ReadBytes, not a Scanner: a tool list can be one line far
			// longer than a Scanner's limit.
			line, err := r.ReadBytes('\n')
			if len(bytes.TrimSpace(line)) > 0 {
				var m message
				// A line that is not JSON is a server logging to stdout,
				// which many do; it is skipped, as the agents skip it.
				if json.Unmarshal(line, &m) == nil && m.JSONRPC != "" {
					select {
					case c.msgs <- m:
					case <-c.quit:
						return
					}
				}
			}
			if err != nil {
				return
			}
		}
	}()
	return c, nil
}

func (c *stdioConn) send(m message) error {
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err = c.in.Write(append(raw, '\n'))
	return err
}

func (c *stdioConn) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	c.next++
	id := c.next
	c.mu.Unlock()
	if err := c.send(request(id, method, params)); err != nil {
		return nil, fmt.Errorf("it would not take a request: %w", err)
	}
	want := fmt.Sprint(id)
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-c.ended:
			// Drain what came before it ended, which may be the answer.
			select {
			case m := <-c.msgs:
				if string(m.ID) == want && m.Method == "" {
					return m.answer()
				}
			default:
			}
			return nil, errors.New("it exited before answering")
		case m := <-c.msgs:
			if m.Method != "" {
				// A request from the server — a ping while it starts is
				// common — is answered so it does not wait on tend.
				if len(m.ID) > 0 {
					reply := message{JSONRPC: "2.0", ID: m.ID}
					if m.Method == "ping" {
						reply.Result = json.RawMessage(`{}`)
					} else {
						reply.Error = &struct {
							Code    int    `json:"code"`
							Message string `json:"message"`
						}{-32601, "tend's test does not handle " + m.Method}
					}
					_ = c.send(reply)
				}
				continue
			}
			if string(m.ID) == want {
				return m.answer()
			}
		}
	}
}

func (c *stdioConn) notify(_ context.Context, method string, params any) error {
	return c.send(request(0, method, params))
}

func (c *stdioConn) close() {
	_ = c.in.Close()
	close(c.quit)
	select {
	case <-c.ended:
	case <-time.After(time.Second):
	}
	killGroup(c.cmd)
	_ = c.cmd.Wait()
	// Read after Wait, when nothing writes to it any more.
	c.res.Stderr = c.errs.last()
}

// --- streamable HTTP ----------------------------------------------------

type httpConn struct {
	d        Def
	client   *http.Client
	session  string
	protocol string
	next     int
}

func (c *httpConn) post(ctx context.Context, m message) (*http.Response, error) {
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.d.URL, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range c.d.Headers {
		req.Header.Set(k, v)
	}
	if c.session != "" {
		req.Header.Set("Mcp-Session-Id", c.session)
	}
	if c.protocol != "" {
		req.Header.Set("MCP-Protocol-Version", c.protocol)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach it: %w", err)
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		resp.Body.Close()
		return nil, errAuth
	case m.Method == "initialize" && (resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed):
		resp.Body.Close()
		return nil, errNotHTTP
	case resp.StatusCode >= 300:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		resp.Body.Close()
		return nil, fmt.Errorf("it answered %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	if s := resp.Header.Get("Mcp-Session-Id"); s != "" {
		c.session = s
	}
	return resp, nil
}

func (c *httpConn) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.next++
	want := fmt.Sprint(c.next)
	resp, err := c.post(ctx, request(c.next, method, params))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		var found json.RawMessage
		var ferr error
		err := readEvents(resp.Body, func(event, data string) bool {
			var m message
			if json.Unmarshal([]byte(data), &m) == nil && string(m.ID) == want && m.Method == "" {
				found, ferr = m.answer()
				return false
			}
			return true
		})
		if found != nil || ferr != nil {
			return found, ferr
		}
		if err == nil {
			err = errors.New("its event stream ended without the answer")
		}
		return nil, err
	}
	var m message
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&m); err != nil {
		return nil, fmt.Errorf("its answer is not JSON-RPC: %w", err)
	}
	return m.answer()
}

func (c *httpConn) notify(ctx context.Context, method string, params any) error {
	resp, err := c.post(ctx, request(0, method, params))
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// close ends the session, as a client is asked to; a server that does not
// keep sessions answers 405, which is fine.
func (c *httpConn) close() {
	if c.session == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.d.URL, nil)
	if err != nil {
		return
	}
	req.Header.Set("Mcp-Session-Id", c.session)
	for k, v := range c.d.Headers {
		req.Header.Set(k, v)
	}
	if resp, err := c.client.Do(req); err == nil {
		resp.Body.Close()
	}
}

// readEvents reads a server-sent event stream, handing each event to fn
// until fn returns false or the stream ends.
func readEvents(r io.Reader, fn func(event, data string) bool) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 8<<20)
	event, data := "", []string{}
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if len(data) > 0 {
				if !fn(event, strings.Join(data, "\n")) {
					return nil
				}
			}
			event, data = "", nil
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	return sc.Err()
}

// --- SSE (the older HTTP transport) -------------------------------------

type sseConn struct {
	d        Def
	client   *http.Client
	endpoint string
	body     io.Closer
	msgs     chan message
	ended    chan struct{}
	quit     chan struct{}
	next     int
}

func dialSSE(ctx context.Context, d Def) (conn, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	for k, v := range d.Headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach it: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		resp.Body.Close()
		return nil, errAuth
	}
	if resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("it answered %s", resp.Status)
	}
	c := &sseConn{d: d, client: client, body: resp.Body, msgs: make(chan message, 16),
		ended: make(chan struct{}), quit: make(chan struct{})}
	endpoint := make(chan string, 1)
	go func() {
		defer close(c.ended)
		_ = readEvents(resp.Body, func(event, data string) bool {
			if event == "endpoint" {
				select {
				case endpoint <- data:
				default:
				}
				return true
			}
			var m message
			if json.Unmarshal([]byte(data), &m) == nil {
				select {
				case c.msgs <- m:
				case <-c.quit:
					return false
				}
			}
			return true
		})
	}()
	select {
	case e := <-endpoint:
		base, err := url.Parse(d.URL)
		if err != nil {
			return nil, err
		}
		ref, err := url.Parse(e)
		if err != nil {
			return nil, err
		}
		c.endpoint = base.ResolveReference(ref).String()
		return c, nil
	case <-c.ended:
		return nil, errors.New("its event stream ended without saying where to send requests")
	case <-ctx.Done():
		close(c.quit)
		resp.Body.Close()
		return nil, ctx.Err()
	}
}

func (c *sseConn) send(ctx context.Context, m message) error {
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range c.d.Headers {
		req.Header.Set(k, v)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach it: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("it answered %s", resp.Status)
	}
	return nil
}

func (c *sseConn) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.next++
	want := fmt.Sprint(c.next)
	if err := c.send(ctx, request(c.next, method, params)); err != nil {
		return nil, err
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-c.ended:
			return nil, errors.New("its event stream ended before the answer")
		case m := <-c.msgs:
			if string(m.ID) == want && m.Method == "" {
				return m.answer()
			}
		}
	}
}

func (c *sseConn) notify(ctx context.Context, method string, params any) error {
	return c.send(ctx, request(0, method, params))
}

func (c *sseConn) close() {
	close(c.quit)
	_ = c.body.Close()
}

// --- variables and secrets ----------------------------------------------

// varRef is a variable as the agents write one in a server's settings:
// ${VAR}, ${VAR:-default} (Claude Code), ${env:VAR} (Cursor) and {env:VAR}
// (OpenCode).
var varRef = regexp.MustCompile(`\$\{env:([A-Za-z_][A-Za-z0-9_]*)\}|\{env:([A-Za-z_][A-Za-z0-9_]*)\}|\$\{([A-Za-z_][A-Za-z0-9_]*)(:-([^}]*))?\}`)

// expandDef fills in the variables a definition refers to, from the tend
// server's environment — which is what the agents do, from theirs. One that
// is not set is said rather than tried, since the server would not be the
// one the agent starts.
func expandDef(d Def) (Def, error) {
	var missing []string
	expand := func(s string) string {
		return varRef.ReplaceAllStringFunc(s, func(m string) string {
			p := varRef.FindStringSubmatch(m)
			name := p[1] + p[2] + p[3]
			if v, ok := os.LookupEnv(name); ok {
				return v
			}
			if p[4] != "" {
				return p[5]
			}
			missing = append(missing, name)
			return m
		})
	}
	out := Def{Transport: d.Transport, Command: expand(d.Command), URL: expand(d.URL)}
	for _, a := range d.Args {
		out.Args = append(out.Args, expand(a))
	}
	if d.Env != nil {
		out.Env = map[string]string{}
		for k, v := range d.Env {
			out.Env[k] = expand(v)
		}
	}
	if d.Headers != nil {
		out.Headers = map[string]string{}
		for k, v := range d.Headers {
			out.Headers[k] = expand(v)
		}
	}
	if len(missing) > 0 {
		return out, fmt.Errorf("it uses ${%s}, which is not set where tend's server runs", strings.Join(missing, "}, ${"))
	}
	return out, nil
}

// redact takes every secret of a definition out of a text that is to be
// shown: its env and header values, and what a URL carries in its user
// information and query.
func redact(s string, d Def) string {
	if s == "" {
		return s
	}
	var secrets []string
	for _, v := range d.Env {
		secrets = append(secrets, v)
	}
	for _, v := range d.Headers {
		secrets = append(secrets, v)
		// "Bearer xyz": the token alone too, in case it is quoted apart.
		if _, tok, ok := strings.Cut(v, " "); ok {
			secrets = append(secrets, tok)
		}
	}
	if u, err := url.Parse(d.URL); err == nil {
		if u.User != nil {
			secrets = append(secrets, u.User.String())
		}
		for _, vs := range u.Query() {
			secrets = append(secrets, vs...)
		}
	}
	for _, sec := range secrets {
		if len(sec) >= 4 {
			s = strings.ReplaceAll(s, sec, "…")
		}
	}
	return s
}

// RedactURL is a URL as it can be shown: its user information and the
// values of its query taken out.
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	u.User = nil
	if u.RawQuery != "" {
		q := u.Query()
		for k := range q {
			q.Set(k, "…")
		}
		u.RawQuery = q.Encode()
	}
	return u.String()
}

// secretArg is an argument that names a secret, whose value — the next
// argument, or after its = — is not shown.
var secretArg = regexp.MustCompile(`(?i)(key|token|secret|password|passwd|auth|credential)`)

// RedactArgs is a command's arguments as they can be shown: the value of
// any argument that names a key, token, secret or password replaced, and
// URLs redacted. It is a guess at what is secret, and errs towards hiding.
func RedactArgs(args []string) []string {
	out := make([]string, len(args))
	hideNext := false
	for i, a := range args {
		switch {
		case hideNext:
			out[i], hideNext = "…", false
		case strings.Contains(a, "://"):
			// Before the key=value rule: a URL's query has = in it, and
			// its user information would be left showing.
			out[i] = RedactURL(a)
		case strings.HasPrefix(a, "-") && secretArg.MatchString(a):
			if k, _, ok := strings.Cut(a, "="); ok {
				out[i] = k + "=…"
			} else {
				out[i], hideNext = a, true
			}
		case strings.Contains(a, "=") && secretArg.MatchString(strings.SplitN(a, "=", 2)[0]):
			k, _, _ := strings.Cut(a, "=")
			out[i] = k + "=…"
		default:
			out[i] = a
		}
	}
	return out
}
