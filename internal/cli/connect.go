package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// connect writes a tool's configuration so it goes through the gateway
// whenever it starts, not only under `mutegate run`. Only files are written;
// a tool configured in a settings window gets a link to its guide instead.
func connect(env *Env, args []string) error {
	fs := flag.NewFlagSet("connect", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(env.Stderr, "usage: mutegate connect <tool>   (claude-code, codex; any other tool gets a link to its guide)")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return exitCode(2)
	}
	c, err := env.loadSignedIn()
	if err != nil {
		return err
	}
	switch tool := fs.Arg(0); tool {
	case "claude-code", "claude":
		return connectClaudeCode(env, c)
	case "codex":
		return connectCodex(env, c)
	default:
		// The gateway's address is not always where its console is, so the
		// values come here and the guide from the repository.
		fmt.Fprintf(env.Stdout, "%s is set up in its own settings. Use:\n", tool)
		fmt.Fprintf(env.Stdout, "  base URL   %s/v1   (Anthropic-style settings: %s)\n", c.URL, c.URL)
		fmt.Fprintf(env.Stdout, "  API key    the one you signed in with, %s\n", maskKey(c.Key))
		fmt.Fprintf(env.Stdout, "The guide: %s\n", guideURL(tool))
		return nil
	}
}

// guideURL is a tool's section of docs/CONNECT.md, or the top of it.
func guideURL(tool string) string {
	const doc = "https://github.com/Mutegate/mutegate/blob/main/docs/CONNECT.md"
	anchors := map[string]string{
		"cursor": "cursor", "cline": "cline", "continue": "continue", "aider": "aider",
		"opencode": "opencode", "langchain": "langchain",
	}
	if a, ok := anchors[tool]; ok {
		return doc + "#" + a
	}
	return doc
}

// connectClaudeCode sets the gateway in the env block of Claude Code's user
// settings, leaving everything else in the file as it was.
func connectClaudeCode(env *Env, c Config) error {
	path := filepath.Join(env.Home, ".claude", "settings.json")
	settings := map[string]any{}
	old, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		old = nil
	case err != nil:
		return err
	default:
		if len(bytes.TrimSpace(old)) > 0 {
			if err := json.Unmarshal(old, &settings); err != nil {
				return fmt.Errorf("%s is not plain JSON (%v); not touching it — set the variables by hand: %s/connect/claude-code", path, err, c.URL)
			}
		}
	}
	envBlock, _ := settings["env"].(map[string]any)
	if envBlock == nil {
		envBlock = map[string]any{}
	}
	envBlock["ANTHROPIC_BASE_URL"] = c.URL
	envBlock["ANTHROPIC_AUTH_TOKEN"] = c.Key
	envBlock["ANTHROPIC_CUSTOM_HEADERS"] = "X-Mutegate-Agent: claude-code"
	// A key of its own here would override the gateway's; it would also be
	// the provider's, which is exactly what should not be in this file now.
	delete(envBlock, "ANTHROPIC_API_KEY")
	settings["env"] = envBlock

	b, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	if err := writeWithBackup(path, old, append(b, '\n')); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "Claude Code now goes through %s (%s).\n", c.URL, path)
	fmt.Fprintln(env.Stdout, "The key is in that file now; `mutegate run -- claude` is the way that leaves no key behind.")
	if old != nil {
		fmt.Fprintf(env.Stdout, "The previous file is at %s.mutegate-backup.\n", path)
	}
	return nil
}

var (
	tomlTable         = regexp.MustCompile(`^\s*\[`)
	tomlModelProvider = regexp.MustCompile(`^\s*model_provider\s*=`)
)

// connectCodex adds the gateway to Codex as a model provider and makes it the
// default. Codex reads the key from MUTEGATE_API_KEY — which `mutegate run`
// sets — so no key is written into the file.
func connectCodex(env *Env, c Config) error {
	path := filepath.Join(env.Home, ".codex", "config.toml")
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if errors.Is(err, os.ErrNotExist) {
		old = nil
	}
	b := codexConfig(string(old), c.URL)
	if err := writeWithBackup(path, old, []byte(b)); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "Codex now uses the gateway at %s as its model provider (%s).\n", c.URL, path)
	if old != nil {
		fmt.Fprintf(env.Stdout, "The previous file is at %s.mutegate-backup.\n", path)
	}
	fmt.Fprintln(env.Stdout, "Start it with: mutegate run -- codex   (or export MUTEGATE_API_KEY yourself)")
	return nil
}

// codexConfig edits Codex's config.toml as text, so its comments and layout
// survive: model_provider is set at the top level, and the provider's table
// is replaced if it is there and appended if it is not.
func codexConfig(old, url string) string {
	table := strings.Join([]string{
		"[model_providers.mutegate]",
		`name = "Mutegate"`,
		fmt.Sprintf("base_url = %q", url+"/v1"),
		`env_key = "MUTEGATE_API_KEY"`,
		`wire_api = "responses"`,
		`http_headers = { "X-Mutegate-Agent" = "codex" }`,
		`env_http_headers = { "X-Mutegate-Session" = "MUTEGATE_SESSION" }`,
	}, "\n")

	lines := strings.Split(strings.TrimRight(old, "\n"), "\n")
	if old == "" {
		lines = nil
	}

	var out []string
	setProvider := false
	inTop := true     // before the first table: where top-level keys live
	skipping := false // inside an old [model_providers.mutegate]
	for _, l := range lines {
		if tomlTable.MatchString(l) {
			inTop = false
			skipping = strings.TrimSpace(l) == "[model_providers.mutegate]"
			if skipping {
				continue
			}
		}
		if skipping {
			continue
		}
		if inTop && tomlModelProvider.MatchString(l) {
			out = append(out, `model_provider = "mutegate"`)
			setProvider = true
			continue
		}
		out = append(out, l)
	}
	if !setProvider {
		out = append([]string{`model_provider = "mutegate"`}, out...)
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n") + "\n\n" + table + "\n"
}

// writeWithBackup keeps the file as it was next to it, then replaces it,
// with the permissions it had — or private ones for a new file, since these
// files end up holding keys.
func writeWithBackup(path string, old, data []byte) error {
	perm := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if old != nil {
		if err := os.WriteFile(path+".mutegate-backup", old, perm); err != nil {
			return err
		}
	}
	return writeFileAtomic(path, data, perm)
}
