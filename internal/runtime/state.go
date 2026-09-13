package runtime

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/apperr"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/config"
)

// logTailBytes bounds how much log text reaches the browser.
const logTailBytes = 64 * 1024

// Status is the local runtime state shown on the overview page.
type Status struct {
	Enabled              bool   `json:"enabled"`
	Running              bool   `json:"running"`
	TokenPresent         bool   `json:"token_present"`
	CoreInstalled        bool   `json:"core_installed"`
	CLIInstalled         bool   `json:"cli_installed"`
	CoreVersion          string `json:"core_version,omitempty"`
	CLIVersion           string `json:"cli_version,omitempty"`
	MachineID            string `json:"machine_id,omitempty"`
	WorkspaceID          string `json:"workspace_id,omitempty"`
	ConfigServer         string `json:"config_server,omitempty"`
	ConsoleURL           string `json:"console_url,omitempty"`
	AllowInsecureConsole bool   `json:"allow_insecure_console"`
	InstallDir           string `json:"install_dir"`
	TUNCapable           bool   `json:"tun_capable"`
}

// Status reports the current runtime state.
func (m *Manager) Status() (Status, *apperr.Error) {
	settings, err := m.store.Settings()
	if err != nil {
		return Status{}, apperr.New(apperr.CodeStateUnavailable)
	}
	coreBinary := m.paths.CoreBinary()
	coreInstalled := isExecutable(coreBinary)
	status := Status{
		Enabled:              settings.Enabled,
		Running:              m.core != nil && m.core.Running(),
		TokenPresent:         m.store.HasBootstrapToken(),
		CoreInstalled:        coreInstalled,
		CLIInstalled:         isExecutable(m.paths.CLIbinary()),
		WorkspaceID:          settings.ActiveWorkspaceID,
		ConfigServer:         settings.ConfigServer,
		ConsoleURL:           settings.ConsoleURL,
		AllowInsecureConsole: settings.AllowInsecureConsole,
		InstallDir:           m.paths.RuntimeDir(),
		TUNCapable:           coreInstalled && tunCapable(coreBinary),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if status.CoreInstalled {
		status.CoreVersion = binaryVersion(ctx, m.paths.CoreBinary())
	}
	if status.CLIInstalled {
		status.CLIVersion = binaryVersion(ctx, m.paths.CLIbinary())
	}
	if machineID, err := m.store.MachineID(); err == nil {
		status.MachineID = machineID
	}
	return status, nil
}

// binaryVersion returns the first line of a runtime binary's version output.
func binaryVersion(ctx context.Context, binary string) string {
	output, err := limitedCommandOutput(ctx, 5*time.Second, downloadMaxVersionBytes, binary, "--version")
	if err != nil && output == "" {
		return ""
	}
	line := output
	if index := strings.IndexByte(line, '\n'); index >= 0 {
		line = line[:index]
	}
	return strings.TrimSpace(truncate(line, 160))
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

// LocalSummary is the management view of the local node.
type LocalSummary struct {
	Node       json.RawMessage `json:"node"`
	Interfaces []string        `json:"interfaces"`
	Peers      json.RawMessage `json:"peers"`
	Stats      json.RawMessage `json:"stats"`
}

// LocalSummary reads the local node, its peers and its statistics through the
// core management RPC.
func (m *Manager) LocalSummary(ctx context.Context) (LocalSummary, *apperr.Error) {
	if !isExecutable(m.paths.CLIbinary()) || m.core == nil || !m.core.Running() {
		return LocalSummary{}, apperr.New(apperr.CodeServiceNotRunning)
	}
	summary := LocalSummary{
		Node:       json.RawMessage("null"),
		Interfaces: []string{},
		Peers:      json.RawMessage("null"),
		Stats:      json.RawMessage("null"),
	}
	node, ok := m.cliCapture(ctx, "node", "info")
	if ok {
		summary.Node = node
		summary.Interfaces = deviceNames(node)
	}
	if peers, ok := m.cliCapture(ctx, "peer"); ok {
		summary.Peers = peers
	}
	if stats, ok := m.cliCapture(ctx, "stats"); ok {
		summary.Stats = stats
	}
	return summary, nil
}

// cliCapture runs one easytier-cli query and returns its JSON output.
func (m *Manager) cliCapture(ctx context.Context, args ...string) (json.RawMessage, bool) {
	query := append([]string{"-p", RPCPortal, "-o", "json"}, args...)
	output, err := commandOutput(ctx, 15*time.Second, m.paths.CLIbinary(), query...)
	if err != nil || strings.TrimSpace(output) == "" {
		return nil, false
	}
	if !json.Valid([]byte(output)) {
		return nil, false
	}
	return json.RawMessage(output), true
}

// deviceNames extracts the TUN interface names from a node info payload. The
// management RPC has changed shape before, so every known layout is accepted
// and every candidate must exist as a network interface.
func deviceNames(node json.RawMessage) []string {
	names := map[string]bool{}
	for _, candidate := range collectStrings(node, "dev_name") {
		if !validDeviceName(candidate) {
			continue
		}
		if _, err := os.Stat("/sys/class/net/" + candidate); err == nil {
			names[candidate] = true
		}
	}
	// Older cores report the tunnel address instead of the device name.
	for _, cidr := range collectStrings(node, "ipv4_addr") {
		for _, name := range interfacesWithAddress(cidr) {
			if validDeviceName(name) {
				names[name] = true
			}
		}
	}
	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

var deviceNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]+$`)

func validDeviceName(name string) bool {
	return name != "" && deviceNamePattern.MatchString(name)
}

// interfacesWithAddress returns the interfaces carrying one address.
func interfacesWithAddress(cidr string) []string {
	target := strings.TrimSpace(cidr)
	if target == "" {
		return nil
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	result := []string{}
	for _, iface := range interfaces {
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			if address.String() == target || strings.TrimSuffix(address.String(), "/32") == target {
				result = append(result, iface.Name)
				break
			}
		}
	}
	return result
}

// collectStrings walks a JSON document and returns every string value stored
// under the given key at any depth or inside arrays.
func collectStrings(document json.RawMessage, key string) []string {
	var value any
	if err := json.Unmarshal(document, &value); err != nil {
		return nil
	}
	result := []string{}
	walkJSON(value, key, &result)
	return result
}

func walkJSON(value any, key string, result *[]string) {
	switch node := value.(type) {
	case map[string]any:
		for name, child := range node {
			if name == key {
				if text, ok := child.(string); ok {
					*result = append(*result, text)
				}
			}
			walkJSON(child, key, result)
		}
	case []any:
		for _, child := range node {
			walkJSON(child, key, result)
		}
	}
}

// LogsResult carries the redacted daemon log.
type LogsResult struct {
	Lines int    `json:"lines"`
	Logs  string `json:"logs"`
}

// Logs returns the tail of the daemon log with secrets removed.
func (m *Manager) Logs(requested int) (LogsResult, *apperr.Error) {
	lines := requested
	if lines < 20 {
		lines = 200
	}
	if lines > 500 {
		lines = 500
	}
	text, err := tailLines(m.paths.DaemonLogFile(), lines, logTailBytes)
	if err != nil && !os.IsNotExist(err) {
		return LogsResult{}, apperr.New(apperr.CodeStateUnavailable)
	}
	return LogsResult{Lines: lines, Logs: Redact(text)}, nil
}

// tailLines reads the last count lines, keeping at most limit bytes.
func tailLines(path string, count int, limit int) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	text := string(data)
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > count {
		lines = lines[len(lines)-count:]
	}
	result := strings.Join(lines, "\n")
	if len(result) > limit {
		result = result[len(result)-limit:]
		if index := strings.IndexByte(result, '\n'); index >= 0 {
			result = result[index+1:]
		}
	}
	if result == "" {
		return "", nil
	}
	return result + "\n", nil
}

var (
	authorizationPattern = regexp.MustCompile(`(?i)(authorization:[[:space:]]*bearer)[[:space:]]+[^[:space:]]+`)
	enrollmentPattern    = regexp.MustCompile(`etk_[A-Za-z0-9._~-]+`)
	jwtPattern           = regexp.MustCompile(`[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{16,}`)
)

// Redact removes enrollment tokens, bearer headers and JWTs from log text.
func Redact(text string) string {
	text = authorizationPattern.ReplaceAllString(text, "$1 [REDACTED]")
	text = enrollmentPattern.ReplaceAllString(text, "[ENROLLMENT TOKEN REDACTED]")
	return jwtPattern.ReplaceAllString(text, "[JWT REDACTED]")
}

// Settings returns the stored configuration for the settings page.
func (m *Manager) Settings() (config.Settings, *apperr.Error) {
	settings, err := m.store.Settings()
	if err != nil {
		return config.Settings{}, apperr.New(apperr.CodeStateUnavailable)
	}
	return settings, nil
}

// ApplySettings validates and stores the user-visible configuration and
// restarts the connection when needed.
func (m *Manager) ApplySettings(ctx context.Context, incoming config.Settings) *apperr.Error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !config.ValidConsoleURL(incoming.ConsoleURL, incoming.AllowInsecureConsole) {
		return apperr.New(apperr.CodeInvalidConsoleURL)
	}
	if incoming.ConfigServer != "" && !config.ValidConfigServer(incoming.ConfigServer) {
		return apperr.New(apperr.CodeInvalidConfigServer)
	}
	stored, err := m.store.Settings()
	if err != nil {
		return apperr.New(apperr.CodeStateUnavailable)
	}
	incoming.ActiveWorkspaceID = stored.ActiveWorkspaceID
	if err := m.store.SaveSettings(incoming); err != nil {
		return apperr.New(apperr.CodeStateUnavailable)
	}
	if !incoming.Enabled {
		m.core.StopAndWait(ctx)
		m.core.ResetBackoff()
		return nil
	}
	if m.store.HasBootstrapToken() && isExecutable(m.paths.CoreBinary()) {
		m.core.StopAndWait(ctx)
		m.core.ResetBackoff()
		m.core.SetDesired(true)
	}
	return nil
}

// ServiceAction starts, stops or restarts the local connection.
func (m *Manager) ServiceAction(ctx context.Context, action string) *apperr.Error {
	m.mu.Lock()
	defer m.mu.Unlock()
	settings, err := m.store.Settings()
	if err != nil {
		return apperr.New(apperr.CodeStateUnavailable)
	}
	switch action {
	case "start":
		if !m.store.HasBootstrapToken() {
			return apperr.New(apperr.CodeNoBootstrapToken)
		}
		if !config.ValidConfigServer(settings.ConfigServer) {
			return apperr.New(apperr.CodeInvalidConfigServer)
		}
		if !isExecutable(m.paths.CoreBinary()) {
			return apperr.New(apperr.CodeCoreNotInstalled)
		}
		settings.Enabled = true
		if err := m.store.SaveSettings(settings); err != nil {
			return apperr.New(apperr.CodeStateUnavailable)
		}
		m.core.ResetBackoff()
		m.core.SetDesired(true)
		return nil
	case "stop":
		settings.Enabled = false
		if err := m.store.SaveSettings(settings); err != nil {
			return apperr.New(apperr.CodeStateUnavailable)
		}
		m.core.StopAndWait(ctx)
		m.core.ResetBackoff()
		return nil
	case "restart":
		if !m.store.HasBootstrapToken() {
			return apperr.New(apperr.CodeNoBootstrapToken)
		}
		if !config.ValidConfigServer(settings.ConfigServer) {
			return apperr.New(apperr.CodeInvalidConfigServer)
		}
		if !isExecutable(m.paths.CoreBinary()) {
			return apperr.New(apperr.CodeCoreNotInstalled)
		}
		settings.Enabled = true
		if err := m.store.SaveSettings(settings); err != nil {
			return apperr.New(apperr.CodeStateUnavailable)
		}
		m.core.StopAndWait(ctx)
		m.core.ResetBackoff()
		m.core.SetDesired(true)
		if !m.core.EnsureHealthy(ctx) {
			return apperr.New(apperr.CodeServiceRestartFailed)
		}
		return nil
	default:
		return apperr.New(apperr.CodeInvalidAction)
	}
}
