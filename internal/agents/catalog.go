package agents

import "github.com/auth-com-br/tend/internal/integration"

// Catalog is every agent tend offers, in the order the manager lists them.
//
// Each install command is the one its vendor documents, read from the page
// in Source on 2026-09-23; an agent whose install could not be confirmed
// from its vendor has no Methods, and is listed as found or not with no
// offer to install it. When a vendor changes its instructions, this is the
// one place to follow them.
func Catalog() []Definition {
	return []Definition{
		{
			ID: "claude", Name: "Claude Code", Description: "Anthropic's coding agent",
			Binaries: binariesOf(integration.TargetClaude),
			Methods:  methods["claude"],
		},
		{
			ID: "codex", Name: "Codex", Description: "OpenAI's coding agent",
			Binaries: binariesOf(integration.TargetCodex),
			Methods:  methods["codex"],
		},
		{
			ID: "gemini", Name: "Gemini CLI", Description: "Google's coding agent",
			Binaries: []string{"gemini"},
			Methods:  methods["gemini"],
		},
		{
			ID: "opencode", Name: "OpenCode", Description: "open source coding agent",
			Binaries: binariesOf(integration.TargetOpencode),
			Methods:  methods["opencode"],
		},
		{
			ID: "copilot", Name: "GitHub Copilot CLI", Description: "GitHub's coding agent",
			Binaries: binariesOf(integration.TargetCopilot),
			Methods:  methods["copilot"],
		},
		{
			ID: "qwen", Name: "Qwen Code", Description: "Alibaba's coding agent",
			Binaries: binariesOf(integration.TargetQwen),
			Methods:  methods["qwen"],
		},
		{
			ID: "cursor", Name: "Cursor CLI", Description: "Cursor's agent in the terminal",
			Binaries: binariesOf(integration.TargetCursor),
			Methods:  methods["cursor"],
		},
		{
			ID: "amp", Name: "Amp", Description: "Sourcegraph's coding agent",
			Binaries: []string{"amp"}, VersionArgs: []string{"version"},
			Methods: methods["amp"],
		},
		{
			ID: "droid", Name: "Droid", Description: "Factory's coding agent",
			Binaries: binariesOf(integration.TargetDroid),
			Methods:  methods["droid"],
		},
		{
			ID: "kimi", Name: "Kimi CLI", Description: "Moonshot's coding agent",
			Binaries: binariesOf(integration.TargetKimi),
			Methods:  methods["kimi"],
		},
		{
			ID: "kilo", Name: "Kilo Code CLI", Description: "Kilo's coding agent",
			Binaries: binariesOf(integration.TargetKilo),
			Methods:  methods["kilo"],
		},
		{
			ID: "letta", Name: "Letta Code", Description: "Letta's memory-first coding agent",
			Binaries: binariesOf(integration.TargetLetta),
			Methods:  methods["letta"],
		},
		{
			ID: "qodercli", Name: "Qoder CLI", Description: "Qoder's coding agent",
			// The docs run it as qoder; the package and its binary are qodercli.
			Binaries: append(binariesOf(integration.TargetQoderCLI), "qoder"),
			Methods:  methods["qodercli"],
		},
		{
			ID: "grok", Name: "Grok CLI", Description: "community agent on xAI's Grok (not xAI's own)",
			Binaries: binariesOf(integration.TargetGrok),
			Methods:  methods["grok"],
		},
		{
			ID: "pi", Name: "Pi", Description: "a minimal coding agent",
			Binaries: binariesOf(integration.TargetPi),
			Methods:  methods["pi"],
		},
		{
			ID: "aider", Name: "Aider", Description: "AI pair programming in the terminal",
			Binaries: []string{"aider"},
			Methods:  methods["aider"],
		},

		// Memory tools (docs/MEMORY.md): not agents, but what gives them
		// memory across sessions and across one another. Offered, not
		// rebuilt.
		{
			ID: "ai-memory", Name: "ai-memory", Kind: "memory",
			Description: "what happened, for every agent: hooks write a wiki per project, read back over MCP",
			Binaries:    []string{"ai-memory"},
			Port:        49374,
			Methods:     methods["ai-memory"],
			Next:        "give it to your agents: ai-memory install-mcp --client claude-code --apply && ai-memory install-hooks --agent claude-code --apply (codex likewise)",
		},
		{
			ID: "graphify", Name: "Graphify", Kind: "memory",
			Description: "a graph of the code, built offline, for agents to query instead of reading every file",
			Binaries:    []string{"graphify"},
			Methods:     methods["graphify"],
			Next:        "in a project, build its graph with /graphify . in Claude Code",
		},
	}
}

// methods are each agent's install methods, by ID (install.go).
var methods map[string][]Method
