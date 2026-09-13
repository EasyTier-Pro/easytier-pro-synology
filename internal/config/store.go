package config

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
)

// DefaultConsoleURL is the production EasyTier Console API.
const DefaultConsoleURL = "https://api.console.easytier.net"

// ErrNoBootstrapToken is returned when no enrollment token is configured yet.
var ErrNoBootstrapToken = errors.New("no bootstrap token is configured")

// Settings is the full non-secret configuration of this client. It is stored
// as state/settings.json with mode 0600 and survives package upgrades.
type Settings struct {
	Enabled              bool   `json:"enabled"`
	ConsoleURL           string `json:"console_url"`
	AllowInsecureConsole bool   `json:"allow_insecure_console"`
	ConfigServer         string `json:"config_server"`
	ActiveWorkspaceID    string `json:"active_workspace_id"`
}

// DefaultSettings returns the configuration used until the user changes it.
func DefaultSettings() Settings {
	return Settings{
		Enabled:    true,
		ConsoleURL: DefaultConsoleURL,
	}
}

// Store reads and writes the mutable state of this client. It serializes
// access to the settings cache and to the secret files.
type Store struct {
	paths Paths

	mu       sync.Mutex
	settings *Settings
}

// NewStore returns a store rooted at paths.
func NewStore(paths Paths) *Store {
	return &Store{paths: paths}
}

// Paths exposes the resolved package roots.
func (s *Store) Paths() Paths { return s.paths }

// Settings returns the current configuration, loading it from disk on first
// use. Missing files yield the defaults.
func (s *Store) Settings() (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settingsLocked()
}

func (s *Store) settingsLocked() (Settings, error) {
	if s.settings != nil {
		return *s.settings, nil
	}
	settings := DefaultSettings()
	data, err := os.ReadFile(s.paths.SettingsFile())
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &settings); err != nil {
			return Settings{}, fmt.Errorf("parse %s: %w", s.paths.SettingsFile(), err)
		}
	case errors.Is(err, os.ErrNotExist):
	default:
		return Settings{}, err
	}
	s.settings = &settings
	return settings, nil
}

// SaveSettings persists settings atomically and refreshes the cache.
func (s *Store) SaveSettings(settings Settings) error {
	settings.ConsoleURL = NormalizeConsoleURL(settings.ConsoleURL)
	if settings.ConsoleURL == "" {
		settings.ConsoleURL = DefaultConsoleURL
	}
	if settings.ConfigServer != "" {
		settings.ConfigServer = NormalizeConfigServer(settings.ConfigServer)
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := AtomicWrite(s.paths.SettingsFile(), data, 0o600); err != nil {
		return err
	}
	s.mu.Lock()
	s.settings = &settings
	s.mu.Unlock()
	return nil
}

// SaveBootstrapToken stores the enrollment token with mode 0600.
func (s *Store) SaveBootstrapToken(token string) error {
	if !ValidSecret(token) {
		return errors.New("invalid bootstrap token")
	}
	return AtomicWrite(s.paths.BootstrapTokenFile(), []byte(token+"\n"), 0o600)
}

// ReadBootstrapToken returns the enrollment token.
func (s *Store) ReadBootstrapToken() (string, error) {
	token, err := ReadSecret(s.paths.BootstrapTokenFile())
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrNoBootstrapToken
	}
	return token, err
}

// RestoreBootstrapToken replaces the enrollment token with the contents of a
// backup file written by a connection transaction.
func (s *Store) RestoreBootstrapToken(source string) error {
	token, err := ReadSecret(source)
	if err != nil {
		return err
	}
	return s.SaveBootstrapToken(token)
}

// HasBootstrapToken reports whether a usable enrollment token exists.
func (s *Store) HasBootstrapToken() bool {
	_, err := s.ReadBootstrapToken()
	return err == nil
}

// RemoveBootstrapToken deletes the enrollment token.
func (s *Store) RemoveBootstrapToken() error {
	return os.Remove(s.paths.BootstrapTokenFile())
}

// MachineID returns the persistent device identity, creating it on first use.
func (s *Store) MachineID() (string, error) {
	data, err := os.ReadFile(s.paths.MachineIDFile())
	if err == nil {
		value := trimNewline(string(data))
		if ValidUUID(value) {
			return value, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	value, err := RandomUUID()
	if err != nil {
		return "", err
	}
	if err := AtomicWrite(s.paths.MachineIDFile(), []byte(value+"\n"), 0o600); err != nil {
		return "", err
	}
	return value, nil
}

func trimNewline(value string) string {
	for len(value) > 0 && (value[len(value)-1] == '\n' || value[len(value)-1] == '\r') {
		value = value[:len(value)-1]
	}
	return value
}

// RandomUUID returns a random version 4 UUID.
func RandomUUID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16]), nil
}
