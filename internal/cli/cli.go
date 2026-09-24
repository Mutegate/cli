// Package cli is the command line for the people whose agents go through a
// Mutegate gateway: sign in to it once, check it answers, and run or
// configure a tool so its traffic goes there. The gateway itself is
// github.com/danilovid/mutegate; this talks to it over its public API only.
package cli

import (
	"fmt"
	"io"
	"os"
)

// commands are the words that select the command line rather than the server.
var commands = map[string]func(env *Env, args []string) error{
	"login":   login,
	"logout":  logout,
	"status":  status,
	"run":     run,
	"connect": connect,
}

// Env is what a command reads and writes: the process's own, or a test's.
type Env struct {
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	Getenv  func(string) string
	Environ func() []string
	// Home is where the tools keep their configuration (~).
	Home string
	// ConfigDir holds the command line's own settings (~/.config/mutegate).
	ConfigDir string
	Version   string
}

// ProcessEnv is the environment of this process.
func ProcessEnv(version string) (*Env, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = home + "/.config"
	}
	return &Env{
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
		Getenv: os.Getenv, Environ: os.Environ,
		Home: home, ConfigDir: base + "/mutegate", Version: version,
	}, nil
}

// Main runs one command and returns the process's exit code.
func Main(env *Env, args []string) int {
	if len(args) == 0 {
		fmt.Fprint(env.Stdout, usage)
		return 0
	}
	switch args[0] {
	case "help", "-h", "--help":
		fmt.Fprint(env.Stdout, usage)
		return 0
	case "version", "--version":
		fmt.Fprintln(env.Stdout, "mutegate", env.Version)
		return 0
	}
	cmd, ok := commands[args[0]]
	if !ok {
		fmt.Fprintf(env.Stderr, "mutegate: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
	if err := cmd(env, args[1:]); err != nil {
		if code, ok := err.(exitCode); ok {
			return int(code)
		}
		fmt.Fprintln(env.Stderr, "mutegate:", err)
		return 1
	}
	return 0
}

// exitCode passes a child process's exit status through unchanged.
type exitCode int

func (e exitCode) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

const usage = `mutegate — point your AI agents at a Mutegate gateway.

  mutegate login [address]     sign in to a gateway with an API key
  mutegate logout              forget the key
  mutegate status              check the gateway answers and the key works
  mutegate run -- <command>    run a tool with its traffic going through the gateway
  mutegate connect <tool>      write a tool's configuration (claude-code, codex)
  mutegate version

The key comes from the gateway's console, Settings → API keys. MUTEGATE_URL
and MUTEGATE_API_KEY, when set, take the place of what login saved.

The gateway: https://github.com/danilovid/mutegate
`
