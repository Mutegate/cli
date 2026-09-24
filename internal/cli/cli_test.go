package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeGateway answers like a gateway that knows one key.
func fakeGateway(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.Write([]byte(`{"status":"ok"}`))
		case "/v1/models":
			if r.Header.Get("Authorization") != "Bearer ap-good" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Write([]byte(`{"object":"list","data":[{"id":"gpt-5"},{"id":"gpt-5-mini"}],
				"unavailable":[{"provider":"anthropic","error":"no key"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

type testEnv struct {
	*Env
	out, err *bytes.Buffer
	vars     map[string]string
}

func newTestEnv(t *testing.T, stdin string) *testEnv {
	t.Helper()
	home := t.TempDir()
	te := &testEnv{out: &bytes.Buffer{}, err: &bytes.Buffer{}, vars: map[string]string{}}
	te.Env = &Env{
		Stdin: strings.NewReader(stdin), Stdout: te.out, Stderr: te.err,
		Getenv:  func(k string) string { return te.vars[k] },
		Environ: func() []string { return []string{"PATH=" + os.Getenv("PATH"), "OPENAI_API_KEY=sk-real-openai"} },
		Home:    home, ConfigDir: filepath.Join(home, ".config", "mutegate"), Version: "test",
	}
	return te
}

func (te *testEnv) run(args ...string) int { return Main(te.Env, args) }

func TestLoginChecksTheKeyAndKeepsItPrivate(t *testing.T) {
	srv := fakeGateway(t)
	te := newTestEnv(t, "ap-good\n")
	if code := te.run("login", srv.URL); code != 0 {
		t.Fatalf("login = %d: %s", code, te.err)
	}
	if !strings.Contains(te.out.String(), "2 models available") {
		t.Errorf("login said: %s", te.out)
	}
	info, err := os.Stat(te.configPath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("config is %v, want 0600: it holds a key", info.Mode().Perm())
	}
	if dir, _ := os.Stat(te.ConfigDir); dir.Mode().Perm() != 0o700 {
		t.Errorf("config dir is %v, want 0700", dir.Mode().Perm())
	}
	c, _ := te.load()
	if c.URL != srv.URL || c.Key != "ap-good" {
		t.Errorf("saved %+v", c)
	}
}

func TestLoginRefusesAKeyTheGatewayDoesNotKnow(t *testing.T) {
	srv := fakeGateway(t)
	te := newTestEnv(t, "ap-wrong\n")
	if code := te.run("login", srv.URL); code != 1 {
		t.Fatalf("login with a wrong key = %d, want 1", code)
	}
	if !strings.Contains(te.err.String(), "does not accept") {
		t.Errorf("stderr: %s", te.err)
	}
	if _, err := os.Stat(te.configPath()); !os.IsNotExist(err) {
		t.Error("a rejected key was saved")
	}
}

func TestStatusSaysWhatAnswers(t *testing.T) {
	srv := fakeGateway(t)
	te := newTestEnv(t, "ap-good\n")
	te.run("login", srv.URL)
	te.out.Reset()
	if code := te.run("status"); code != 0 {
		t.Fatalf("status = %d: %s", code, te.err)
	}
	for _, want := range []string{"Models    2 available", "anthropic is not answering: no key", "ap-good"} {
		if want == "ap-good" {
			if strings.Contains(te.out.String(), want) {
				t.Error("status printed the whole key")
			}
			continue
		}
		if !strings.Contains(te.out.String(), want) {
			t.Errorf("status output lacks %q:\n%s", want, te.out)
		}
	}
}

// CI signs in with the environment, not with login.
func TestTheEnvironmentTakesThePlaceOfLogin(t *testing.T) {
	srv := fakeGateway(t)
	te := newTestEnv(t, "")
	if code := te.run("status"); code != 1 || !strings.Contains(te.err.String(), "not signed in") {
		t.Fatalf("status without a login = %d: %s", code, te.err)
	}
	te.vars["MUTEGATE_URL"] = srv.URL + "/"
	te.vars["MUTEGATE_API_KEY"] = "ap-good"
	te.err.Reset()
	if code := te.run("status"); code != 0 {
		t.Fatalf("status from the environment = %d: %s", code, te.err)
	}
}

func envMap(kvs []string) map[string]string {
	m := map[string]string{}
	for _, kv := range kvs {
		k, v, _ := strings.Cut(kv, "=")
		if _, dup := m[k]; dup {
			m[k] = "DUPLICATE"
			continue
		}
		m[k] = v
	}
	return m
}

func TestRunPointsEveryToolAtTheGateway(t *testing.T) {
	c := Config{URL: "http://gw:8080", Key: "ap-good"}
	base := []string{"PATH=/bin", "OPENAI_API_KEY=sk-real", "ANTHROPIC_API_KEY=sk-ant-real", "ANTHROPIC_AUTH_TOKEN=tok-real"}

	m := envMap(runEnv(base, c, "aider", "aider-1", "aider"))
	for k, want := range map[string]string{
		"PATH":                     "/bin",
		"OPENAI_BASE_URL":          "http://gw:8080/v1",
		"OPENAI_API_BASE":          "http://gw:8080/v1",
		"OPENAI_API_KEY":           "ap-good",
		"ANTHROPIC_BASE_URL":       "http://gw:8080",
		"ANTHROPIC_API_KEY":        "ap-good",
		"MUTEGATE_API_KEY":         "ap-good",
		"MUTEGATE_SESSION":         "aider-1",
		"ANTHROPIC_CUSTOM_HEADERS": "X-Mutegate-Agent: aider\nX-Mutegate-Session: aider-1",
	} {
		if m[k] != want {
			t.Errorf("%s = %q, want %q", k, m[k], want)
		}
	}
	if _, ok := m["ANTHROPIC_AUTH_TOKEN"]; ok {
		t.Error("a real ANTHROPIC_AUTH_TOKEN was passed through")
	}

	// Claude Code: the key as a bearer token, and no second key beside it.
	m = envMap(runEnv(base, c, "claude", "claude-1", "claude"))
	if m["ANTHROPIC_AUTH_TOKEN"] != "ap-good" {
		t.Errorf("claude: ANTHROPIC_AUTH_TOKEN = %q", m["ANTHROPIC_AUTH_TOKEN"])
	}
	if _, ok := m["ANTHROPIC_API_KEY"]; ok {
		t.Error("claude: the real ANTHROPIC_API_KEY was passed through")
	}
}

func TestRunPassesTheToolItsEnvironmentAndExitCode(t *testing.T) {
	srv := fakeGateway(t)
	te := newTestEnv(t, "ap-good\n")
	te.run("login", srv.URL)
	te.out.Reset()
	code := te.run("run", "--", "sh", "-c", `echo "$OPENAI_BASE_URL $OPENAI_API_KEY $MUTEGATE_SESSION"; exit 3`)
	if code != 3 {
		t.Errorf("exit code = %d, want the tool's 3", code)
	}
	got := strings.Fields(te.out.String())
	if len(got) != 3 || got[0] != srv.URL+"/v1" || got[1] != "ap-good" || !strings.HasPrefix(got[2], "sh-") {
		t.Errorf("the tool saw %q", te.out.String())
	}
}

func TestConnectClaudeCodeKeepsTheRestOfTheSettings(t *testing.T) {
	srv := fakeGateway(t)
	te := newTestEnv(t, "ap-good\n")
	te.run("login", srv.URL)

	path := filepath.Join(te.Home, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	old := `{"model":"opus","env":{"FOO":"1","ANTHROPIC_API_KEY":"sk-ant-real"}}`
	os.WriteFile(path, []byte(old), 0o644)

	if code := te.run("connect", "claude-code"); code != 0 {
		t.Fatalf("connect = %d: %s", code, te.err)
	}
	var s struct {
		Model string            `json:"model"`
		Env   map[string]string `json:"env"`
	}
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	if s.Model != "opus" || s.Env["FOO"] != "1" {
		t.Errorf("other settings were lost: %s", b)
	}
	if s.Env["ANTHROPIC_BASE_URL"] != srv.URL || s.Env["ANTHROPIC_AUTH_TOKEN"] != "ap-good" {
		t.Errorf("gateway not set: %s", b)
	}
	if _, ok := s.Env["ANTHROPIC_API_KEY"]; ok {
		t.Error("the provider's own key was left in the settings")
	}
	if backup, _ := os.ReadFile(path + ".mutegate-backup"); string(backup) != old {
		t.Errorf("backup = %q", backup)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o644 {
		t.Errorf("permissions changed to %v", info.Mode().Perm())
	}
}

func TestConnectClaudeCodeLeavesAFileItCannotReadAlone(t *testing.T) {
	srv := fakeGateway(t)
	te := newTestEnv(t, "ap-good\n")
	te.run("login", srv.URL)
	path := filepath.Join(te.Home, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("{ // a comment\n}"), 0o644)
	if code := te.run("connect", "claude-code"); code != 1 {
		t.Fatalf("connect = %d, want 1", code)
	}
	if b, _ := os.ReadFile(path); string(b) != "{ // a comment\n}" {
		t.Error("the file was changed")
	}
}

func TestCodexConfig(t *testing.T) {
	fresh := codexConfig("", "http://gw:8080")
	want := `model_provider = "mutegate"

[model_providers.mutegate]
name = "Mutegate"
base_url = "http://gw:8080/v1"
env_key = "MUTEGATE_API_KEY"
wire_api = "responses"
http_headers = { "X-Mutegate-Agent" = "codex" }
env_http_headers = { "X-Mutegate-Session" = "MUTEGATE_SESSION" }
`
	if fresh != want {
		t.Errorf("fresh config:\n%s", fresh)
	}

	old := `# my settings
model = "gpt-5"
model_provider = "openai"

[model_providers.mutegate]
base_url = "http://old:1/v1"

[profiles.fast]
model_provider = "openai"
model = "gpt-5-mini"
`
	got := codexConfig(old, "http://gw:8080")
	for _, keep := range []string{"# my settings", `model = "gpt-5"`, "[profiles.fast]", `model = "gpt-5-mini"`} {
		if !strings.Contains(got, keep) {
			t.Errorf("lost %q:\n%s", keep, got)
		}
	}
	if strings.Contains(got, "http://old:1") || strings.Count(got, "[model_providers.mutegate]") != 1 {
		t.Errorf("the old provider table was not replaced:\n%s", got)
	}
	if !strings.Contains(got, "model_provider = \"mutegate\"\n\n[model_providers") && !strings.HasPrefix(got, "# my settings\nmodel = \"gpt-5\"\nmodel_provider = \"mutegate\"") {
		t.Errorf("top-level model_provider not set:\n%s", got)
	}
	// A profile's own provider is the user's choice, and stays.
	if !strings.Contains(got, "[profiles.fast]\nmodel_provider = \"openai\"") {
		t.Errorf("a profile's provider was changed:\n%s", got)
	}
	if again := codexConfig(got, "http://gw:8080"); again != got {
		t.Errorf("connecting twice changed the file again:\n%s", again)
	}
}

func TestUsage(t *testing.T) {
	te := newTestEnv(t, "")
	if code := te.run(); code != 0 || !strings.Contains(te.out.String(), "mutegate run -- <command>") {
		t.Errorf("no arguments = %d:\n%s", code, te.out)
	}
	te = newTestEnv(t, "")
	if code := te.run("bogus"); code != 2 || !strings.Contains(te.err.String(), `unknown command "bogus"`) {
		t.Errorf("unknown command = %d: %s", code, te.err)
	}
	te = newTestEnv(t, "")
	if code := te.run("version"); code != 0 || strings.TrimSpace(te.out.String()) != "mutegate test" {
		t.Errorf("version = %d: %q", code, te.out)
	}
}

// A tool set up in a settings window gets the values and the guide. The link
// is the repository's, since the gateway's address is not always where its
// console — and so its /connect page — is served.
func TestConnectAToolWithASettingsWindow(t *testing.T) {
	srv := fakeGateway(t)
	te := newTestEnv(t, "ap-good\n")
	te.run("login", srv.URL)
	te.out.Reset()
	if code := te.run("connect", "cursor"); code != 0 {
		t.Fatalf("connect cursor = %d: %s", code, te.err)
	}
	out := te.out.String()
	for _, want := range []string{srv.URL + "/v1", "docs/CONNECT.md#cursor"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "ap-good") {
		t.Error("the whole key was printed")
	}
}
