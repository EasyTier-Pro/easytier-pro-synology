// Package httpserver exposes the local API of the DSM client on loopback.
//
// Every request is authenticated with the caller's DSM session before it is
// routed, and every answer uses the same envelope the OpenWrt client returns:
// {"ok":true,...} or {"ok":false,"error":"<code>","message":"<text>"}.
package httpserver

import (
	"encoding/json"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/apperr"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/config"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/console"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/dsmenv"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/runtime"
)

// maxBodyBytes bounds request bodies.
const maxBodyBytes = 64 << 10

// requestMarkerHeader must accompany state-changing requests. A cross-origin
// page cannot set it without a CORS preflight, which this API never allows.
const requestMarkerHeader = "X-Easytier-Request"

// Server routes the local API and serves the bundled UI.
type Server struct {
	manager *runtime.Manager
	console *console.Client
	auth    *dsmenv.Authenticator
	log     *config.Logger
	static  fs.FS
}

// New builds the local API server.
func New(manager *runtime.Manager, client *console.Client, auth *dsmenv.Authenticator, log *config.Logger, static fs.FS) *Server {
	return &Server{manager: manager, console: client, auth: auth, log: log, static: static}
}

// Handler returns the root handler: DSM-authenticated API plus static UI.
func (s *Server) Handler() http.Handler {
	api := http.NewServeMux()
	api.HandleFunc("GET /api/status", s.handleStatus)
	api.HandleFunc("GET /api/local-summary", s.handleLocalSummary)
	api.HandleFunc("POST /api/service/action", s.handleServiceAction)
	api.HandleFunc("POST /api/settings", s.handleApplySettings)
	api.HandleFunc("POST /api/auth/device/start", s.handleAuthStart)
	api.HandleFunc("POST /api/auth/device/poll", s.handleAuthPoll)
	api.HandleFunc("GET /api/auth/status", s.handleAuthStatus)
	api.HandleFunc("GET /api/auth/me", s.handleAuthMe)
	api.HandleFunc("POST /api/auth/logout", s.handleAuthLogout)
	api.HandleFunc("GET /api/workspaces/{workspace}/enrollment-options", s.handleEnrollmentOptions)
	api.HandleFunc("POST /api/workspaces/{workspace}/activate", s.handleActivate)
	api.HandleFunc("POST /api/connect-token", s.handleConnectToken)
	api.HandleFunc("POST /api/disconnect", s.handleDisconnect)
	api.HandleFunc("GET /api/connection-change/status", s.handleOperationStatus)
	api.HandleFunc("GET /api/networks", s.handleNetworks)
	api.HandleFunc("GET /api/networks/{network}/nodes", s.handleNetworkNodes)
	api.HandleFunc("POST /api/networks/{network}/join", s.handleNetworkJoin)
	api.HandleFunc("POST /api/networks/{network}/leave", s.handleNetworkLeave)
	api.HandleFunc("POST /api/runtime/download", s.handleDownloadStart)
	api.HandleFunc("GET /api/runtime/download/status", s.handleDownloadStatus)
	api.HandleFunc("GET /api/logs", s.handleLogs)
	api.HandleFunc("/api/", s.handleNotFound)

	root := http.NewServeMux()
	root.Handle("/api/", s.requireDSMSession(api))
	root.Handle("/", s.staticHandler())
	return root
}

// requireDSMSession rejects requests without an authenticated DSM
// administrator session.
func (s *Server) requireDSMSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if aerr := checkSameOrigin(r); aerr != nil {
			s.logAuthorizationFailure(r, aerr)
			writeErrorStatus(w, http.StatusForbidden, aerr)
			return
		}
		user, aerr := s.auth.Authenticate(r.Context(), r)
		if aerr != nil {
			s.logAuthorizationFailure(r, aerr)
			status := http.StatusUnauthorized
			if aerr.Code == apperr.CodeDSMAuthForbidden {
				status = http.StatusForbidden
			}
			writeErrorStatus(w, status, aerr)
			return
		}
		s.log.Printf("本机 API %s %s（用户 %s）", r.Method, r.URL.Path, user)
		next.ServeHTTP(w, r)
	})
}

// checkSameOrigin protects state-changing requests from being triggered by
// another site with the browser's DSM cookie.
func checkSameOrigin(r *http.Request) *apperr.Error {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return nil
	}
	if r.Header.Get(requestMarkerHeader) != "1" {
		return apperr.New(apperr.CodeInvalidRequest)
	}
	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
		parsed, err := url.Parse(origin)
		// nginx forwards "Host $host", which drops the port the browser used,
		// so only the hosts themselves can be compared.
		if err != nil || !strings.EqualFold(parsed.Hostname(), hostnameOf(r.Host)) {
			return apperr.New(apperr.CodeInvalidRequest)
		}
	}
	return nil
}

// hostnameOf strips an optional port from a Host header value.
func hostnameOf(host string) string {
	if name, _, err := net.SplitHostPort(host); err == nil {
		return name
	}
	return host
}

// logAuthorizationFailure records why a request was rejected. Cookie values are
// credentials and are never logged, only their names: a rejection caused by the
// cookies a browser happens to send is otherwise impossible to diagnose.
func (s *Server) logAuthorizationFailure(r *http.Request, aerr *apperr.Error) {
	names := make([]string, 0, 4)
	for _, cookie := range r.Cookies() {
		names = append(names, cookie.Name)
	}
	if len(names) == 0 {
		names = append(names, "无")
	}
	s.log.Printf("本机 API 拒绝 %s %s：%s（Host %q，X-Real-IP %q，Cookie %s，Origin %q）",
		r.Method, r.URL.Path, aerr.Code, r.Host, r.Header.Get("X-Real-IP"),
		strings.Join(names, ","), r.Header.Get("Origin"))
}

func (s *Server) staticHandler() http.Handler {
	fileServer := http.FileServer(http.FS(s.static))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		fileServer.ServeHTTP(w, r)
	})
}

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, apperr.New(apperr.CodeMethodNotFound))
}

// respond writes either the payload or the error envelope.
func (s *Server) respond(w http.ResponseWriter, payload any, aerr *apperr.Error) {
	if aerr != nil {
		writeError(w, aerr)
		return
	}
	writePayload(w, payload)
}

func writeError(w http.ResponseWriter, aerr *apperr.Error) {
	writeErrorStatus(w, http.StatusOK, aerr)
}

func writeErrorStatus(w http.ResponseWriter, status int, aerr *apperr.Error) {
	writeJSON(w, status, map[string]any{
		"ok":      false,
		"error":   aerr.Code,
		"message": aerr.Message,
	})
}

// writePayload merges the payload with {"ok":true}.
func writePayload(w http.ResponseWriter, payload any) {
	if payload == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	data, err := json.Marshal(payload)
	if err != nil {
		writeError(w, apperr.New(apperr.CodeStateUnavailable))
		return
	}
	object := map[string]any{}
	if err := json.Unmarshal(data, &object); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": json.RawMessage(data)})
		return
	}
	object["ok"] = true
	writeJSON(w, http.StatusOK, object)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(payload)
}

// decodeBody parses a JSON request body.
func decodeBody(w http.ResponseWriter, r *http.Request, target any) *apperr.Error {
	body := http.MaxBytesReader(w, r.Body, maxBodyBytes)
	defer body.Close()
	decoder := json.NewDecoder(body)
	if err := decoder.Decode(target); err != nil {
		return apperr.New(apperr.CodeInvalidRequest)
	}
	return nil
}
