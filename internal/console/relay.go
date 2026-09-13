package console

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/apperr"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/config"
)

// newIdempotencyKey builds the key of one Console write.
//
// The key must be unique per attempt. The Console stores the operation of a
// resource under its idempotency key and, when the key is seen again, returns
// the recorded operation without doing anything. A fixed key derived only from
// the machine and the network therefore turns every later write into a no-op:
// a node rejoining a network it has already left would reuse the key of the
// original attach and never be created again.
func newIdempotencyKey(prefix, machineID, networkID string) string {
	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		return prefix + "-" + machineID + "-" + networkID + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return prefix + "-" + machineID + "-" + networkID + "-" + hex.EncodeToString(suffix)
}

// machineNetworks is the part of the machine payload this daemon needs: the
// device it belongs to, and the networks that device already has a node in.
type machineNetworks struct {
	Device struct {
		ID string `json:"id"`
	} `json:"device"`
	Networks []struct {
		ID string `json:"id"`
	} `json:"networks"`
}

// nodeConfigView is the part of the node config the relay-mode switch needs.
type nodeConfigView struct {
	Override map[string]any `json:"override"`
}

// deviceDefaultConfigView is the device-declared settings the Console holds.
type deviceDefaultConfigView struct {
	NoTun      *bool `json:"no_tun"`
	BindDevice *bool `json:"bind_device"`
}

// MachineState is what this device is, as the Console sees it.
type MachineState struct {
	// DeviceID is the Console's device record for this machine, and is what the
	// device-level declaration is written against.
	DeviceID string
	// NetworkIDs are the networks that device already has a node in.
	NetworkIDs []string
}

// MachineState reads this machine's device record and its network memberships.
func (c *Client) MachineState(ctx context.Context) (MachineState, *apperr.Error) {
	workspaceID, aerr := c.workspaceID(ctx)
	if aerr != nil {
		return MachineState{}, aerr
	}
	machineID, err := c.store.MachineID()
	if err != nil {
		return MachineState{}, apperr.New(apperr.CodeStateUnavailable)
	}
	status, payload, aerr := c.request(ctx, http.MethodGet,
		tenantPath(workspaceID, "/machines/"+machineID), nil, "application/json", "")
	if aerr != nil {
		return MachineState{}, aerr
	}
	if status == http.StatusNotFound {
		// The device is not enrolled yet: there is nothing to configure.
		return MachineState{}, nil
	}
	if status != http.StatusOK {
		return MachineState{}, apperr.New(apperr.CodeRouterNotEnrolled)
	}
	var machine machineNetworks
	if aerr := decodeJSON(payload, &machine); aerr != nil {
		return MachineState{}, apperr.New(apperr.CodeInvalidConsoleResponse)
	}
	state := MachineState{DeviceID: machine.Device.ID}
	for _, network := range machine.Networks {
		if config.ValidUUID(network.ID) {
			state.NetworkIDs = append(state.NetworkIDs, network.ID)
		}
	}
	return state, nil
}

// nodeIDForMachine returns this machine's node in one network, or an empty
// string when the machine is not a member of that network.
func (c *Client) nodeIDForMachine(ctx context.Context, workspaceID, networkID, machineID string) (string, *apperr.Error) {
	status, payload, aerr := c.request(ctx, http.MethodGet,
		tenantPath(workspaceID, "/networks/"+networkID+"/nodes"), nil, "application/json", "")
	if aerr != nil {
		return "", aerr
	}
	if status != http.StatusOK {
		return "", apperr.New(apperr.CodeNodeLookupFailed)
	}
	var nodes []Node
	if aerr := decodeJSON(payload, &nodes); aerr != nil {
		return "", apperr.New(apperr.CodeInvalidConsoleResponse)
	}
	for _, node := range nodes {
		if node.MachineID == machineID && config.ValidUUID(node.ID) {
			return node.ID, nil
		}
	}
	return "", nil
}

// DeclareDeviceDefaults tells the Console which settings every node of this
// device must start from. It reports whether the Console supports the
// declaration at all.
//
// A node-level override can only be written once the node exists, which is
// after the Console has already pushed that node a configuration this device
// cannot honour. The declaration is held on the device instead, so a node is
// built correctly the first time it is pushed and never has to be repaired:
// this is the difference between a node that fails for a minute and one that
// simply works.
//
// An older Console does not know the endpoint. That is reported rather than
// treated as a failure, because the node-level override still covers such a
// Console and a device must not refuse to connect over a missing improvement.
func (c *Client) DeclareDeviceDefaults(ctx context.Context, deviceID string, mode NodeMode) (bool, *apperr.Error) {
	if !config.ValidUUID(deviceID) {
		return false, apperr.New(apperr.CodeRouterNotEnrolled)
	}
	workspaceID, aerr := c.workspaceID(ctx)
	if aerr != nil {
		return false, aerr
	}
	path := tenantPath(workspaceID, "/devices/"+deviceID+"/default-config")
	status, payload, aerr := c.request(ctx, http.MethodGet, path, nil, "application/json", "")
	if aerr != nil {
		return false, aerr
	}
	switch status {
	case http.StatusOK:
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusForbidden:
		// This Console has no device-level declaration, so the node override
		// remains the only place the mode can be expressed.
		return false, nil
	default:
		return false, apperr.New(apperr.CodeRelayModeFailed)
	}
	var view deviceDefaultConfigView
	if aerr := decodeJSON(payload, &view); aerr != nil {
		return false, apperr.New(apperr.CodeInvalidConsoleResponse)
	}
	// The endpoint replaces the whole declaration, so it is written only when
	// it would differ. The settings have opposite core defaults, so each is
	// declared only when it has to be: no_tun is off unless asked for, and
	// bind_device is on unless turned off.
	declaration := map[string]any{}
	if mode.NoTun {
		declaration["no_tun"] = true
	}
	if mode.DisableBindDevice {
		declaration["bind_device"] = false
	}
	if declarationEquals(view, mode) {
		return true, nil
	}
	body, err := json.Marshal(declaration)
	if err != nil {
		return true, apperr.New(apperr.CodeStateUnavailable)
	}
	status, _, aerr = c.request(ctx, http.MethodPut, path, body, "application/json",
		newIdempotencyKey("device-mode", deviceID, ""))
	if aerr != nil {
		return true, aerr
	}
	if !isSuccess(status) {
		return true, apperr.New(apperr.CodeRelayModeFailed)
	}
	return true, nil
}

// declarationEquals reports whether the stored declaration already expresses
// the requested mode.
func declarationEquals(view deviceDefaultConfigView, mode NodeMode) bool {
	noTun := view.NoTun != nil && *view.NoTun
	bindDevice := view.BindDevice == nil || *view.BindDevice
	return noTun == mode.NoTun && bindDevice == !mode.DisableBindDevice
}

// NodeMode is the configuration this device needs on the Console.
//
// Both settings exist because of capabilities the package cannot hold, and each
// one removes a different requirement:
//
//   - NoTun keeps the core from creating a virtual interface (CAP_NET_ADMIN).
//   - DisableBindDevice keeps it from binding its sockets to an interface
//     (CAP_NET_RAW). Without it the core fails every peer connection, so the
//     node registers and looks online while its peer list stays empty.
type NodeMode struct {
	NoTun             bool
	DisableBindDevice bool
}

// SetNodeMode makes the Console's node override match the requested mode. It
// reports whether anything had to change.
//
// The Console builds the runtime configuration of a node from the network
// defaults merged with the node's own override, and both settings are part of
// that projection. The core cannot be told any of this on its command line,
// because in secure mode its instances come from the Console, so the settings
// have to be written here.
func (c *Client) SetNodeMode(ctx context.Context, networkID string, mode NodeMode) (bool, *apperr.Error) {
	if !config.ValidUUID(networkID) {
		return false, apperr.New(apperr.CodeInvalidNetwork)
	}
	workspaceID, aerr := c.workspaceID(ctx)
	if aerr != nil {
		return false, aerr
	}
	machineID, err := c.store.MachineID()
	if err != nil {
		return false, apperr.New(apperr.CodeStateUnavailable)
	}
	nodeID, aerr := c.nodeIDForMachine(ctx, workspaceID, networkID, machineID)
	if aerr != nil {
		return false, aerr
	}
	if nodeID == "" {
		// The machine is listed as a member of this network but the network
		// does not report its node. Reporting this rather than succeeding keeps
		// a transient inconsistency from silently leaving the device on a mode
		// its core cannot honour.
		return false, apperr.New(apperr.CodeNodeLookupFailed)
	}
	status, payload, aerr := c.request(ctx, http.MethodGet,
		tenantPath(workspaceID, "/nodes/"+nodeID+"/config"), nil, "application/json", "")
	if aerr != nil {
		return false, aerr
	}
	if status != http.StatusOK {
		return false, apperr.New(apperr.CodeNodeLookupFailed)
	}
	var view nodeConfigView
	if aerr := decodeJSON(payload, &view); aerr != nil {
		return false, apperr.New(apperr.CodeInvalidConsoleResponse)
	}
	// The endpoint replaces the whole override, so the existing one is merged
	// rather than overwritten: other Console settings must survive.
	override := view.Override
	if override == nil {
		override = map[string]any{}
	}
	// The two settings have opposite defaults in the core: no_tun is off unless
	// asked for, bind_device is on unless turned off. Each is therefore written
	// as an explicit override only when it differs from its default.
	changed := setOverrideBool(override, "no_tun", mode.NoTun, false)
	changed = setOverrideBool(override, "bind_device", !mode.DisableBindDevice, true) || changed
	if !changed {
		return false, nil
	}
	body, err := json.Marshal(override)
	if err != nil {
		return false, apperr.New(apperr.CodeStateUnavailable)
	}
	status, _, aerr = c.request(ctx, http.MethodPut,
		tenantPath(workspaceID, "/nodes/"+nodeID+"/config"), body, "application/json",
		newIdempotencyKey("mode", machineID, networkID))
	if aerr != nil {
		return false, aerr
	}
	if !isSuccess(status) {
		return false, apperr.New(apperr.CodeRelayModeFailed)
	}
	return true, nil
}

// setOverrideBool sets one boolean override so that the node's effective value
// becomes value, given what the core defaults that setting to. The key is kept
// only when it has to override that default, and the function reports whether
// the override changed at all - a request that changes nothing must not rewrite
// the Console configuration.
func setOverrideBool(override map[string]any, key string, value, coreDefault bool) bool {
	current, present := override[key].(bool)
	if present {
		if current == value {
			return false
		}
	} else if value == coreDefault {
		return false
	}
	if value == coreDefault {
		delete(override, key)
	} else {
		override[key] = value
	}
	return true
}
