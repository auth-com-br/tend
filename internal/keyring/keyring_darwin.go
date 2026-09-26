package keyring

import (
	"errors"
	"os/exec"
	"strings"
)

// The Keychain, through security(1). add-generic-password takes the
// secret as an argument — it has no way to read it from standard input
// without a terminal — so it is briefly in the process list of this user's
// session; the Keychain is still better than a file for everything after.

// Available reports whether security is here.
func Available() bool {
	_, err := lookPath("security")
	return err == nil
}

// Set keeps a secret under a name, replacing one kept before.
func Set(name, secret string) error {
	if !Available() {
		return ErrUnavailable
	}
	_, err := run("", "security", "add-generic-password", "-U", "-s", Service, "-a", name, "-w", secret)
	return err
}

// Get is the secret kept under a name.
func Get(name string) (string, error) {
	if !Available() {
		return "", ErrUnavailable
	}
	out, err := run("", "security", "find-generic-password", "-s", Service, "-a", name, "-w")
	var exit *exec.ExitError
	if errors.As(err, &exit) {
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
	_, err := run("", "security", "delete-generic-password", "-s", Service, "-a", name)
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return nil
	}
	return err
}
