package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/apperr"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/config"
	"sync"
)

// Operation kinds. The first two mirror the OpenWrt client; the others are the
// local actions this client performs in the background.
const (
	KindWorkspace  = "workspace"
	KindToken      = "token"
	KindDisconnect = "disconnect"
	KindJoin       = "join"
	KindLeave      = "leave"
)

// Operation states.
const (
	StateQueued    = "queued"
	StateRunning   = "running"
	StateCompleted = "completed"
	StateFailed    = "failed"
)

// operationTTL is how long a finished operation stays readable.
const operationTTL = 5 * time.Minute

// Operation is one background change with its progress, persisted so the UI
// can follow it across page reloads.
type Operation struct {
	ID            string `json:"operation_id"`
	State         string `json:"state"`
	Phase         string `json:"phase"`
	Kind          string `json:"kind"`
	WorkspaceID   string `json:"workspace_id,omitempty"`
	NetworkID     string `json:"network_id,omitempty"`
	NeedsDownload bool   `json:"needs_download"`
	Error         string `json:"error,omitempty"`
	Message       string `json:"message,omitempty"`
	UpdatedAt     int64  `json:"updated_at"`
}

var errInvalidOperation = errors.New("invalid operation record")

// operationBody performs the work of one operation and reports whether the
// local runtime still needs to be installed afterwards.
type operationBody func(ctx context.Context, report func(phase string)) (bool, *apperr.Error)

// validKind reports whether the operation kind is one this client produces.
func validKind(kind string) bool {
	switch kind {
	case KindWorkspace, KindToken, KindDisconnect, KindJoin, KindLeave:
		return true
	}
	return false
}

// operationSet owns the single in-flight operation and its persisted records.
type operationSet struct {
	dir string

	mu     sync.Mutex
	active *Operation
}

func newOperationSet(m *Manager) *operationSet {
	return &operationSet{dir: m.paths.OperationsDir()}
}

// Start marks operations abandoned by a previous daemon run as interrupted and
// launches the janitor that expires finished records.
func (s *operationSet) Start(ctx context.Context) {
	entries, err := os.ReadDir(s.dir)
	if err == nil {
		for _, entry := range entries {
			operation, err := s.load(strings.TrimSuffix(entry.Name(), ".json"))
			if err != nil {
				os.Remove(filepath.Join(s.dir, entry.Name()))
				continue
			}
			if operation.State == StateRunning || operation.State == StateQueued {
				message := apperr.New(apperr.CodeConnectionChangeInterrupted).Message
				s.write(operation.ID, StateFailed, StateFailed, operation.Kind, operation.WorkspaceID,
					operation.NetworkID, false, apperr.CodeConnectionChangeInterrupted, message)
			}
		}
	}
	go s.janitor(ctx)
}

func (s *operationSet) janitor(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.expire()
		}
	}
}

func (s *operationSet) expire() {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-operationTTL).Unix()
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		operation, err := s.load(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			os.Remove(filepath.Join(s.dir, entry.Name()))
			continue
		}
		if operation.State != StateCompleted && operation.State != StateFailed {
			continue
		}
		if operation.UpdatedAt <= cutoff {
			os.Remove(filepath.Join(s.dir, entry.Name()))
		}
	}
}

func (s *operationSet) file(id string) string {
	return filepath.Join(s.dir, id+".json")
}

func (s *operationSet) load(id string) (Operation, error) {
	data, err := os.ReadFile(s.file(id))
	if err != nil {
		return Operation{}, err
	}
	var operation Operation
	if err := json.Unmarshal(data, &operation); err != nil {
		return Operation{}, err
	}
	if operation.ID != id || !config.ValidUUID(operation.ID) || !validKind(operation.Kind) {
		return Operation{}, errInvalidOperation
	}
	return operation, nil
}

// Begin reserves the single operation slot and records it as queued.
func (s *operationSet) Begin(kind, workspaceID, networkID string) (Operation, *apperr.Error) {
	if !validKind(kind) {
		return Operation{}, apperr.New(apperr.CodeInvalidRequest)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != nil {
		return Operation{}, apperr.New(apperr.CodeConnectionChangeBusy)
	}
	id, err := config.RandomUUID()
	if err != nil {
		return Operation{}, apperr.New(apperr.CodeStateUnavailable)
	}
	operation := Operation{
		ID:          id,
		State:       StateQueued,
		Phase:       StateQueued,
		Kind:        kind,
		WorkspaceID: workspaceID,
		NetworkID:   networkID,
	}
	if err := s.writeLocked(operation); err != nil {
		return Operation{}, apperr.New(apperr.CodeConnectionChangeStartFailed)
	}
	s.active = &operation
	return operation, nil
}

// Run executes body as the reserved operation and records the outcome.
func (s *operationSet) Run(operation Operation, body operationBody) {
	defer func() {
		s.mu.Lock()
		s.active = nil
		s.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	report := func(phase string) {
		s.write(operation.ID, StateRunning, phase, operation.Kind, operation.WorkspaceID,
			operation.NetworkID, false, "", "")
	}
	report(StateQueued)

	needsDownload, aerr := body(ctx, report)
	if aerr == nil {
		s.write(operation.ID, StateCompleted, StateCompleted, operation.Kind, operation.WorkspaceID,
			operation.NetworkID, needsDownload, "", "")
		return
	}
	s.write(operation.ID, StateFailed, StateFailed, operation.Kind, operation.WorkspaceID,
		operation.NetworkID, false, aerr.Code, aerr.Message)
}

// Get returns one operation, marking it interrupted when the daemon that ran
// it is no longer working on it.
func (s *operationSet) Get(id string) (Operation, *apperr.Error) {
	if !config.ValidUUID(id) {
		return Operation{}, apperr.New(apperr.CodeInvalidConnectionOperation)
	}
	operation, err := s.load(id)
	if err != nil {
		return Operation{}, apperr.New(apperr.CodeConnectionChangeNotFound)
	}
	s.mu.Lock()
	active := s.active != nil && s.active.ID == id
	s.mu.Unlock()
	if !active && (operation.State == StateRunning || operation.State == StateQueued) {
		message := apperr.New(apperr.CodeConnectionChangeInterrupted).Message
		s.write(id, StateFailed, StateFailed, operation.Kind, operation.WorkspaceID,
			operation.NetworkID, false, apperr.CodeConnectionChangeInterrupted, message)
		operation.State = StateFailed
		operation.Phase = StateFailed
		operation.NeedsDownload = false
		operation.Error = apperr.CodeConnectionChangeInterrupted
		operation.Message = message
	}
	return operation, nil
}

// Current returns the in-flight operation, if any.
func (s *operationSet) Current() (Operation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == nil {
		return Operation{}, false
	}
	return *s.active, true
}

func (s *operationSet) write(id, state, phase, kind, workspaceID, networkID string, needsDownload bool, code, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.writeLocked(Operation{
		ID:            id,
		State:         state,
		Phase:         phase,
		Kind:          kind,
		WorkspaceID:   workspaceID,
		NetworkID:     networkID,
		NeedsDownload: needsDownload,
		Error:         code,
		Message:       message,
	})
}

func (s *operationSet) writeLocked(operation Operation) error {
	operation.UpdatedAt = time.Now().Unix()
	data, err := json.Marshal(operation)
	if err != nil {
		return err
	}
	return config.AtomicWrite(s.file(operation.ID), data, 0o600)
}
