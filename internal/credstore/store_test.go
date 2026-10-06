package credstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fileStore forces the fallback backend, so these tests never touch the real
// keyring and stay green on a machine with no Secret Service.
func fileStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state", "session.json")
	t.Setenv(EnvPath, path)
	return &Store{keyringOK: false}, path
}

func session() Session {
	return Session{
		SiteURL: "https://infisical.example.com",
		Email:   "someone@example.com",
		Token:   "header.payload.signature",
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	store, path := fileStore(t)
	want := session()

	if err := store.Save(want); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("session file mode = %#o, want 0600", mode)
	}
	if dir, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	} else if mode := dir.Mode().Perm(); mode != 0o700 {
		t.Fatalf("session dir mode = %#o, want 0700", mode)
	}

	got, err := store.Load(want.SiteURL)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if got.Token != want.Token || got.Email != want.Email {
		t.Fatalf("Load() = %+v, want the saved session", got)
	}
	if got.SavedAt.IsZero() {
		t.Fatal("Save() must stamp SavedAt")
	}
}

func TestLoadIsNotFoundWhenNothingWasSaved(t *testing.T) {
	store, _ := fileStore(t)
	if _, err := store.Load("https://infisical.example.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Load() error = %v, want ErrNotFound", err)
	}
}

func TestLoadRefusesAnAccessibleFile(t *testing.T) {
	store, path := fileStore(t)
	if err := store.Save(session()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := store.Load(session().SiteURL)
	if !errors.Is(err, ErrInsecurePermissions) {
		t.Fatalf("Load() error = %v, want ErrInsecurePermissions", err)
	}
}

func TestLoadRefusesASymlink(t *testing.T) {
	store, path := fileStore(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "elsewhere.json")
	if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}

	_, err := store.Load(session().SiteURL)
	if !errors.Is(err, ErrInsecurePermissions) {
		t.Fatalf("Load() error = %v, want ErrInsecurePermissions", err)
	}
}

func TestCorruptSessionReadsAsAbsentAndNeverEchoes(t *testing.T) {
	store, path := fileStore(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"token":"secret-fragment`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := store.Load(session().SiteURL)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Load() error = %v, want ErrNotFound", err)
	}
	if strings.Contains(err.Error(), "secret-fragment") {
		t.Fatalf("a corrupt payload must never appear in the error: %q", err)
	}
}

func TestSessionFromAnotherInstanceIsRejected(t *testing.T) {
	store, _ := fileStore(t)
	if err := store.Save(session()); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Load("https://someone-else.example.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a token must not be offered to a different instance: %v", err)
	}
}

func TestOldPayloadVersionIsRejected(t *testing.T) {
	store, path := fileStore(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(Session{Version: 0, SiteURL: session().SiteURL, Token: "t", SavedAt: time.Now()})
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Load(session().SiteURL); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Load() error = %v, want ErrNotFound", err)
	}
}

func TestDeleteRemovesTheFile(t *testing.T) {
	store, path := fileStore(t)
	if err := store.Save(session()); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(session().SiteURL); err != nil {
		t.Fatalf("Delete() failed: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("Delete must remove the file, not blank it")
	}
	if err := store.Delete(session().SiteURL); err != nil {
		t.Fatalf("Delete must be idempotent, got %v", err)
	}
}

func TestSessionNeverFormatsItsToken(t *testing.T) {
	s := session()
	for _, rendered := range []string{s.String(), fmt.Sprint(s), fmt.Sprintf("%+v", s)} {
		if strings.Contains(rendered, s.Token) {
			t.Fatalf("formatting a Session leaked the token: %q", rendered)
		}
	}
}
