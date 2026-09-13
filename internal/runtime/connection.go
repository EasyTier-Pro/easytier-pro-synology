package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/apperr"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/config"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/console"
)

// connectionTransaction records the state that a connection change replaced so
// that an interrupted change can be rolled back.
type connectionTransaction struct {
	PreviousWorkspace    string `json:"previous_workspace"`
	PreviousConfigServer string `json:"previous_config_server"`
	PreviousEnabled      bool   `json:"previous_enabled"`
	HadToken             bool   `json:"had_token"`
	BackupToken          string `json:"backup_token"`
	WasRunning           bool   `json:"was_running"`
}

// Activate enrolls this device through Console and connects the local runtime.
func (m *Manager) Activate(ctx context.Context, workspaceID, mode, keyID string) (Operation, *apperr.Error) {
	if !config.ValidUUID(workspaceID) {
		return Operation{}, apperr.New(apperr.CodeInvalidWorkspace)
	}
	if !console.ValidEnrollmentMode(mode) {
		return Operation{}, apperr.New(apperr.CodeInvalidEnrollmentMode)
	}
	if mode == console.ModeExisting && !config.ValidUUID(keyID) {
		return Operation{}, apperr.New(apperr.CodeEnrollmentChoiceRequired)
	}
	if keyID != "" && !config.ValidUUID(keyID) {
		return Operation{}, apperr.New(apperr.CodeEnrollmentChoiceRequired)
	}
	operation, aerr := m.ops.Begin(KindWorkspace, workspaceID, "")
	if aerr != nil {
		return Operation{}, aerr
	}
	go m.ops.Run(operation, func(ctx context.Context, report func(string)) (bool, *apperr.Error) {
		report("console")
		plan, aerr := m.cli.Activate(ctx, workspaceID, mode, keyID)
		if aerr != nil {
			return false, aerr
		}
		report("service")
		return m.replaceConnection(ctx, plan.BootstrapToken, plan.ConfigServer, plan.WorkspaceID)
	})
	return operation, nil
}

// ConnectToken connects this device with a pasted enrollment token.
func (m *Manager) ConnectToken(ctx context.Context, token, configServer string) (Operation, *apperr.Error) {
	if !config.ValidSecret(token) {
		return Operation{}, apperr.New(apperr.CodeInvalidBootstrapToken)
	}
	if configServer != "" && !config.ValidConfigServer(configServer) {
		return Operation{}, apperr.New(apperr.CodeInvalidConfigServer)
	}
	operation, aerr := m.ops.Begin(KindToken, "", "")
	if aerr != nil {
		return Operation{}, aerr
	}
	go m.ops.Run(operation, func(ctx context.Context, report func(string)) (bool, *apperr.Error) {
		report("release")
		resolved := configServer
		if resolved == "" {
			release, aerr := m.cli.LatestRelease(ctx)
			if aerr != nil {
				return false, aerr
			}
			resolved = release.ConfigServerURL()
		}
		if !config.ValidConfigServer(resolved) {
			return false, apperr.New(apperr.CodeInvalidConfigServer)
		}
		report("service")
		return m.replaceConnection(ctx, token, resolved, "")
	})
	return operation, nil
}

// Disconnect stops the connection and forgets the enrollment token while
// keeping the Console session.
func (m *Manager) Disconnect(ctx context.Context) (Operation, *apperr.Error) {
	operation, aerr := m.ops.Begin(KindDisconnect, "", "")
	if aerr != nil {
		return Operation{}, aerr
	}
	go m.ops.Run(operation, func(ctx context.Context, report func(string)) (bool, *apperr.Error) {
		report("service")
		return false, m.disconnect(ctx)
	})
	return operation, nil
}

// NetworkJoin adds this device to one Console network.
func (m *Manager) NetworkJoin(ctx context.Context, networkID string) (Operation, *apperr.Error) {
	if !config.ValidUUID(networkID) {
		return Operation{}, apperr.New(apperr.CodeInvalidNetwork)
	}
	operation, aerr := m.ops.Begin(KindJoin, "", networkID)
	if aerr != nil {
		return Operation{}, aerr
	}
	go m.ops.Run(operation, func(ctx context.Context, report func(string)) (bool, *apperr.Error) {
		report("network")
		if _, aerr := m.cli.NetworkJoin(ctx, networkID); aerr != nil {
			return false, aerr
		}
		// The Console has just created the node for this device; make sure the
		// runtime configuration it pushes matches what the core can do.
		m.syncRelayModeQuietly(ctx)
		return false, nil
	})
	return operation, nil
}

// NetworkLeave removes this device from one Console network.
func (m *Manager) NetworkLeave(ctx context.Context, networkID string) (Operation, *apperr.Error) {
	if !config.ValidUUID(networkID) {
		return Operation{}, apperr.New(apperr.CodeInvalidNetwork)
	}
	operation, aerr := m.ops.Begin(KindLeave, "", networkID)
	if aerr != nil {
		return Operation{}, aerr
	}
	go m.ops.Run(operation, func(ctx context.Context, report func(string)) (bool, *apperr.Error) {
		report("network")
		if _, aerr := m.cli.NetworkLeave(ctx, networkID); aerr != nil {
			return false, aerr
		}
		return false, nil
	})
	return operation, nil
}

// OperationStatus returns the state of one background operation, or idle when
// no operation was requested at all.
func (m *Manager) OperationStatus(operationID string) (Operation, *apperr.Error) {
	if operationID == "" {
		if current, ok := m.ops.Current(); ok {
			return m.ops.Get(current.ID)
		}
		return Operation{State: "idle"}, nil
	}
	return m.ops.Get(operationID)
}

// disconnect stops the core and clears the enrollment token.
func (m *Manager) disconnect(ctx context.Context) *apperr.Error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.recoverConnectionStateLocked(ctx); err != nil {
		return apperr.New(apperr.CodeStateUnavailable)
	}
	m.core.StopAndWait(ctx)
	m.core.ResetBackoff()
	settings, err := m.store.Settings()
	if err != nil {
		return apperr.New(apperr.CodeStateUnavailable)
	}
	settings.Enabled = false
	settings.ActiveWorkspaceID = ""
	if err := m.store.SaveSettings(settings); err != nil {
		return apperr.New(apperr.CodeStateUnavailable)
	}
	if err := m.store.RemoveBootstrapToken(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return apperr.New(apperr.CodeStateUnavailable)
	}
	return nil
}

// replaceConnection swaps the enrollment token and configuration server with a
// persistent transaction, restarting the core and rolling back on failure.
func (m *Manager) replaceConnection(ctx context.Context, token, configServer, workspaceID string) (bool, *apperr.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.replaceConnectionLocked(ctx, token, configServer, workspaceID)
}

func (m *Manager) replaceConnectionLocked(ctx context.Context, token, configServer, workspaceID string) (bool, *apperr.Error) {
	if !config.ValidSecret(token) {
		return false, apperr.New(apperr.CodeInvalidBootstrapToken)
	}
	if !config.ValidConfigServer(configServer) {
		return false, apperr.New(apperr.CodeInvalidConfigServer)
	}
	if err := m.recoverConnectionStateLocked(ctx); err != nil {
		return false, apperr.New(apperr.CodeStateUnavailable)
	}
	if workspaceID != "" && !config.ValidUUID(workspaceID) {
		return false, apperr.New(apperr.CodeInvalidWorkspace)
	}
	settings, err := m.store.Settings()
	if err != nil {
		return false, apperr.New(apperr.CodeStateUnavailable)
	}

	transaction := connectionTransaction{
		PreviousWorkspace:    settings.ActiveWorkspaceID,
		PreviousConfigServer: settings.ConfigServer,
		PreviousEnabled:      settings.Enabled,
		HadToken:             m.store.HasBootstrapToken(),
		WasRunning:           m.core.Running(),
	}
	if transaction.HadToken {
		backup := filepath.Join(m.paths.SecretsDir(), fmt.Sprintf(".bootstrap-token.previous.%d", os.Getpid()))
		if err := copyFile(m.paths.BootstrapTokenFile(), backup, 0o600); err != nil {
			return false, apperr.New(apperr.CodeStateUnavailable)
		}
		transaction.BackupToken = backup
	}
	if err := m.writeConnectionTransactionLocked(transaction); err != nil {
		os.Remove(transaction.BackupToken)
		return false, apperr.New(apperr.CodeStateUnavailable)
	}

	m.core.StopAndWait(ctx)
	m.core.ResetBackoff()

	next := settings
	next.ActiveWorkspaceID = workspaceID
	next.ConfigServer = config.NormalizeConfigServer(configServer)
	next.Enabled = true
	if err := m.store.SaveSettings(next); err != nil {
		return m.failConnectionChangeLocked(ctx, apperr.New(apperr.CodeStateUnavailable))
	}
	if err := m.store.SaveBootstrapToken(token); err != nil {
		return m.failConnectionChangeLocked(ctx, apperr.New(apperr.CodeStateUnavailable))
	}

	if !isExecutable(m.paths.CoreBinary()) {
		// The connection is stored; the caller offers the runtime download.
		if err := m.commitConnectionTransactionLocked(); err != nil {
			return m.failConnectionChangeLocked(ctx, apperr.New(apperr.CodeStateUnavailable))
		}
		return true, nil
	}
	if !m.core.EnsureHealthy(ctx) {
		return m.failConnectionChangeLocked(ctx, apperr.New(apperr.CodeServiceStartFailed))
	}
	if err := m.commitConnectionTransactionLocked(); err != nil {
		return m.failConnectionChangeLocked(ctx, apperr.New(apperr.CodeStateUnavailable))
	}
	return false, nil
}

// failConnectionChangeLocked rolls the previous connection back and reports
// the original failure, or a storage failure when the rollback is incomplete.
func (m *Manager) failConnectionChangeLocked(ctx context.Context, cause *apperr.Error) (bool, *apperr.Error) {
	if err := m.recoverConnectionStateLocked(ctx); err != nil {
		m.log.Errorf("连接切换失败且回滚未完成: %v", err)
		return false, apperr.New(apperr.CodeStateUnavailable)
	}
	return false, cause
}

func (m *Manager) writeConnectionTransactionLocked(transaction connectionTransaction) error {
	data, err := json.Marshal(transaction)
	if err != nil {
		return err
	}
	return config.AtomicWrite(m.paths.ConnectionTransactionFile(), data, 0o600)
}

func (m *Manager) commitConnectionTransactionLocked() error {
	path := m.paths.ConnectionTransactionFile()
	backup := ""
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		var transaction connectionTransaction
		if json.Unmarshal(data, &transaction) == nil {
			backup = transaction.BackupToken
		}
	case errors.Is(err, os.ErrNotExist):
	default:
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// The removal must be durable before the only token backup goes away,
	// otherwise a power loss could resurrect the transaction without it.
	config.SyncDir(m.paths.StateDir())
	if backup != "" {
		os.Remove(backup)
	}
	return nil
}

// recoverConnectionState restores the state recorded by an interrupted
// connection change. It is safe to call when no transaction exists.
func (m *Manager) recoverConnectionState(ctx context.Context, force bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.recoverConnectionStateLocked(ctx)
}

func (m *Manager) recoverConnectionStateLocked(ctx context.Context) error {
	path := m.paths.ConnectionTransactionFile()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var transaction connectionTransaction
	if err := json.Unmarshal(data, &transaction); err != nil {
		return err
	}
	if !validBackupTokenPath(m.paths, transaction.BackupToken) {
		return errors.New("connection transaction has an unsafe backup path")
	}
	if transaction.PreviousWorkspace != "" && !config.ValidUUID(transaction.PreviousWorkspace) {
		return errors.New("connection transaction has an invalid workspace")
	}
	if transaction.PreviousConfigServer != "" && !config.ValidConfigServer(transaction.PreviousConfigServer) {
		return errors.New("connection transaction has an invalid configuration server")
	}
	m.log.Printf("回滚未完成的连接切换")
	m.core.StopAndWait(ctx)
	m.core.ResetBackoff()

	if transaction.HadToken {
		if _, err := os.Stat(transaction.BackupToken); err == nil {
			if err := m.store.RestoreBootstrapToken(transaction.BackupToken); err != nil {
				return err
			}
		} else if !m.store.HasBootstrapToken() {
			return errors.New("the previous enrollment token is missing")
		}
	} else {
		if err := m.store.RemoveBootstrapToken(); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	settings, err := m.store.Settings()
	if err != nil {
		return err
	}
	settings.ActiveWorkspaceID = transaction.PreviousWorkspace
	settings.ConfigServer = transaction.PreviousConfigServer
	settings.Enabled = transaction.PreviousEnabled
	if err := m.store.SaveSettings(settings); err != nil {
		return err
	}
	os.Remove(transaction.BackupToken)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	config.SyncDir(m.paths.StateDir())
	// Restore the previous connection whenever it was enabled and complete,
	// even if the core happened to be between restarts when the change began.
	if settings.Enabled && m.store.HasBootstrapToken() &&
		config.ValidConfigServer(settings.ConfigServer) && isExecutable(m.paths.CoreBinary()) {
		m.core.EnsureHealthy(ctx)
	}
	return nil
}

// validBackupTokenPath confines transaction backup paths to our secrets dir.
func validBackupTokenPath(paths config.Paths, backup string) bool {
	if backup == "" {
		return true
	}
	prefix := filepath.Join(paths.SecretsDir(), ".bootstrap-token.previous.")
	if !strings.HasPrefix(backup, prefix) {
		return false
	}
	suffix := strings.TrimPrefix(backup, prefix)
	if suffix == "" {
		return false
	}
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func copyFile(source, target string, perm os.FileMode) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return config.AtomicWrite(target, data, perm)
}
