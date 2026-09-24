package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config is the gateway this command line talks to, and the key it uses.
type Config struct {
	URL string `json:"url"`
	Key string `json:"key"`
}

func (e *Env) configPath() string { return filepath.Join(e.ConfigDir, "cli.json") }

// load reads what login saved, with MUTEGATE_URL and MUTEGATE_API_KEY taking
// its place when set — which is how CI uses it, without a login.
func (e *Env) load() (Config, error) {
	var c Config
	b, err := os.ReadFile(e.configPath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return c, err
	}
	if err == nil {
		if err := json.Unmarshal(b, &c); err != nil {
			return c, fmt.Errorf("%s: %w", e.configPath(), err)
		}
	}
	if v := e.Getenv("MUTEGATE_URL"); v != "" {
		c.URL = v
	}
	if v := e.Getenv("MUTEGATE_API_KEY"); v != "" {
		c.Key = v
	}
	c.URL = strings.TrimRight(c.URL, "/")
	return c, nil
}

// loadSignedIn is load, insisting there is somewhere to go and a key to go
// there with.
func (e *Env) loadSignedIn() (Config, error) {
	c, err := e.load()
	if err != nil {
		return c, err
	}
	if c.URL == "" || c.Key == "" {
		return c, errors.New("not signed in: run `mutegate login <address>`, or set MUTEGATE_URL and MUTEGATE_API_KEY")
	}
	return c, nil
}

// save writes the settings readable by this user only: they hold a key.
func (e *Env) save(c Config) error {
	if err := os.MkdirAll(e.ConfigDir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(e.configPath(), append(b, '\n'), 0o600)
}

// writeFileAtomic replaces path in one step, so a crash never leaves half a
// configuration file behind — ours or a tool's.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// maskKey shows enough of a key to recognize it.
func maskKey(k string) string {
	if len(k) <= 12 {
		return strings.Repeat("•", len(k))
	}
	return k[:7] + "…" + k[len(k)-4:]
}
