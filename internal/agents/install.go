package agents

// The install methods, each as its vendor's own page gives it, read on
// 2026-09-23; the recommended one first, then the others the page lists
// that run without asking which package manager is wanted. A script piped
// to a shell needs curl (and the shell it names); an npm package needs npm;
// and so on. The manager offers the first method whose program is here.
//
// Two are not a vendor's own: Grok CLI (superagent-ai/grok-cli) is a
// community project on xAI's model, which its README says; and Cursor's
// installer names the program `agent` now, which is also left as
// `cursor-agent`, the name found.

func init() {
	const (
		claudeDocs   = "https://code.claude.com/docs/en/setup"
		codexDocs    = "https://github.com/openai/codex"
		geminiDocs   = "https://github.com/google-gemini/gemini-cli"
		opencodeDocs = "https://github.com/anomalyco/opencode"
		copilotDocs  = "https://github.com/github/copilot-cli"
		qwenDocs     = "https://github.com/QwenLM/qwen-code"
		cursorDocs   = "https://cursor.com/docs/cli/installation"
		ampDocs      = "https://ampcode.com/docs/cli"
		droidDocs    = "https://docs.factory.ai/cli/getting-started/quickstart"
		kimiDocs     = "https://github.com/MoonshotAI/kimi-code"
		kiloDocs     = "https://github.com/Kilo-Org/kilocode"
		lettaDocs    = "https://github.com/letta-ai/letta-code"
		qoderDocs    = "https://docs.qoder.com/cli/installation"
		grokDocs     = "https://github.com/superagent-ai/grok-cli"
		piDocs       = "https://github.com/earendil-works/pi"
		aiderDocs    = "https://aider.chat/docs/install.html"
		memoryDocs   = "https://github.com/akitaonrails/ai-memory"
		graphDocs    = "https://github.com/safishamsi/graphify"
	)
	script := func(cmd, source string) Method {
		return Method{Kind: "script", Needs: "curl", Command: cmd, Source: source}
	}
	npm := func(pkg, source string) Method {
		return Method{Kind: "npm", Needs: "npm", Command: "npm install -g " + pkg, Source: source}
	}
	brew := func(args, source string) Method {
		return Method{Kind: "brew", Needs: "brew", Command: "brew install " + args, Source: source}
	}

	methods = map[string][]Method{
		"claude": {
			script("curl -fsSL https://claude.ai/install.sh | bash", claudeDocs),
			brew("--cask claude-code", claudeDocs),
			npm("@anthropic-ai/claude-code", claudeDocs),
		},
		"codex": {
			script("curl -fsSL https://chatgpt.com/codex/install.sh | sh", codexDocs),
			npm("@openai/codex", codexDocs),
			brew("--cask codex", codexDocs),
		},
		"gemini": {
			npm("@google/gemini-cli", geminiDocs),
			brew("gemini-cli", geminiDocs),
		},
		"opencode": {
			script("curl -fsSL https://opencode.ai/install | bash", opencodeDocs),
			npm("opencode-ai@latest", opencodeDocs),
			brew("anomalyco/tap/opencode", opencodeDocs),
		},
		"copilot": {
			script("curl -fsSL https://gh.io/copilot-install | bash", copilotDocs),
			brew("copilot-cli", copilotDocs),
			npm("@github/copilot", copilotDocs),
		},
		"qwen": {
			script("curl -fsSL https://qwen-code-assets.oss-cn-hangzhou.aliyuncs.com/installation/install-qwen-standalone.sh | bash", qwenDocs),
			npm("@qwen-code/qwen-code@latest", qwenDocs),
			brew("qwen-code", qwenDocs),
		},
		"cursor": {
			script("curl https://cursor.com/install -fsS | bash", cursorDocs),
		},
		"amp": {
			script("curl -fsSL https://ampcode.com/install.sh | bash", ampDocs),
		},
		"droid": {
			script("curl -fsSL https://app.factory.ai/cli | sh", droidDocs),
			brew("--cask droid", droidDocs),
			npm("droid", droidDocs),
		},
		"kimi": {
			script("curl -fsSL https://code.kimi.com/kimi-code/install.sh | bash", kimiDocs),
			brew("kimi-code", kimiDocs),
			npm("@moonshot-ai/kimi-code", kimiDocs),
		},
		"kilo": {
			npm("@kilocode/cli", kiloDocs),
			script("curl -fsSL https://kilo.ai/cli/install | bash", kiloDocs),
			brew("Kilo-Org/tap/kilo", kiloDocs),
		},
		"letta": {
			npm("@letta-ai/letta-code", lettaDocs),
		},
		"qodercli": {
			script("curl -fsSL https://qoder.com/install | bash", qoderDocs),
		},
		"grok": {
			script("curl -fsSL https://raw.githubusercontent.com/superagent-ai/grok-cli/main/install.sh | bash", grokDocs),
		},
		"pi": {
			npm("--ignore-scripts @earendil-works/pi-coding-agent", piDocs),
			script("curl -fsSL https://pi.dev/install.sh | sh", piDocs),
		},
		"aider": {
			{Kind: "pip", Needs: "python", Command: "python -m pip install aider-install && aider-install", Source: aiderDocs},
			script("curl -LsSf https://aider.chat/install.sh | sh", aiderDocs),
		},
		// Its README's Docker quick start, without the optional model keys:
		// it works with no model at all, and a key is the user's to add.
		"ai-memory": {
			{Kind: "aur", Needs: "yay", Command: "yay -S ai-memory-bin", Source: memoryDocs},
			{Kind: "docker", Needs: "docker", Command: "docker run -d --name ai-memory --restart unless-stopped -p 127.0.0.1:49374:49374 -v ai-memory-data:/data docker.io/akitaonrails/ai-memory:latest", Source: memoryDocs},
		},
		// The PyPI package is graphifyy while the name graphify is reclaimed;
		// the program is graphify, and graphify install registers its skill.
		"graphify": {
			{Kind: "pipx", Needs: "pipx", Command: "pipx install graphifyy && graphify install", Source: graphDocs},
			{Kind: "uv", Needs: "uv", Command: "uv tool install graphifyy && graphify install", Source: graphDocs},
			{Kind: "pip", Needs: "pip", Command: "pip install graphifyy && graphify install", Source: graphDocs},
		},
	}
}
