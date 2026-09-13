package config

import (
	"os"
	"path/filepath"
	"testing"
)

func testPaths(t *testing.T) Paths {
	t.Helper()
	root := t.TempDir()
	return Paths{PkgDest: filepath.Join(root, "target"), PkgVar: filepath.Join(root, "var")}
}

func TestAtomicWriteUsesOwnerOnlyMode(t *testing.T) {
	paths := testPaths(t)
	if err := paths.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	target := paths.SettingsFile()
	if err := AtomicWrite(target, []byte("{\"enabled\":true}\n"), 0o600); err != nil {
		t.Fatalf("AtomicWrite: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("permissions = %o, want 600", perm)
	}
	entries, err := os.ReadDir(filepath.Dir(target))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, entry := range entries {
		if entry.Name()[0] == '.' {
			t.Fatalf("temporary file left behind: %s", entry.Name())
		}
	}
}

func TestAtomicWriteReplacesContent(t *testing.T) {
	paths := testPaths(t)
	target := paths.SettingsFile()
	for _, value := range []string{"first\n", "second\n"} {
		if err := AtomicWrite(target, []byte(value), 0o600); err != nil {
			t.Fatalf("AtomicWrite(%q): %v", value, err)
		}
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "second\n" {
		t.Fatalf("content = %q, want %q", data, "second\n")
	}
}

func TestMachineIDIsStable(t *testing.T) {
	paths := testPaths(t)
	if err := paths.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	store := NewStore(paths)
	first, err := store.MachineID()
	if err != nil {
		t.Fatalf("MachineID: %v", err)
	}
	if !ValidUUID(first) {
		t.Fatalf("machine id %q is not a UUID", first)
	}
	second, err := store.MachineID()
	if err != nil {
		t.Fatalf("MachineID: %v", err)
	}
	if first != second {
		t.Fatalf("machine id changed between calls: %q then %q", first, second)
	}

	reopened := NewStore(paths)
	third, err := reopened.MachineID()
	if err != nil {
		t.Fatalf("MachineID after reopen: %v", err)
	}
	if third != first {
		t.Fatalf("machine id changed after reopen: %q then %q", first, third)
	}
}

func TestSettingsDefaultsAndRoundTrip(t *testing.T) {
	paths := testPaths(t)
	store := NewStore(paths)
	settings, err := store.Settings()
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	if !settings.Enabled || settings.ConsoleURL != DefaultConsoleURL {
		t.Fatalf("unexpected defaults: %+v", settings)
	}

	settings.Enabled = false
	settings.ActiveWorkspaceID = "6d3a0d2b-2a2f-4a44-9c47-1c1e13a8a2a1"
	settings.ConfigServer = "tcp://config.example.com:22020/"
	if err := store.SaveSettings(settings); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}

	reopened := NewStore(paths)
	stored, err := reopened.Settings()
	if err != nil {
		t.Fatalf("Settings after reopen: %v", err)
	}
	if stored.Enabled {
		t.Fatalf("enabled flag was lost: %+v", stored)
	}
	if stored.ConfigServer != "tcp://config.example.com:22020" {
		t.Fatalf("config server = %q, want trailing slash trimmed", stored.ConfigServer)
	}
	if stored.ActiveWorkspaceID != settings.ActiveWorkspaceID {
		t.Fatalf("workspace = %q, want %q", stored.ActiveWorkspaceID, settings.ActiveWorkspaceID)
	}
}

func TestBootstrapTokenRoundTrip(t *testing.T) {
	paths := testPaths(t)
	if err := paths.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	store := NewStore(paths)
	if store.HasBootstrapToken() {
		t.Fatal("unexpected token before saving")
	}
	if _, err := store.ReadBootstrapToken(); err != ErrNoBootstrapToken {
		t.Fatalf("ReadBootstrapToken error = %v, want ErrNoBootstrapToken", err)
	}
	if err := store.SaveBootstrapToken("etk_abc123"); err != nil {
		t.Fatalf("SaveBootstrapToken: %v", err)
	}
	token, err := store.ReadBootstrapToken()
	if err != nil {
		t.Fatalf("ReadBootstrapToken: %v", err)
	}
	if token != "etk_abc123" {
		t.Fatalf("token = %q", token)
	}
	if err := store.SaveBootstrapToken("bad\ntoken"); err == nil {
		t.Fatal("expected control characters to be rejected")
	}
	if err := store.RemoveBootstrapToken(); err != nil {
		t.Fatalf("RemoveBootstrapToken: %v", err)
	}
	if store.HasBootstrapToken() {
		t.Fatal("token still present after removal")
	}
}

func TestValidConfigServer(t *testing.T) {
	valid := []string{
		"tcp://host:22020",
		"udp://host:22020",
		"quic://host:22020",
		"wss://host/path",
		"https://host/",
	}
	for _, value := range valid {
		if !ValidConfigServer(value) {
			t.Errorf("ValidConfigServer(%q) = false, want true", value)
		}
	}
	invalid := []string{
		"",
		"tcp://",
		"ftp://x",
		"host:22020",
		"tcp://host:22 020",
		"tcp://host:22020\n",
		"tcp://host\t:22020",
	}
	for _, value := range invalid {
		if ValidConfigServer(value) {
			t.Errorf("ValidConfigServer(%q) = true, want false", value)
		}
	}
}

func TestValidConsoleURL(t *testing.T) {
	if !ValidConsoleURL("https://api.console.easytier.net", false) {
		t.Error("https URL rejected")
	}
	if !ValidConsoleURL("https://api.console.easytier.net/", false) {
		t.Error("https URL with trailing slash rejected")
	}
	if ValidConsoleURL("http://api.console.easytier.net", false) {
		t.Error("http URL accepted without allow_insecure_console")
	}
	if !ValidConsoleURL("http://10.0.0.5:8080", true) {
		t.Error("http URL rejected with allow_insecure_console")
	}
	for _, value := range []string{"", "ftp://host", "https://", "https://host x", "https://host\n"} {
		if ValidConsoleURL(value, true) {
			t.Errorf("ValidConsoleURL(%q) = true, want false", value)
		}
	}
}

func TestNormalizeConsoleURL(t *testing.T) {
	if got := NormalizeConsoleURL("https://api.console.easytier.net/"); got != "https://api.console.easytier.net" {
		t.Fatalf("got %q", got)
	}
}
