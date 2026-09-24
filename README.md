# mutegate

The command line for [Mutegate](https://github.com/danilovid/mutegate), a
self-hosted DLP gateway for AI agents: sign in to your gateway once, then run
any tool with its traffic going through it.

```bash
mutegate login mutegate.example.com
mutegate run -- claude
```

Nothing on the machine changes, no key is written into the tool's settings, and
every run is its own session in the gateway's incident feed and cost figures.

## Install

Homebrew (macOS and Linux):

```bash
brew install --cask mutegate/tap/mutegate
```

Scoop (Windows):

```bash
scoop bucket add mutegate https://github.com/Mutegate/scoop-bucket
scoop install mutegate/mutegate
```

Or download the archive for your system from the
[releases](https://github.com/Mutegate/cli/releases) — Linux, macOS and
Windows, amd64 and arm64 — or build it:

```bash
go install github.com/mutegate/cli/cmd/mutegate@latest
```

## Sign in

Create a key in your gateway's console, under **Settings → API keys**, then:

```bash
mutegate login mutegate.example.com   # https:// is assumed; localhost gets http://
mutegate status
```

The key is read without echoing it, or from stdin when piped
(`pass show mutegate | mutegate login …`). It is checked against the gateway
before it is saved to `~/.config/mutegate/cli.json`, readable by you only.
`status` checks it again and says which providers answer.

In CI, skip the login: `MUTEGATE_URL` and `MUTEGATE_API_KEY` take the place of
what it would save.

## Run a tool through the gateway

```bash
mutegate run -- claude
mutegate run -- aider --model openai/gpt-5
mutegate run --agent nightly-report -- python report.py
```

`run` starts the command with the variables the common tools and SDKs read to
find their provider pointed at the gateway — for that process only. Provider
keys already in your shell are replaced, so a real one never reaches the tool.
The tool's exit code is `run`'s exit code.

Incidents and costs are attributed to the agent (the command's name, or
`--agent`) and to a session made up for this run (or `--session`), wherever the
tool sends headers — Claude Code and the OpenAI and Anthropic SDKs do.

| Variable | Value |
|----------|-------|
| `OPENAI_BASE_URL`, `OPENAI_API_BASE` | the gateway, with `/v1` |
| `OPENAI_API_KEY` | your key |
| `ANTHROPIC_BASE_URL`, `ANTHROPIC_API_BASE` | the gateway |
| `ANTHROPIC_AUTH_TOKEN` for `claude`, `ANTHROPIC_API_KEY` otherwise | your key |
| `OPENAI_CUSTOM_HEADERS`, `ANTHROPIC_CUSTOM_HEADERS` | `X-Mutegate-Agent` and `X-Mutegate-Session` |
| `MUTEGATE_URL`, `MUTEGATE_API_KEY`, `MUTEGATE_AGENT`, `MUTEGATE_SESSION` | for configurations that name them, like Codex's after `connect codex` |

## Configure a tool for good

```bash
mutegate connect claude-code
mutegate connect codex
```

- **claude-code** sets the gateway in the `env` block of
  `~/.claude/settings.json` and leaves the rest of the file alone. The key is
  written into that file; `mutegate run -- claude` is the way that leaves no
  key behind.
- **codex** adds the gateway to `~/.codex/config.toml` as a model provider and
  makes it the default, keeping the file's comments and profiles. The key is not
  written: Codex reads it from `MUTEGATE_API_KEY`, which `mutegate run -- codex`
  sets.

The file as it was is kept next to it, with `.mutegate-backup` added to its
name, and a file that is not plain JSON is left alone. Tools configured in a
settings window — Cursor, Cline, Continue — get the values to enter and a link
to their guide in the gateway's
[CONNECT.md](https://github.com/danilovid/mutegate/blob/main/docs/CONNECT.md).

## What it talks to

Only the gateway's public API: `/health` and `/v1/models` to check the key, and
whatever the tool itself sends. Any Mutegate gateway works.

## License

Apache 2.0
