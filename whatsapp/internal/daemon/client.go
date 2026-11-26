package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/auth-com-br/tend/internal/transport"
)

// Listen takes the daemon's socket, private to its owner: transport.Listen
// makes the directory 0700 and the socket 0600, and refuses a socket another
// daemon is still answering on — one number has one connection.
func Listen(path string) (net.Listener, error) { return transport.Listen(path) }

// RemoteError is an error the daemon answered with.
type RemoteError struct{ ErrorBody }

func (e *RemoteError) Error() string { return e.Message }

// Client calls a daemon.
type Client struct {
	Path string
	// Dial, when set, replaces dialing Path: tests answer in-process.
	Dial func() (net.Conn, error)
}

var nextID atomic.Int64

// ErrNoDaemon is a socket nobody answers on.
var ErrNoDaemon = errors.New("tend-whatsapp's daemon is not running")

func (c *Client) dial() (net.Conn, error) {
	if c.Dial != nil {
		return c.Dial()
	}
	conn, err := net.DialTimeout("unix", c.Path, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("%w (%s): %v", ErrNoDaemon, c.Path, err)
	}
	return conn, nil
}

// Call makes one call and decodes its result into result (when not nil).
func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	return c.Stream(ctx, method, params, nil, result)
}

// Stream is Call for a method that answers with events first (link).
func (c *Client) Stream(ctx context.Context, method string, params any, onEvent func(LinkEvent), result any) error {
	conn, err := c.dial()
	if err != nil {
		return err
	}
	defer conn.Close()
	// A connection of its own per call, ended with the context: a link that
	// waits minutes for a phone holds up nothing else.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	req := Request{ID: strconv.FormatInt(nextID.Add(1), 10), Method: method}
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return err
		}
		req.Params = raw
	}
	line, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if _, err := conn.Write(append(line, '\n')); err != nil {
		return err
	}
	r := bufio.NewReaderSize(conn, 1<<20)
	for {
		reply, err := r.ReadBytes('\n')
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("tend-whatsapp's daemon hung up: %w", err)
		}
		var resp struct {
			Result json.RawMessage `json:"result"`
			Event  *LinkEvent      `json:"event"`
			Error  *ErrorBody      `json:"error"`
		}
		if err := json.Unmarshal(reply, &resp); err != nil {
			return err
		}
		switch {
		case resp.Error != nil:
			return &RemoteError{*resp.Error}
		case resp.Event != nil:
			if onEvent != nil {
				onEvent(*resp.Event)
			}
		default:
			if result != nil && len(resp.Result) > 0 {
				return json.Unmarshal(resp.Result, result)
			}
			return nil
		}
	}
}

// IsCode is whether err is the daemon's error with that code.
func IsCode(err error, code string) bool {
	var re *RemoteError
	return errors.As(err, &re) && re.Code == code
}
