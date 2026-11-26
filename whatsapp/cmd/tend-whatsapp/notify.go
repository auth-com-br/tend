package main

import (
	"encoding/json"
	"net"
	"time"

	"github.com/auth-com-br/tend/internal/api"
	"github.com/auth-com-br/tend/internal/transport"
	waLog "go.mau.fi/whatsmeow/util/log"
)

func apiSocketPath(session string) (string, error) { return transport.APISocketPath(session) }

// notify is `tend notify title body` on the session at path, best effort.
func notify(path, title, body string) {
	conn, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	line, err := json.Marshal(map[string]any{
		"id": "whatsapp", "method": api.MethodNotificationShow,
		"params": map[string]any{"title": title, "body": body},
	})
	if err != nil {
		return
	}
	if _, err := conn.Write(append(line, '\n')); err != nil {
		return
	}
	// The answer is read so the server is not left writing to a closed
	// connection; what it says does not change anything here.
	buf := make([]byte, 4096)
	_, _ = conn.Read(buf)
}

// logger is whatsmeow's logging, to the daemon's stderr (its log file when
// started on demand): warnings and errors, or everything with -v. A message
// may name a chat; the log is the owner's, 0600, and `unlink -forget`
// empties it.
func logger(verbose bool) waLog.Logger {
	level := "WARN"
	if verbose {
		level = "DEBUG"
	}
	return waLog.Stdout("whatsapp", level, false)
}
