package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/term"

	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/github"
	"github.com/auth-com-br/tend/internal/keyring"
)

// `tend gitlab`: the token the issues panel uses for a GitLab (#9). GitHub
// has gh to keep its login; GitLab is asked directly, so tend keeps a
// token for each host — in the system keyring when there is one, else in
// the settings file, readable by its owner alone. A public project is read
// without one; writing, and a private project, need it.

const gitlabUsage = `usage: tend gitlab login [host]    keep a token for a GitLab (gitlab.com by default)
       tend gitlab logout [host]   forget it
       tend gitlab status          which hosts have one, and whom it signs in as`

func runGitLab(args []string) error {
	if len(args) == 0 {
		return errors.New(gitlabUsage)
	}
	host := func(rest []string) string {
		if len(rest) > 0 && rest[0] != "" {
			return strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(rest[0], "https://"), "http://"), "/"))
		}
		return "gitlab.com"
	}
	switch args[0] {
	case "login":
		fs := flag.NewFlagSet("gitlab login", flag.ExitOnError)
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return gitlabLogin(host(fs.Args()))
	case "logout":
		return gitlabLogout(host(args[1:]))
	case "status":
		return gitlabStatus()
	}
	return errors.New(gitlabUsage)
}

// gitlabLogin asks for a token — hidden, on a terminal; a line, from a
// pipe — checks whom it signs in as, and keeps it only then.
func gitlabLogin(host string) error {
	var token string
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintf(os.Stderr, "a token for %s (in GitLab: Preferences → Access tokens, with the api scope): ", host)
		raw, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return err
		}
		token = string(raw)
	} else {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return errors.New("no token on standard input")
		}
		token = line
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("no token given")
	}
	who, err := github.GitLabWhoAmI(host, token)
	if err != nil {
		return fmt.Errorf("%s did not take the token: %w", host, err)
	}
	value, where := token, "the settings file (readable by you alone)"
	if keyring.Available() {
		name := "gitlab:" + host
		if err := keyring.Set(name, token); err == nil {
			value, where = keyring.Ref(name), "the system keyring"
		}
	}
	if err := config.Set("gitlab.tokens", strconv.Quote(host), config.Quote(value)); err != nil {
		return err
	}
	fmt.Printf("signed in to %s as %s; the token is in %s\n", host, who, where)
	return nil
}

func gitlabLogout(host string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	value, ok := cfg.GitLab.Tokens[host]
	if !ok || value == "" {
		fmt.Printf("no token was kept for %s\n", host)
		return nil
	}
	if name, isRef := keyring.Name(value); isRef {
		_ = keyring.Delete(name)
	}
	if err := config.Set("gitlab.tokens", strconv.Quote(host), config.Quote("")); err != nil {
		return err
	}
	fmt.Printf("forgot the token for %s\n", host)
	return nil
}

func gitlabStatus() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	var hosts []string
	for h, v := range cfg.GitLab.Tokens {
		if v != "" {
			hosts = append(hosts, h)
		}
	}
	sort.Strings(hosts)
	if len(hosts) == 0 {
		fmt.Println("no GitLab token kept (tend gitlab login keeps one)")
	}
	for _, h := range hosts {
		who, err := github.GitLabWhoAmI(h, github.GitLabToken(h))
		if err != nil {
			fmt.Printf("%s  the token kept does not work: %v\n", h, err)
			continue
		}
		fmt.Printf("%s  signed in as %s\n", h, who)
	}
	if os.Getenv("GITLAB_TOKEN") != "" {
		fmt.Println("GITLAB_TOKEN is set, and is used where no host has a token of its own")
	}
	return nil
}
