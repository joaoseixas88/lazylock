// Package credstore persists lazylock's own credential: the Infisical session
// token. It prefers the OS keyring and falls back to a 0600 file only when no
// Secret Service answers, which is what makes the tool usable on a headless
// box without silently putting a token on disk everywhere else.
package credstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/zalando/go-keyring"
)

const (
	service      = "lazylock"
	payloadVer   = 1
	probeAccount = "__probe__"
)

var (
	ErrNotFound            = errors.New("credstore: no stored session")
	ErrInsecurePermissions = errors.New("credstore: session file is group or world accessible")
)

// Session is what a successful login produces. There is deliberately no refresh
// token: the Infisical browser callback never hands one over, and the renew
// endpoint reads it from a browser cookie. A dead token means logging in again.
type Session struct {
	Version int       `json:"v"`
	SiteURL string    `json:"siteURL"`
	Email   string    `json:"email"`
	Token   string    `json:"token"`
	SavedAt time.Time `json:"savedAt"`
}

// String keeps the token out of anything that formats the struct.
func (s Session) String() string { return "credstore.Session{redacted}" }

type Store struct {
	keyringOK bool
}

// New probes the keyring once and settles on a backend.
func New() *Store {
	_, err := keyring.Get(service, probeAccount)
	// ErrNotFound means the Secret Service answered and simply has no such
	// entry, which is exactly the healthy case. Anything else means no keyring.
	return &Store{keyringOK: errors.Is(err, keyring.ErrNotFound) || err == nil}
}

// Backend reports where sessions are being kept, so the UI can tell the user.
func (s *Store) Backend() string {
	if s.keyringOK {
		return "keyring"
	}
	return "file"
}

func (s *Store) Load(siteURL string) (Session, error) {
	var raw []byte
	if s.keyringOK {
		value, err := keyring.Get(service, siteURL)
		if errors.Is(err, keyring.ErrNotFound) {
			return Session{}, ErrNotFound
		}
		if err != nil {
			return Session{}, fmt.Errorf("read keyring: %w", err)
		}
		raw = []byte(value)
	} else {
		path, err := sessionPath()
		if err != nil {
			return Session{}, err
		}
		if err := checkFile(path); err != nil {
			return Session{}, err
		}
		raw, err = os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			return Session{}, ErrNotFound
		}
		if err != nil {
			return Session{}, fmt.Errorf("read session file: %w", err)
		}
	}

	var session Session
	// A corrupt payload is reported as absent, never echoed: it may still hold
	// most of a token.
	if err := json.Unmarshal(raw, &session); err != nil {
		return Session{}, ErrNotFound
	}
	if session.Version != payloadVer || session.Token == "" {
		return Session{}, ErrNotFound
	}
	// Pin the token to the instance it came from, so editing the config cannot
	// send one instance's token to another.
	if session.SiteURL != siteURL {
		return Session{}, ErrNotFound
	}
	return session, nil
}

func (s *Store) Save(session Session) error {
	session.Version = payloadVer
	if session.SavedAt.IsZero() {
		session.SavedAt = time.Now()
	}
	raw, err := json.Marshal(session)
	if err != nil {
		return err
	}
	if s.keyringOK {
		if err := keyring.Set(service, session.SiteURL, string(raw)); err != nil {
			return fmt.Errorf("write keyring: %w", err)
		}
		return nil
	}
	return writeFile(raw)
}

func (s *Store) Delete(siteURL string) error {
	if s.keyringOK {
		err := keyring.Delete(service, siteURL)
		if err != nil && !errors.Is(err, keyring.ErrNotFound) {
			return fmt.Errorf("delete from keyring: %w", err)
		}
		return nil
	}
	path, err := sessionPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove session file: %w", err)
	}
	return nil
}
