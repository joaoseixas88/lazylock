// Package config holds the little that lazylock needs to know before it can
// talk to anything: which Infisical instance, and who was logged in last.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/joaoseixas88/lazylock/internal/atomicfile"
)

// Env vars override the file and are never written back to it.
const (
	EnvSiteURL = "LAZYLOCK_SITE_URL"
	EnvToken   = "LAZYLOCK_TOKEN"
	EnvPath    = "LAZYLOCK_CONFIG"
)

type Config struct {
	SiteURL string `json:"siteURL"` // https://infisical.example.com, never with /api
	Account string `json:"account"` // last logged-in email; a hint, not a credential
}

// Path reports where the config file lives. os.UserConfigDir already honours
// XDG_CONFIG_HOME, so there is no XDG handling to write here.
func Path() (string, error) {
	if override := os.Getenv(EnvPath); override != "" {
		return override, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate config dir: %w", err)
	}
	return filepath.Join(dir, "lazylock", "config.json"), nil
}

// Load reads the config. A missing file is not an error: it is a first run.
func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}

// Save writes the config atomically, so a crash mid-write cannot leave a
// truncated file behind.
func Save(cfg Config) error {
	path, err := Path()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(path, append(data, '\n'), 0o600)
}

// WithEnvOverrides applies the environment on top of the file.
func (c Config) WithEnvOverrides() Config {
	if site := os.Getenv(EnvSiteURL); site != "" {
		if normalized, err := NormalizeSiteURL(site); err == nil {
			c.SiteURL = normalized
		} else {
			c.SiteURL = site // let Validate produce the complaint
		}
	}
	return c
}

// Token returns the token supplied by the environment, if any. It exists so the
// HTTP layer can be exercised against a real instance before login is written.
func Token() string { return os.Getenv(EnvToken) }

func (c Config) Validate() error {
	if c.SiteURL == "" {
		return errors.New("no Infisical site URL configured")
	}
	_, err := NormalizeSiteURL(c.SiteURL)
	return err
}

// APIBase is the prefix every endpoint hangs off.
func (c Config) APIBase() string { return c.SiteURL + "/api" }

// Origin is what the browser will send in the Origin header of the login
// callback, and therefore the only value that handler accepts.
func (c Config) Origin() string {
	u, err := url.Parse(c.SiteURL)
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// LoginURL is the page that hands the token back to the port we are listening
// on.
func (c Config) LoginURL(callbackPort int) string {
	return fmt.Sprintf("%s/login?callback_port=%d", c.SiteURL, callbackPort)
}

// NormalizeSiteURL turns what a person would paste into the base URL the rest
// of the package assumes. It notably strips a trailing /api, because that is
// how the Infisical CLI stores the domain and therefore what gets copied.
func NormalizeSiteURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("site URL is empty")
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("parse site URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("site URL must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return "", errors.New("site URL has no host")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("site URL must not carry a query or fragment")
	}
	path := strings.TrimSuffix(u.Path, "/")
	path = strings.TrimSuffix(path, "/api")
	path = strings.TrimSuffix(path, "/")
	return u.Scheme + "://" + u.Host + path, nil
}
