package vt

import (
	"encoding/base64"
	"strings"
)

// OSC 7501, the Program Status Protocol: a program saying what state it is
// in, rather than leaving the terminal to guess from what it drew (herdr
// discussion #5073, where reading the screen is the cause of its recurring
// state bugs). Claude Code speaks it — read in its binary, 2.1.322 — but
// only to a terminal that answers its probe, "OSC 7501 ; ?", with an OSC
// 7501 whose data starts with "?"; one that does not answer never hears it.
// Then it sends, whenever its state changes:
//
//	OSC 7501 ; state=working:app=claude-code[:id=…][:kind=…][:progress=N][:title=<b64>][:msg=<b64>]
//	OSC 7501 ; state=clear[:id=…]
//
// with state one of working, blocked (kind: permission, question, auth…),
// done, idle and error; title and msg in base64. An entry with an id is a
// subagent's; the one without is the program's own, which is what is kept.

// ProgramStatus is what a program last said of itself over OSC 7501.
type ProgramStatus struct {
	State string // working, blocked, done, idle, error; "" once cleared
	App   string // who said it: "claude-code"
	Kind  string // what a blocked program waits on
	Msg   string // a line about it, decoded
}

// ProgramStatus is the program's own status and a revision that changes
// with every report, cleared ones too, so a reader can tell news from the
// same thing again.
func (s *Screen) ProgramStatus() (ProgramStatus, uint64) {
	return s.status, s.statusRev
}

// programStatus handles one OSC 7501.
func (s *Screen) programStatus(data string) {
	if strings.HasPrefix(data, "?") {
		// The probe. Answered with the protocol's own "?": yes, this
		// terminal reads it.
		s.reply([]byte("\x1b]7501;?\x1b\\"))
		return
	}
	fields := map[string]string{}
	for _, kv := range strings.Split(data, ":") {
		k, v, ok := strings.Cut(kv, "=")
		if ok {
			fields[k] = v
		}
	}
	if fields["id"] != "" {
		return // a subagent's: the pane is the program's own
	}
	next := ProgramStatus{}
	if state := fields["state"]; state != "clear" {
		switch state {
		case "working", "blocked", "done", "idle", "error":
		default:
			return // nothing the reader would know what to do with
		}
		next = ProgramStatus{State: state, App: fields["app"], Kind: fields["kind"], Msg: decodeStatusText(fields["msg"])}
	}
	s.status = next
	s.statusRev++
}

// decodeStatusText reads a base64 field; text that is not base64 is kept
// as it came, for a program that sends it plain.
func decodeStatusText(v string) string {
	if v == "" {
		return ""
	}
	if b, err := base64.StdEncoding.DecodeString(v); err == nil {
		return string(b)
	}
	return v
}
