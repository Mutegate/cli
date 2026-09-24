package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

// probe is what the gateway says about itself and about a key.
type probe struct {
	Latency     time.Duration
	Models      []string
	Unavailable []struct {
		Provider string `json:"provider"`
		Error    string `json:"error"`
	}
}

var errKeyRejected = errors.New("the gateway does not accept this key")

// check asks the gateway whether it is up and whether it takes the key: the
// model list is the cheapest request a key can make, and it says which
// providers answer.
func check(c Config) (probe, error) {
	var p probe
	client := &http.Client{Timeout: 15 * time.Second}
	start := time.Now()
	resp, err := client.Get(c.URL + "/health")
	if err != nil {
		return p, fmt.Errorf("cannot reach %s: %w", c.URL, err)
	}
	resp.Body.Close()
	p.Latency = time.Since(start)
	if resp.StatusCode != http.StatusOK {
		return p, fmt.Errorf("%s answered %s on /health — is it a Mutegate gateway?", c.URL, resp.Status)
	}

	req, _ := http.NewRequest(http.MethodGet, c.URL+"/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+c.Key)
	resp, err = client.Do(req)
	if err != nil {
		return p, fmt.Errorf("listing models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return p, errKeyRejected
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return p, fmt.Errorf("listing models: %s %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Unavailable []struct {
			Provider string `json:"provider"`
			Error    string `json:"error"`
		} `json:"unavailable"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return p, fmt.Errorf("listing models: %w", err)
	}
	for _, m := range list.Data {
		p.Models = append(p.Models, m.ID)
	}
	p.Unavailable = list.Unavailable
	return p, nil
}

// normalizeURL accepts an address as people type it: without a scheme,
// https — except on this machine, where it is almost always plain http.
func normalizeURL(s string) string {
	s = strings.TrimRight(strings.TrimSpace(s), "/")
	if s == "" || strings.Contains(s, "://") {
		return s
	}
	host := s
	if i := strings.IndexAny(host, ":/"); i >= 0 {
		host = host[:i]
	}
	if host == "localhost" || host == "127.0.0.1" || host == "[::1]" {
		return "http://" + s
	}
	return "https://" + s
}

func login(env *Env, args []string) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(env.Stderr, "usage: mutegate login [address]   (the key is read from the terminal, or from stdin)")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	saved, err := env.load()
	if err != nil {
		return err
	}
	url := normalizeURL(fs.Arg(0))
	if url == "" {
		url = saved.URL
	}
	if url == "" {
		url = "http://localhost:8080"
	}

	key, err := readKey(env, fmt.Sprintf("API key for %s (from its console, Settings → API keys): ", url))
	if err != nil {
		return err
	}
	if key == "" {
		return errors.New("no key given")
	}
	c := Config{URL: url, Key: key}
	p, err := check(c)
	if err != nil {
		return err
	}
	if err := env.save(c); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "Signed in to %s with %s. %d models available.\n", url, maskKey(key), len(p.Models))
	fmt.Fprintf(env.Stdout, "Next: mutegate run -- claude   (or any tool; see mutegate help)\n")
	return nil
}

// readKey reads a key without echoing it when there is a terminal to type
// in, and a line from stdin when it is piped: `pass show x | mutegate login`.
func readKey(env *Env, prompt string) (string, error) {
	if f, ok := env.Stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprint(env.Stderr, prompt)
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(env.Stderr)
		return strings.TrimSpace(string(b)), err
	}
	line, err := bufio.NewReader(env.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func logout(env *Env, args []string) error {
	if len(args) > 0 {
		return errors.New("usage: mutegate logout")
	}
	saved, err := env.load()
	if err != nil {
		return err
	}
	// The address stays, so the next login only asks for a key.
	if err := env.save(Config{URL: saved.URL}); err != nil {
		return err
	}
	fmt.Fprintln(env.Stdout, "Signed out; the key is forgotten.")
	return nil
}

func status(env *Env, args []string) error {
	if len(args) > 0 {
		return errors.New("usage: mutegate status")
	}
	c, err := env.loadSignedIn()
	if err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "Gateway   %s\n", c.URL)
	fmt.Fprintf(env.Stdout, "Key       %s\n", maskKey(c.Key))
	p, err := check(c)
	if err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "Status    ok, %d ms\n", p.Latency.Milliseconds())
	fmt.Fprintf(env.Stdout, "Models    %d available\n", len(p.Models))
	for _, u := range p.Unavailable {
		fmt.Fprintf(env.Stdout, "          %s is not answering: %s\n", u.Provider, u.Error)
	}
	return nil
}
