package notify

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// A desktop notification that can be clicked (herdr discussion #625: "click
// the notification, land on the agent").
//
// The click is told only to the connection that raised the notification:
// GNOME Shell sends ActionInvoked to it alone, so a second program listening
// on the bus hears nothing — tried, with gdbus calling Notify and gdbus
// monitor listening, on the owner's GNOME 42: the notification went up and
// no signal came. A click can only be heard by the process that raised it
// and stays connected, which is what notify-send does when it is given an
// action: it waits, and prints the action clicked.
//
// Without notify-send — the owner's desktop has gdbus and not it, and
// "system" notifications had been failing there for want of it — the
// notification is raised over D-Bus with gdbus, and cannot be clicked
// through.

// clickWait is how long a notification is waited on for its click.
var clickWait = 30 * time.Minute

// commandFor runs a notifier; a test replaces it.
var commandFor = func(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

// ErrNoActions means nothing here can raise a notification that is clicked.
var ErrNoActions = errors.New("notify: nothing here raises a notification that can be clicked")

// SystemWithClick raises a desktop notification and calls clicked, once, if
// the person clicks it. It returns once the notification is asked for; the
// wait for the click goes on behind it. ErrNoActions means notify-send is not
// here or too old for actions, and the caller falls back to System.
func SystemWithClick(title, body string, clicked func()) error {
	path, err := exec.LookPath("notify-send")
	if err != nil {
		return ErrNoActions
	}
	title, body = sanitize(title), sanitize(body)
	args := []string{"--app-name=tend", "--action=default=Open", title}
	if body != "" {
		args = append(args, body)
	}
	ctx, cancel := context.WithTimeout(context.Background(), clickWait)
	cmd := commandFor(ctx, path, args...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Start(); err != nil {
		cancel()
		return ErrNoActions
	}
	// An old notify-send refuses --action at once; a new one waits for the
	// click. Which it is shows within a moment.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		cancel()
		if err != nil {
			return ErrNoActions
		}
		if strings.TrimSpace(out.String()) == "default" {
			clicked()
		}
		return nil
	case <-time.After(500 * time.Millisecond):
	}
	go func() {
		defer cancel()
		if err := <-done; err == nil && strings.TrimSpace(out.String()) == "default" {
			clicked()
		}
	}()
	return nil
}

// systemOverDBus raises a notification by asking the notification server
// over D-Bus with gdbus, for a desktop without notify-send. Not clickable:
// see above.
func systemOverDBus(title, body string) error {
	if _, err := exec.LookPath("gdbus"); err != nil {
		return ErrNoSystemNotifier
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	reply, err := commandFor(ctx, "gdbus", "call", "--session", "--dest", "org.freedesktop.Notifications",
		"--object-path", "/org/freedesktop/Notifications",
		"--method", "org.freedesktop.Notifications.Notify",
		// "int32 -1", the server's own expiry: a bare -1 is read by gdbus
		// as an option, and the call refused with its usage.
		"'tend'", "uint32 0", "''", gvariantString(title), gvariantString(body), "@as []", "@a{sv} {}", "int32 -1").Output()
	if err != nil || !idReply.Match(reply) {
		return ErrNoSystemNotifier
	}
	return nil
}

var idReply = regexp.MustCompile(`\(uint32 \d+,\)`)

// gvariantString is s as a GVariant string literal, which is how gdbus
// reads each argument: quoted, its quote and backslash escaped.
func gvariantString(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}
