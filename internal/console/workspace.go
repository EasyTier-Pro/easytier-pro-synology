package console

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/apperr"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/config"
)

// workspaceID returns the workspace this device belongs to.
//
// A device activated through the Console sign-in flow has its workspace chosen
// by the operator and stored. A connection made with a device token alone never
// makes that choice, and every Console endpoint is tenant-scoped, so the
// workspace has to be discovered before anything else can be asked - including
// the runtime mode this device needs, which is what made a token-only
// connection unable to repair its own configuration.
func (c *Client) workspaceID(ctx context.Context) (string, *apperr.Error) {
	settings, err := c.store.Settings()
	if err != nil {
		return "", apperr.New(apperr.CodeStateUnavailable)
	}
	if config.ValidUUID(settings.ActiveWorkspaceID) {
		return settings.ActiveWorkspaceID, nil
	}
	workspaceID, aerr := c.discoverWorkspace(ctx)
	if aerr != nil {
		return "", aerr
	}
	// Remember it. The device token tied this machine to that workspace, and
	// every later Console call needs the identifier anyway.
	settings.ActiveWorkspaceID = workspaceID
	if err := c.store.SaveSettings(settings); err != nil {
		return "", apperr.New(apperr.CodeStateUnavailable)
	}
	if c.log != nil {
		c.log.Printf("已按设备令牌确定本机所属工作空间 %s", workspaceID)
	}
	return workspaceID, nil
}

// discoverWorkspace reports which of the account's workspaces knows this
// machine.
func (c *Client) discoverWorkspace(ctx context.Context) (string, *apperr.Error) {
	if !c.LoggedIn() {
		// A device token on its own cannot list an account's workspaces; only a
		// Console session can.
		return "", apperr.New(apperr.CodeNotAuthenticated)
	}
	machineID, err := c.store.MachineID()
	if err != nil {
		return "", apperr.New(apperr.CodeStateUnavailable)
	}
	account, aerr := c.AuthMe(ctx)
	if aerr != nil {
		return "", aerr
	}
	var payload struct {
		Tenants []struct {
			ID string `json:"id"`
		} `json:"tenants"`
	}
	if err := json.Unmarshal(account, &payload); err != nil {
		return "", apperr.New(apperr.CodeInvalidConsoleResponse)
	}
	for _, tenant := range payload.Tenants {
		if !config.ValidUUID(tenant.ID) {
			continue
		}
		status, _, aerr := c.request(ctx, http.MethodGet,
			tenantPath(tenant.ID, "/machines/"+machineID), nil, "application/json", "")
		if aerr != nil {
			return "", aerr
		}
		if status == http.StatusOK {
			return tenant.ID, nil
		}
	}
	// The token has not registered this machine anywhere yet. The core
	// registers it as soon as it connects, so a later attempt can succeed; the
	// caller retries.
	return "", apperr.New(apperr.CodeNoWorkspace)
}
