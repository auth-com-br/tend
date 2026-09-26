// Package keyring keeps secrets in the system's keyring — the Secret
// Service on Linux (GNOME Keyring, KWallet), the Keychain on macOS —
// through the tools each system ships for it (secret-tool, security), so
// tend needs no library for it and no cgo. Where there is no such tool,
// Available says so and the caller keeps the secret in its settings file,
// readable by its owner alone, as before.
//
// A secret kept here is named in the settings file by a Ref ("keyring:"
// and its name), never written there itself.
package keyring

import (
	"errors"
	"os/exec"
	"strings"
)

// Service is the name tend's secrets are kept under.
const Service = "tend"

var (
	// ErrUnavailable is a system with no keyring tool tend can use.
	ErrUnavailable = errors.New("keyring: no keyring tool on this system")
	// ErrNotFound is a name the keyring has no secret for.
	ErrNotFound = errors.New("keyring: no such secret")
)

// refPrefix marks a settings value that names a secret in the keyring.
const refPrefix = "keyring:"

// Ref is what the settings file holds for a secret kept here.
func Ref(name string) string { return refPrefix + name }

// Name is the secret a settings value names, if it names one.
func Name(value string) (string, bool) {
	if !strings.HasPrefix(value, refPrefix) {
		return "", false
	}
	return strings.TrimPrefix(value, refPrefix), true
}

// lookPath is exec.LookPath, replaced in tests.
var lookPath = exec.LookPath

// run runs a tool with input on its standard input and returns what it
// printed; replaced in tests.
var run = func(input string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	out, err := cmd.Output()
	return string(out), err
}
