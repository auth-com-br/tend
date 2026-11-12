package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/ui"
)

// `tend keys` prints what every key does, with the user's own bindings
// applied. It is the list to read before writing a [keys.bind] table, and it
// comes from the same table the parser uses, so it cannot describe a key that
// does something else.
func runKeys(args []string) error {
	fs := flag.NewFlagSet("keys", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(),
			"usage: tend keys\n\n"+
				"prints every command and the key it is on, after the prefix.\n"+
				"rebind them with a [keys.bind] table in the settings file:\n\n"+
				"  [keys.bind]\n"+
				"  detach = \"q\"\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s %v\n", tag(), err)
		cfg = config.Defaults()
	}
	bindings, notes, err := ui.BindingsFrom(cfg.Keys.Bind)
	if err != nil {
		return err
	}

	prefix := cfg.Keys.Prefix
	if prefix == "" {
		prefix = "ctrl+b"
	}
	fmt.Fprintf(os.Stderr, "%s prefix is %s\n", tag(), prefix)
	t := newTable("COMMAND", "KEY")
	for _, pair := range ui.Bound(bindings) {
		key := pair[1]
		if key == "" {
			key = "-"
		}
		t.row(pair[0], key)
	}
	if err := t.flush(); err != nil {
		return err
	}
	for _, note := range notes {
		fmt.Fprintf(os.Stderr, "%s %s\n", tag(), note)
	}
	if len(cfg.Keys.Command) > 0 {
		_, customNotes, err := ui.CustomFrom(cfg.Keys.Command, bindings)
		if err != nil {
			return err
		}
		fmt.Println()
		c := newTable("YOUR COMMAND", "KEY", "TYPE")
		for _, cmd := range cfg.Keys.Command {
			c.row(commandLabel(cmd), cmd.KeyName(), cmd.Kind())
		}
		if err := c.flush(); err != nil {
			return err
		}
		for _, note := range customNotes {
			fmt.Fprintf(os.Stderr, "%s %s\n", tag(), note)
		}
	}
	copyKeys, err := ui.CopyKeyNames(cfg.Keys.CopyMode)
	if err != nil {
		return err
	}
	fmt.Println()
	c := newTable("COPY MODE", "KEYS")
	for _, pair := range copyKeys {
		key := pair[1]
		if key == "" {
			key = "-"
		}
		c.row(pair[0], key)
	}
	if err := c.flush(); err != nil {
		return err
	}
	if len(ui.Bound(bindings)) == 0 {
		return errors.New("no commands are bound")
	}
	return nil
}
