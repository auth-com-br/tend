package keyring

import (
	"errors"
	"os/exec"
	"strings"
)

// The Secret Service, through secret-tool (libsecret-tools). The secret
// goes on its standard input, never on its command line, where any user
// could read it in the process list.

// Available reports whether secret-tool is here.
func Available() bool {
	_, err := lookPath("secret-tool")
	return err == nil
}

// Set keeps a secret under a name, replacing one kept before.
func Set(name, secret string) error {
	if !Available() {
		return ErrUnavailable
	}
	_, err := run(secret, "secret-tool", "store", "--label=tend: "+name, "service", Service, "account", name)
	return err
}

// Get is the secret kept under a name.
func Get(name string) (string, error) {
	if !Available() {
		return "", ErrUnavailable
	}
	out, err := run("", "secret-tool", "lookup", "service", Service, "account", name)
	var exit *exec.ExitError
	if errors.As(err, &exit) || (err == nil && out == "") {
		// secret-tool says nothing, and fails, for a name it has not.
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return strings.TrimRight(out, "\n"), nil
}

// Delete forgets a secret; one not kept is not an error.
func Delete(name string) error {
	if !Available() {
		return ErrUnavailable
	}
	_, err := run("", "secret-tool", "clear", "service", Service, "account", name)
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return nil
	}
	return err
}
