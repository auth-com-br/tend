package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/transport"
	"github.com/auth-com-br/tend/internal/update"
)

// `tend update` and `tend channel`, herdr's two update commands.
//
// Nothing here runs on its own. The server checks in the background and
// says when a release is ready, as herdr's does (server/updatecheck.go);
// downloading and installing wait for somebody to run this command.

func runUpdate(args []string) error {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	check := fs.Bool("check", false, "say what is published and stop")
	handoff := fs.Bool("handoff", false, "after installing, move every running session onto the new build, keeping its programs")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(),
			"usage: tend update [-check]\n\n"+
				"downloads the build published on the configured channel and puts it in\n"+
				"place of this one. the download is checked against the manifest's\n"+
				"checksum before anything is installed.\n\n"+
				"the stable channel is published with each release on github; a\n"+
				"preview channel needs its manifest set in the settings file:\n\n"+
				"  [update]\n"+
				"  channel = \"preview\"\n"+
				"  preview = \"https://…/preview.json\"\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	url := cfg.ManifestURL()
	if url == "" {
		return fmt.Errorf("no manifest for the %q channel; set [update] manifest in the settings file",
			channelName(cfg))
	}

	manifest, err := update.Fetch(url)
	if err != nil {
		return err
	}
	release, err := update.ReleaseFor(manifest)
	if err != nil {
		return err
	}

	if !offered(release, version) {
		fmt.Fprintf(os.Stderr, "%s already on %s (%s)\n", tag(), release.Version, channelName(cfg))
		return nil
	}
	fmt.Fprintf(os.Stderr, "%s %s publishes %s; this is %s\n", tag(), channelName(cfg), release.Version, version)
	if release.Notes != "" {
		fmt.Println(release.Notes)
	}
	if *check {
		return nil
	}

	self, err := os.Executable()
	if err != nil {
		return err
	}
	if m, ok := update.ManagerOf(self); ok {
		return update.ErrManaged(m)
	}
	downloaded, err := update.Download(release, self)
	if err != nil {
		if errors.Is(err, update.ErrChecksum) {
			// Worth saying plainly: it is the one failure that means the
			// bytes served were not the bytes published.
			return fmt.Errorf("%w — nothing was installed", err)
		}
		return err
	}
	if err := update.Install(downloaded, self); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%s installed %s at %s\n", tag(), release.Version, self)
	if !*handoff {
		fmt.Fprintf(os.Stderr, "%s run \"tend handoff\" (or \"tend update -handoff\") to move running sessions onto it\n", tag())
		return nil
	}
	return handoffAll(self)
}

// handoffAll moves every running session onto the binary now installed, as
// herdr's `update --handoff` does. A server too old to hand off is named and
// left running: replacing it means a restart, which ends its programs, and
// that is the user's decision, not an updater's.
func handoffAll(binary string) error {
	names, err := transport.Sessions()
	if err != nil {
		return err
	}
	sort.Strings(names)
	var failed []string
	for _, name := range names {
		c, err := connect(name, nil)
		if err != nil {
			continue // a socket left by a crash, not a running server
		}
		// The path it was installed at, not os.Executable now: on Linux
		// that follows this process's file to where the install moved it.
		err = c.HandoffTo(binary)
		_ = c.Close()
		switch {
		case err == nil:
			fmt.Fprintf(os.Stderr, "%s session %q is on the new build, with its panes\n", tag(), name)
		case errors.Is(err, proto.ErrUnknownMethod):
			fmt.Fprintf(os.Stderr, "%s session %q runs a server from before handoff; it is left as it is (tend kill -s %s -server restarts it, ending its programs)\n",
				tag(), name, name)
		default:
			fmt.Fprintf(os.Stderr, "%s session %q: %v\n", tag(), name, err)
			failed = append(failed, name)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("handoff failed for %s; those servers carry on on the old build", strings.Join(failed, ", "))
	}
	return nil
}

// runChannel shows or sets which channel updates come from.
func runChannel(args []string) error {
	fs := flag.NewFlagSet("channel", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(),
			"usage: tend channel [stable|preview]\n\n"+
				"with no argument, says which channel this tend follows.\n\n")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if fs.NArg() == 0 {
		url := cfg.ManifestURL()
		if url == "" {
			url = "(no manifest configured)"
		}
		fmt.Fprintf(os.Stderr, "%s channel %s · %s\n", tag(), channelName(cfg), url)
		return nil
	}

	channel := update.Channel(fs.Arg(0))
	if !channel.Valid() {
		return fmt.Errorf("%q is not a channel; use stable or preview", fs.Arg(0))
	}
	if self, err := os.Executable(); err == nil && channel == update.Preview {
		// herdr's preview_channel_rejection: a package manager publishes
		// releases, not previews.
		if m, ok := update.ManagerOf(self); ok {
			return fmt.Errorf("the preview channel is for tend installed on its own; %s installs update through `%s`", m.Name, m.Command)
		}
	}
	if err := config.Set("update", "channel", config.Quote(string(channel))); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%s channel %s\n", tag(), channel)
	return nil
}

func channelName(cfg config.Config) string {
	if cfg.Update.Channel == "preview" {
		return "preview"
	}
	return "stable"
}

// offered is whether a published release is one to install over this build:
// a newer one, as herdr decides, when this build is a release; any other
// one when it is not, since a build from a working tree has no place in
// the order and whoever runs it asked for the published one.
func offered(release update.Release, running string) bool {
	if _, ok := update.ParseVersion(running); ok {
		return update.Newer(release.Version, running)
	}
	return update.Differs(release, running)
}
