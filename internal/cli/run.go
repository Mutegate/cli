package cli

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
)

// run starts a tool with its traffic going through the gateway, for that
// process only: nothing on the machine changes, and the tool's own settings
// stay as they were.
func run(env *Env, args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	agent := fs.String("agent", "", "the agent's name on incidents and costs (default: the command's name)")
	session := fs.String("session", "", "this run's name (default: a new one each time)")
	fs.Usage = func() {
		fmt.Fprintln(env.Stderr, "usage: mutegate run [--agent name] [--session id] -- <command> [args…]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return exitCode(2)
	}
	c, err := env.loadSignedIn()
	if err != nil {
		return err
	}
	command := fs.Args()
	a := *agent
	if a == "" {
		a = filepath.Base(command[0])
	}
	s := *session
	if s == "" {
		s = newSession(a)
	}

	path, err := exec.LookPath(command[0])
	if err != nil {
		return err
	}
	cmd := exec.Command(path, command[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = env.Stdin, env.Stdout, env.Stderr
	cmd.Env = runEnv(env.Environ(), c, a, s, filepath.Base(command[0]))

	// Ctrl-C reaches the whole foreground group, the tool included; the tool
	// decides what it means, and this process waits to pass its answer on.
	signal.Ignore(os.Interrupt)
	defer signal.Reset(os.Interrupt)
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exitCode(exit.ExitCode())
		}
		return err
	}
	return nil
}

func newSession(agent string) string {
	b := make([]byte, 3)
	rand.Read(b)
	return agent + "-" + hex.EncodeToString(b)
}

// runEnv is the tool's environment: the caller's, with every variable the
// common tools and SDKs read to find a provider pointed at the gateway
// instead. The provider keys among them are replaced, so a real one in the
// caller's shell never goes anywhere near the gateway or the tool.
func runEnv(base []string, c Config, agent, session, command string) []string {
	headers := "X-Mutegate-Agent: " + agent + "\nX-Mutegate-Session: " + session
	set := map[string]string{
		// For configurations that name them: Codex's env_key and
		// env_http_headers, OpenCode's {env:…}.
		"MUTEGATE_URL":     c.URL,
		"MUTEGATE_API_KEY": c.Key,
		"MUTEGATE_AGENT":   agent,
		"MUTEGATE_SESSION": session,

		// OpenAI SDKs, Codex's built-in provider, LangChain; LiteLLM and
		// Aider read OPENAI_API_BASE.
		"OPENAI_BASE_URL":       c.URL + "/v1",
		"OPENAI_API_BASE":       c.URL + "/v1",
		"OPENAI_API_KEY":        c.Key,
		"OPENAI_CUSTOM_HEADERS": headers,

		// Anthropic SDKs and Claude Code; LiteLLM and Aider read
		// ANTHROPIC_API_BASE. No /v1: these clients add it.
		"ANTHROPIC_BASE_URL":       c.URL,
		"ANTHROPIC_API_BASE":       c.URL,
		"ANTHROPIC_CUSTOM_HEADERS": headers,
	}
	// Claude Code takes the key as a bearer token without asking to approve
	// it, and warns when it finds both variables; everything else reads
	// ANTHROPIC_API_KEY.
	unset := map[string]bool{}
	if command == "claude" {
		set["ANTHROPIC_AUTH_TOKEN"] = c.Key
		unset["ANTHROPIC_API_KEY"] = true
	} else {
		set["ANTHROPIC_API_KEY"] = c.Key
		unset["ANTHROPIC_AUTH_TOKEN"] = true
	}

	out := make([]string, 0, len(base)+len(set))
	for _, kv := range base {
		name, _, _ := strings.Cut(kv, "=")
		if _, replaced := set[name]; replaced || unset[name] {
			continue
		}
		out = append(out, kv)
	}
	for name, value := range set {
		out = append(out, name+"="+value)
	}
	return out
}
