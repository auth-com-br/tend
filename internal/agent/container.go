package agent

import (
	"path"
	"strings"
)

// An agent run in a container — `docker exec -it box claude`, `podman run
// … codex`, `kubectl exec pod -- claude` — is a pane whose foreground
// program is docker, and whose name says nothing about the agent (herdr#699
// asks for it; herdr reads only runtimes and shells, issue #53). The agent
// is the command after the container: read out of the arguments, as a
// runtime's script is, and looked up the same way.

// containerTools are the programs that run a command in a container.
var containerTools = map[string]bool{"docker": true, "podman": true, "nerdctl": true, "kubectl": true}

// IsContainerTool reports whether a process name runs commands in
// containers.
func IsContainerTool(name string) bool { return containerTools[lookupName(path.Base(name))] }

// Options of docker/podman exec and run that take a value, so the word
// after them is not the container, the image or the command.
var containerValueFlags = map[string]bool{
	"-e": true, "--env": true, "--env-file": true, "-u": true, "--user": true,
	"-w": true, "--workdir": true, "--detach-keys": true, "-v": true, "--volume": true,
	"-p": true, "--publish": true, "--name": true, "--network": true, "--net": true,
	"--entrypoint": true, "--mount": true, "-h": true, "--hostname": true,
	"--label": true, "-l": true, "--platform": true, "--pull": true, "--restart": true,
	"--cpus": true, "-m": true, "--memory": true, "--add-host": true, "--device": true,
	"--cap-add": true, "--cap-drop": true, "--security-opt": true, "--ulimit": true,
	"--group-add": true, "--ipc": true, "--pid": true, "--log-driver": true,
	"--log-opt": true, "--shm-size": true, "--tmpfs": true, "--gpus": true,
	"--runtime": true, "--stop-signal": true, "--volumes-from": true, "--userns": true,
	"--index": true, "-n": true, "--namespace": true, "-c": true, "--container": true,
	"--context": true, "--kubeconfig": true, "-f": true, "--file": true, "--project-name": true,
}

// ContainerAgentName is the program a container tool runs, as an agent is
// looked up by: `docker exec -it box claude` is "claude", and one run
// through a shell (`docker exec box sh -c "claude --resume"`) is read
// through it as WrappedAgentName reads a shell. "" when there is none.
func ContainerAgentName(argv []string) string {
	// A tool that is a script — podman-docker's docker is one — runs as its
	// interpreter with the script after it: /bin/sh /usr/bin/docker exec ….
	if len(argv) > 2 && !IsContainerTool(argv[0]) && IsContainerTool(argv[1]) {
		argv = argv[1:]
	}
	if len(argv) < 2 || !IsContainerTool(argv[0]) {
		return ""
	}
	tool := lookupName(path.Base(argv[0]))
	args := argv[1:]
	// The tool's own options before its subcommand: docker --context x,
	// kubectl -n ns.
	args = skipOptions(args)
	if len(args) == 0 {
		return ""
	}
	sub := args[0]
	args = args[1:]
	if tool != "kubectl" && (sub == "compose" || sub == "container") {
		// docker compose exec SERVICE cmd; docker container exec CTR cmd.
		args = skipOptions(args)
		if len(args) == 0 {
			return ""
		}
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "exec", "run":
	default:
		return ""
	}
	if tool == "kubectl" {
		// kubectl exec POD -- command: the command is what follows --.
		for i, a := range args {
			if a == "--" && i+1 < len(args) {
				return commandName(args[i+1:])
			}
		}
		return ""
	}
	args = skipOptions(args)
	if len(args) < 2 {
		return "" // a container or image, and no command: the image's own
	}
	return commandName(args[1:])
}

// skipOptions drops leading options, and the values of those that take one.
func skipOptions(args []string) []string {
	for len(args) > 0 {
		a := args[0]
		if a == "--" {
			return args[1:]
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			return args
		}
		args = args[1:]
		if !strings.Contains(a, "=") && containerValueFlags[a] && len(args) > 0 {
			args = args[1:]
		}
	}
	return args
}

// commandName is the agent a command line names, read through a shell.
func commandName(cmd []string) string {
	if len(cmd) == 0 {
		return ""
	}
	if n := lookupName(path.Base(cmd[0])); n == "sh" || n == "bash" || n == "zsh" || n == "fish" {
		for i, a := range cmd[1:] {
			if a == "-c" || (strings.HasPrefix(a, "-") && strings.Contains(a, "c")) {
				if i+2 < len(cmd) {
					if fields := strings.Fields(cmd[i+2]); len(fields) > 0 {
						return pathTokenName(fields[0])
					}
				}
				return ""
			}
		}
		return WrappedAgentName(cmd[0], cmd)
	}
	if IsRuntimeOrShell(cmd[0]) {
		return WrappedAgentName(cmd[0], cmd)
	}
	return pathTokenName(cmd[0])
}
