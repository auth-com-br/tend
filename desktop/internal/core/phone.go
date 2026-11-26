package core

import (
	"errors"
	"fmt"

	"github.com/auth-com-br/tend/internal/proto"
)

// The phone view, as the TUI's menu item opens it (cmd/tend/tui_phone.go):
// the session's server runs `tend web`, and the screen says whether it is
// on, where to open it from the phone, and turns it on or off. The
// addresses are copied with a click. It is the server's, so a view turned
// on here is on for the TUI too.

// panelPhone is the phone view's screen.
const panelPhone = "phone"

// PhoneState is the screen as QML draws it.
type PhoneState struct {
	Loaded  bool     `json:"loaded"`
	On      bool     `json:"on"`
	Running bool     `json:"running"`
	URLs    []string `json:"urls,omitempty"`
	User    string   `json:"user,omitempty"`
	Error   string   `json:"error,omitempty"`
	// Busy is what the screen says while the server is asked.
	Busy string `json:"busy,omitempty"`
}

// errOldServer is a server from before the phone view was in the menu.
var errOldServer = errors.New("this server is older than the phone view in the menu: tend handoff moves it to this build")

// phoneCall asks the server for the view's state, or to change it.
func (s *Session) phoneCall(method, busy string) {
	s.mu.Lock()
	c, v := s.client, s.tools.phone
	if v == nil {
		s.mu.Unlock()
		return
	}
	v.Busy = busy
	s.mu.Unlock()
	s.stateChanged()

	var st proto.WebStatus
	var err error
	switch method {
	case proto.MethodWebStart:
		st, err = c.WebStart()
	case proto.MethodWebStop:
		st, err = c.WebStop()
	default:
		st, err = c.WebStatus()
	}
	if errors.Is(err, proto.ErrUnknownMethod) {
		err = errOldServer
	}

	s.mu.Lock()
	if v := s.tools.phone; v != nil {
		v.Busy, v.Loaded = "", true
		if err != nil {
			v.Error = err.Error()
		} else {
			v.On, v.Running, v.URLs, v.User, v.Error = st.On, st.Running, st.URLs, st.User, st.Error
		}
	}
	s.mu.Unlock()
	s.stateChanged()
}

// PhoneAction is what the screen asks: "toggle" the view on or off, or
// "copy" the address at Index.
type PhoneAction struct {
	Do    string `json:"do"`
	Index int    `json:"index"`
}

// Phone does what the phone view's screen asks.
func (s *Session) Phone(a PhoneAction) error {
	s.mu.Lock()
	v := s.tools.phone
	var on bool
	var urls []string
	if v != nil {
		on, urls = v.On, v.URLs
	}
	s.mu.Unlock()
	if v == nil {
		return nil
	}
	switch a.Do {
	case "toggle":
		if on {
			go s.phoneCall(proto.MethodWebStop, "turning it off…")
		} else {
			go s.phoneCall(proto.MethodWebStart, "starting it…")
		}
	case "copy":
		if a.Index < 0 || a.Index >= len(urls) {
			return nil
		}
		if s.Copied != nil {
			s.Copied(urls[a.Index])
		}
		s.say("copied " + urls[a.Index])
	default:
		return fmt.Errorf("the phone view's screen asked for %q", a.Do)
	}
	return nil
}
