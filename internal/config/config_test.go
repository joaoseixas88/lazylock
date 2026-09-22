package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeSiteURL(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		wantErr  bool
	}{
		{in: "https://infisical.example.com", want: "https://infisical.example.com"},
		{in: "  https://infisical.example.com/  ", want: "https://infisical.example.com"},
		{in: "infisical.example.com", want: "https://infisical.example.com"},
		{in: "http://localhost:8080", want: "http://localhost:8080"},
		// What the Infisical CLI stores, and therefore what gets pasted.
		{in: "https://infisical.example.com/api", want: "https://infisical.example.com"},
		{in: "https://infisical.example.com/api/", want: "https://infisical.example.com"},
		{in: "https://example.com/infisical/api", want: "https://example.com/infisical"},
		{in: "", wantErr: true},
		{in: "   ", wantErr: true},
		{in: "ftp://example.com", wantErr: true},
		{in: "https://example.com?x=1", wantErr: true},
		{in: "https://example.com#frag", wantErr: true},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got, err := NormalizeSiteURL(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("NormalizeSiteURL(%q) = %q, want an error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeSiteURL(%q) failed: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("NormalizeSiteURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestDerivedURLs(t *testing.T) {
	cfg := Config{SiteURL: "https://infisical.example.com"}
	if got := cfg.APIBase(); got != "https://infisical.example.com/api" {
		t.Fatalf("APIBase() = %q", got)
	}
	if got := cfg.Origin(); got != "https://infisical.example.com" {
		t.Fatalf("Origin() = %q", got)
	}
	if got := cfg.LoginURL(51234); got != "https://infisical.example.com/login?callback_port=51234" {
		t.Fatalf("LoginURL() = %q", got)
	}
}

func TestLoadIsEmptyOnFirstRun(t *testing.T) {
	t.Setenv(EnvPath, filepath.Join(t.TempDir(), "config.json"))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("a missing config must not be an error: %v", err)
	}
	if cfg != (Config{}) {
		t.Fatalf("Load() = %+v, want the zero config", cfg)
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("the zero config must not validate")
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	t.Setenv(EnvPath, path)

	want := Config{SiteURL: "https://infisical.example.com", Account: "someone@example.com"}
	if err := Save(want); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("config mode = %o, want 600", mode)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if got != want {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
}

func TestEnvOverridesTheFileAndNormalizes(t *testing.T) {
	t.Setenv(EnvPath, filepath.Join(t.TempDir(), "config.json"))
	t.Setenv(EnvSiteURL, "https://other.example.com/api")

	cfg := Config{SiteURL: "https://infisical.example.com"}.WithEnvOverrides()
	if cfg.SiteURL != "https://other.example.com" {
		t.Fatalf("SiteURL = %q, want the normalized override", cfg.SiteURL)
	}
}

func TestCorruptConfigNamesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(EnvPath, path)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Load()
	if err == nil {
		t.Fatal("a corrupt config must be reported")
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("error should name the offending file, got %q", err)
	}
}
