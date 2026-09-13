package httpserver

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/apperr"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/config"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/runtime"
)

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	status, aerr := s.manager.Status()
	s.respond(w, status, aerr)
}

func (s *Server) handleLocalSummary(w http.ResponseWriter, r *http.Request) {
	summary, aerr := s.manager.LocalSummary(r.Context())
	s.respond(w, summary, aerr)
}

func (s *Server) handleServiceAction(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string `json:"action"`
	}
	if aerr := decodeBody(w, r, &body); aerr != nil {
		s.respond(w, nil, aerr)
		return
	}
	s.respond(w, nil, s.manager.ServiceAction(r.Context(), body.Action))
}

func (s *Server) handleApplySettings(w http.ResponseWriter, r *http.Request) {
	var body config.Settings
	if aerr := decodeBody(w, r, &body); aerr != nil {
		s.respond(w, nil, aerr)
		return
	}
	s.respond(w, nil, s.manager.ApplySettings(r.Context(), body))
}

func (s *Server) handleAuthStart(w http.ResponseWriter, r *http.Request) {
	result, aerr := s.console.AuthStart(r.Context())
	s.respond(w, result, aerr)
}

func (s *Server) handleAuthPoll(w http.ResponseWriter, r *http.Request) {
	result, aerr := s.console.AuthPoll(r.Context())
	s.respond(w, result, aerr)
}

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	result, aerr := s.console.AuthStatus()
	s.respond(w, result, aerr)
}

func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	account, aerr := s.console.AuthMe(r.Context())
	if aerr != nil {
		s.respond(w, nil, aerr)
		return
	}
	s.respond(w, map[string]json.RawMessage{"account": account}, nil)
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	s.respond(w, nil, s.console.Logout())
}

func (s *Server) handleEnrollmentOptions(w http.ResponseWriter, r *http.Request) {
	options, aerr := s.console.EnrollmentOptions(r.Context(), r.PathValue("workspace"))
	s.respond(w, options, aerr)
}

func (s *Server) handleActivate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		EnrollmentMode  string `json:"enrollment_mode"`
		EnrollmentKeyID string `json:"enrollment_key_id"`
	}
	if aerr := decodeOptionalBody(w, r, &body); aerr != nil {
		s.respond(w, nil, aerr)
		return
	}
	if body.EnrollmentMode == "" {
		body.EnrollmentMode = "auto"
	}
	operation, aerr := s.manager.Activate(r.Context(), r.PathValue("workspace"), body.EnrollmentMode, body.EnrollmentKeyID)
	if aerr != nil {
		s.respond(w, nil, aerr)
		return
	}
	s.respond(w, operationAccepted(operation), nil)
}

func (s *Server) handleConnectToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		BootstrapToken string `json:"bootstrap_token"`
		ConfigServer   string `json:"config_server"`
	}
	if aerr := decodeBody(w, r, &body); aerr != nil {
		s.respond(w, nil, aerr)
		return
	}
	operation, aerr := s.manager.ConnectToken(r.Context(), body.BootstrapToken, body.ConfigServer)
	if aerr != nil {
		s.respond(w, nil, aerr)
		return
	}
	s.respond(w, operationAccepted(operation), nil)
}

func (s *Server) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	operation, aerr := s.manager.Disconnect(r.Context())
	if aerr != nil {
		s.respond(w, nil, aerr)
		return
	}
	s.respond(w, operationAccepted(operation), nil)
}

func (s *Server) handleOperationStatus(w http.ResponseWriter, r *http.Request) {
	operation, aerr := s.manager.OperationStatus(r.URL.Query().Get("operation_id"))
	s.respond(w, operation, aerr)
}

func (s *Server) handleNetworks(w http.ResponseWriter, r *http.Request) {
	result, aerr := s.console.Networks(r.Context())
	s.respond(w, result, aerr)
}

func (s *Server) handleNetworkNodes(w http.ResponseWriter, r *http.Request) {
	result, aerr := s.console.NetworkNodes(r.Context(), r.PathValue("network"))
	s.respond(w, result, aerr)
}

func (s *Server) handleNetworkJoin(w http.ResponseWriter, r *http.Request) {
	operation, aerr := s.manager.NetworkJoin(r.Context(), r.PathValue("network"))
	if aerr != nil {
		s.respond(w, nil, aerr)
		return
	}
	s.respond(w, operationAccepted(operation), nil)
}

func (s *Server) handleNetworkLeave(w http.ResponseWriter, r *http.Request) {
	operation, aerr := s.manager.NetworkLeave(r.Context(), r.PathValue("network"))
	if aerr != nil {
		s.respond(w, nil, aerr)
		return
	}
	s.respond(w, operationAccepted(operation), nil)
}

func (s *Server) handleDownloadStart(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Version string `json:"version"`
	}
	if aerr := decodeOptionalBody(w, r, &body); aerr != nil {
		s.respond(w, nil, aerr)
		return
	}
	status, aerr := s.manager.DownloadStart(strings.TrimSpace(body.Version))
	s.respond(w, status, aerr)
}

func (s *Server) handleDownloadStatus(w http.ResponseWriter, r *http.Request) {
	s.respond(w, s.manager.DownloadStatus(), nil)
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	lines := 0
	if value := r.URL.Query().Get("lines"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			s.respond(w, nil, apperr.New(apperr.CodeInvalidRequest))
			return
		}
		lines = parsed
	}
	result, aerr := s.manager.Logs(lines)
	s.respond(w, result, aerr)
}

// operationAccepted is the answer of every asynchronous action.
type operationAcceptedResult struct {
	OperationID   string `json:"operation_id"`
	State         string `json:"state"`
	Kind          string `json:"kind"`
	WorkspaceID   string `json:"workspace_id,omitempty"`
	NetworkID     string `json:"network_id,omitempty"`
	NeedsDownload bool   `json:"needs_download"`
}

func operationAccepted(operation runtime.Operation) operationAcceptedResult {
	return operationAcceptedResult{
		OperationID:   operation.ID,
		State:         operation.State,
		Kind:          operation.Kind,
		WorkspaceID:   operation.WorkspaceID,
		NetworkID:     operation.NetworkID,
		NeedsDownload: operation.NeedsDownload,
	}
}

// decodeOptionalBody accepts an empty body as "no fields set".
func decodeOptionalBody(w http.ResponseWriter, r *http.Request, target any) *apperr.Error {
	body := http.MaxBytesReader(w, r.Body, maxBodyBytes)
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil {
		return apperr.New(apperr.CodeInvalidRequest)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, target); err != nil {
		return apperr.New(apperr.CodeInvalidRequest)
	}
	return nil
}
