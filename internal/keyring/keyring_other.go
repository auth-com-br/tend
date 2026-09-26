//go:build !linux && !darwin

package keyring

// Available is false: tend knows no keyring here.
func Available() bool { return false }

// Set cannot keep anything here.
func Set(name, secret string) error { return ErrUnavailable }

// Get has nothing here.
func Get(name string) (string, error) { return "", ErrUnavailable }

// Delete has nothing to forget here.
func Delete(name string) error { return ErrUnavailable }
