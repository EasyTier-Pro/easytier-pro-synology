package console

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/apperr"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/config"
)

// enrollmentModes accepted by Activate.
const (
	ModeAuto      = "auto"
	ModeLocal     = "local"
	ModeRecover   = "recover"
	ModeExisting  = "existing"
	ModeShared    = "shared"
	ModeDedicated = "dedicated"
)

// ActivateResult is what a completed Console-side activation produced.
type ActivateResult struct {
	WorkspaceID    string `json:"workspace_id"`
	MachineID      string `json:"machine_id"`
	ExistingDevice bool   `json:"existing_device"`
	BootstrapToken string `json:"-"`
	ConfigServer   string `json:"-"`
}

// ValidEnrollmentMode reports whether mode is one Activate understands.
func ValidEnrollmentMode(mode string) bool {
	switch mode {
	case ModeAuto, ModeLocal, ModeRecover, ModeExisting, ModeShared, ModeDedicated:
		return true
	}
	return false
}

// EnrollmentOptions describes the enrollment choices available for one
// workspace: whether this device already exists there, which key it currently
// uses, and which reusable keys could be used instead.
func (c *Client) EnrollmentOptions(ctx context.Context, workspaceID string) (EnrollmentOptions, *apperr.Error) {
	if !config.ValidUUID(workspaceID) {
		return EnrollmentOptions{}, apperr.New(apperr.CodeInvalidWorkspace)
	}
	if aerr := c.requireWorkspace(ctx, workspaceID); aerr != nil {
		return EnrollmentOptions{}, aerr
	}
	machineID, err := c.store.MachineID()
	if err != nil {
		return EnrollmentOptions{}, apperr.New(apperr.CodeStateUnavailable)
	}
	devices, aerr := c.workspaceDevices(ctx, workspaceID)
	if aerr != nil {
		return EnrollmentOptions{}, aerr
	}
	device, existing := findMachineDevice(devices, machineID)
	keys, aerr := c.enrollmentKeys(ctx, workspaceID)
	if aerr != nil {
		return EnrollmentOptions{}, aerr
	}

	options := EnrollmentOptions{
		WorkspaceID:    workspaceID,
		MachineID:      machineID,
		ExistingDevice: existing,
		ReusableKeys:   []EnrollmentKeyView{},
	}
	currentKeyID := device.EnrollmentKeyID
	if config.ValidUUID(currentKeyID) {
		key, ok := findEnrollmentKey(keys, currentKeyID, false)
		if ok {
			view := enrollmentKeyView(key)
			options.CurrentKey = &view
		} else {
			options.CurrentKeyUnavailable = true
		}
	}
	settings, err := c.store.Settings()
	if err != nil {
		return EnrollmentOptions{}, apperr.New(apperr.CodeStateUnavailable)
	}
	options.LocalConnection = settings.ActiveWorkspaceID == workspaceID && c.store.HasBootstrapToken()
	for _, key := range keys {
		if key.ID == currentKeyID || !enrollmentKeyUsable(key, true) {
			continue
		}
		options.ReusableKeys = append(options.ReusableKeys, enrollmentKeyView(key))
	}
	return options, nil
}

// Activate resolves the enrollment key for this device in the selected
// workspace and returns everything the runtime needs to connect. It performs
// no local state change.
func (c *Client) Activate(ctx context.Context, workspaceID, mode, keyID string) (ActivateResult, *apperr.Error) {
	if !config.ValidUUID(workspaceID) {
		return ActivateResult{}, apperr.New(apperr.CodeInvalidWorkspace)
	}
	if !ValidEnrollmentMode(mode) {
		return ActivateResult{}, apperr.New(apperr.CodeInvalidEnrollmentMode)
	}
	if mode == ModeExisting && !config.ValidUUID(keyID) {
		return ActivateResult{}, apperr.New(apperr.CodeEnrollmentChoiceRequired)
	}
	if keyID != "" && !config.ValidUUID(keyID) {
		return ActivateResult{}, apperr.New(apperr.CodeEnrollmentChoiceRequired)
	}
	if aerr := c.requireWorkspace(ctx, workspaceID); aerr != nil {
		return ActivateResult{}, aerr
	}
	machineID, err := c.store.MachineID()
	if err != nil {
		return ActivateResult{}, apperr.New(apperr.CodeStateUnavailable)
	}
	settings, err := c.store.Settings()
	if err != nil {
		return ActivateResult{}, apperr.New(apperr.CodeStateUnavailable)
	}
	devices, aerr := c.workspaceDevices(ctx, workspaceID)
	if aerr != nil {
		return ActivateResult{}, aerr
	}
	device, existing := findMachineDevice(devices, machineID)
	currentKeyID := device.EnrollmentKeyID

	if mode == ModeAuto {
		switch {
		case settings.ActiveWorkspaceID == workspaceID && c.store.HasBootstrapToken():
			mode = ModeLocal
		case config.ValidUUID(currentKeyID):
			mode = ModeRecover
		default:
			keys, aerr := c.enrollmentKeys(ctx, workspaceID)
			if aerr != nil {
				return ActivateResult{}, aerr
			}
			if reusable, ok := firstReusableKey(keys); ok {
				mode = ModeExisting
				keyID = reusable
			} else {
				mode = ModeShared
			}
		}
	}

	release, aerr := c.LatestRelease(ctx)
	if aerr != nil {
		return ActivateResult{}, aerr
	}
	configServer := release.ConfigServerURL()
	if !config.ValidConfigServer(configServer) {
		return ActivateResult{}, apperr.New(apperr.CodeInvalidConfigServer)
	}

	var bootstrapToken string
	switch mode {
	case ModeLocal:
		if settings.ActiveWorkspaceID != workspaceID || !c.store.HasBootstrapToken() {
			return ActivateResult{}, apperr.New(apperr.CodeEnrollmentKeyUnavailable)
		}
		bootstrapToken, err = c.store.ReadBootstrapToken()
		if err != nil {
			return ActivateResult{}, apperr.New(apperr.CodeStateUnavailable)
		}
	case ModeRecover:
		if !config.ValidUUID(currentKeyID) {
			return ActivateResult{}, apperr.New(apperr.CodeEnrollmentKeyUnavailable)
		}
		bootstrapToken, aerr = c.EnrollmentKeySecret(ctx, workspaceID, currentKeyID)
		if aerr != nil {
			return ActivateResult{}, apperr.New(apperr.CodeEnrollmentKeyUnavailable)
		}
	case ModeExisting:
		keys, aerr := c.enrollmentKeys(ctx, workspaceID)
		if aerr != nil {
			return ActivateResult{}, aerr
		}
		if _, ok := findEnrollmentKey(keys, keyID, true); !ok {
			return ActivateResult{}, apperr.New(apperr.CodeEnrollmentKeyUnavailable)
		}
		bootstrapToken, aerr = c.EnrollmentKeySecret(ctx, workspaceID, keyID)
		if aerr != nil {
			return ActivateResult{}, apperr.New(apperr.CodeEnrollmentKeyUnavailable)
		}
	case ModeShared, ModeDedicated:
		bootstrapToken, aerr = c.createEnrollmentKey(ctx, workspaceID, machineID, mode)
		if aerr != nil {
			return ActivateResult{}, aerr
		}
	default:
		return ActivateResult{}, apperr.New(apperr.CodeInvalidEnrollmentMode)
	}
	if !config.ValidSecret(bootstrapToken) {
		return ActivateResult{}, apperr.New(apperr.CodeInvalidConsoleResponse)
	}
	return ActivateResult{
		WorkspaceID:    workspaceID,
		MachineID:      machineID,
		ExistingDevice: existing,
		BootstrapToken: bootstrapToken,
		ConfigServer:   config.NormalizeConfigServer(configServer),
	}, nil
}

// EnrollmentKeySecret reads the enrollment token of one key.
func (c *Client) EnrollmentKeySecret(ctx context.Context, workspaceID, keyID string) (string, *apperr.Error) {
	if !config.ValidUUID(workspaceID) || !config.ValidUUID(keyID) {
		return "", apperr.New(apperr.CodeInvalidWorkspace)
	}
	status, body, aerr := c.request(ctx, http.MethodGet,
		tenantPath(workspaceID, "/device-enrollment-keys/"+keyID+"/secret"), nil, "application/json", "")
	if aerr != nil {
		return "", aerr
	}
	if status != http.StatusOK {
		return "", apperr.New(apperr.CodeEnrollmentKeyUnavailable)
	}
	var payload struct {
		BootstrapToken string `json:"bootstrap_token"`
	}
	if aerr := decodeJSON(body, &payload); aerr != nil {
		return "", apperr.New(apperr.CodeInvalidConsoleResponse)
	}
	if !config.ValidSecret(payload.BootstrapToken) {
		return "", apperr.New(apperr.CodeEnrollmentKeyUnavailable)
	}
	return payload.BootstrapToken, nil
}

// createEnrollmentKey creates a shared or dedicated key for this device.
func (c *Client) createEnrollmentKey(ctx context.Context, workspaceID, machineID, mode string) (string, *apperr.Error) {
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		hostname = "Synology NAS"
	}
	displayName := "Synology NAS"
	if mode == ModeDedicated {
		suffix := machineID
		if len(suffix) > 8 {
			suffix = suffix[:8]
		}
		displayName = fmt.Sprintf("Synology %s-%s", hostname, suffix)
	}
	body, err := json.Marshal(map[string]any{
		"display_name": displayName,
		"reusable":     mode == ModeShared,
		"pre_approved": true,
	})
	if err != nil {
		return "", apperr.New(apperr.CodeStateUnavailable)
	}
	status, payload, aerr := c.request(ctx, http.MethodPost,
		tenantPath(workspaceID, "/device-enrollment-keys"), body, "application/json",
		"enroll-"+machineID+"-"+mode)
	if aerr != nil {
		return "", aerr
	}
	if !isSuccess(status) {
		return "", apperr.New(apperr.CodeEnrollmentFailed)
	}
	var key struct {
		BootstrapToken string `json:"bootstrap_token"`
	}
	if aerr := decodeJSON(payload, &key); aerr != nil {
		return "", apperr.New(apperr.CodeEnrollmentFailed)
	}
	if !config.ValidSecret(key.BootstrapToken) {
		return "", apperr.New(apperr.CodeEnrollmentFailed)
	}
	return key.BootstrapToken, nil
}

// requireWorkspace verifies the signed-in account can see workspaceID.
func (c *Client) requireWorkspace(ctx context.Context, workspaceID string) *apperr.Error {
	account, aerr := c.AuthMe(ctx)
	if aerr != nil {
		return aerr
	}
	var payload struct {
		Tenants []struct {
			ID string `json:"id"`
		} `json:"tenants"`
	}
	if err := json.Unmarshal(account, &payload); err != nil {
		return apperr.New(apperr.CodeInvalidConsoleResponse)
	}
	for _, tenant := range payload.Tenants {
		if tenant.ID == workspaceID {
			return nil
		}
	}
	return apperr.New(apperr.CodeWorkspaceAccessDenied)
}

func (c *Client) workspaceDevices(ctx context.Context, workspaceID string) ([]Device, *apperr.Error) {
	status, body, aerr := c.request(ctx, http.MethodGet, tenantPath(workspaceID, "/devices"), nil, "application/json", "")
	if aerr != nil {
		return nil, aerr
	}
	if status != http.StatusOK {
		return nil, apperr.New(apperr.CodeEnrollmentLookupFailed)
	}
	var devices []Device
	if aerr := decodeJSON(body, &devices); aerr != nil {
		return nil, apperr.New(apperr.CodeEnrollmentLookupFailed)
	}
	return devices, nil
}

func (c *Client) enrollmentKeys(ctx context.Context, workspaceID string) ([]EnrollmentKey, *apperr.Error) {
	status, body, aerr := c.request(ctx, http.MethodGet, tenantPath(workspaceID, "/device-enrollment-keys"), nil, "application/json", "")
	if aerr != nil {
		return nil, aerr
	}
	if status != http.StatusOK {
		return nil, apperr.New(apperr.CodeEnrollmentLookupFailed)
	}
	var keys []EnrollmentKey
	if aerr := decodeJSON(body, &keys); aerr != nil {
		return nil, apperr.New(apperr.CodeEnrollmentLookupFailed)
	}
	return keys, nil
}

func findMachineDevice(devices []Device, machineID string) (Device, bool) {
	for _, device := range devices {
		if device.MachineID == machineID {
			return device, true
		}
	}
	return Device{}, false
}

func findEnrollmentKey(keys []EnrollmentKey, keyID string, reusableRequired bool) (EnrollmentKey, bool) {
	for _, key := range keys {
		if key.ID == keyID && enrollmentKeyUsable(key, reusableRequired) {
			return key, true
		}
	}
	return EnrollmentKey{}, false
}

func firstReusableKey(keys []EnrollmentKey) (string, bool) {
	for _, key := range keys {
		if enrollmentKeyUsable(key, true) {
			return key.ID, true
		}
	}
	return "", false
}

// enrollmentKeyUsable reports whether a key can still enroll a device.
func enrollmentKeyUsable(key EnrollmentKey, reusableRequired bool) bool {
	if !config.ValidUUID(key.ID) {
		return false
	}
	if reusableRequired && !key.Reusable {
		return false
	}
	if key.Revoked {
		return false
	}
	if key.LifecycleState == "deleted" || key.DesiredState == "absent" {
		return false
	}
	if key.ExpiresAt != "" {
		expires, ok := rfc3339Epoch(key.ExpiresAt)
		if !ok || expires <= now() {
			return false
		}
	}
	return true
}

func enrollmentKeyView(key EnrollmentKey) EnrollmentKeyView {
	return EnrollmentKeyView{
		ID:          key.ID,
		DisplayName: key.DisplayName,
		KeyCode:     key.KeyCode,
		Reusable:    key.Reusable,
		PreApproved: key.PreApproved,
		UsedCount:   key.UsedCount,
		ExpiresAt:   key.ExpiresAt,
	}
}

func rfc3339Epoch(value string) (int64, bool) {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return 0, false
	}
	return parsed.Unix(), true
}
