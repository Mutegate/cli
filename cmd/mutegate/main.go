// Command mutegate points AI agents at a Mutegate gateway: mutegate login,
// status, run -- <command> and connect <tool>.
package main

import (
	"fmt"
	"os"

	"github.com/danilovid/mutegate-cli/internal/cli"
)

// version is stamped at release time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	env, err := cli.ProcessEnv(version)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mutegate:", err)
		os.Exit(1)
	}
	os.Exit(cli.Main(env, os.Args[1:]))
}
