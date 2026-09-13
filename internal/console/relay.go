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

// machineNetworks is the part of the machine payload that lists the networks
// this device already has a node in.
type machineNetworks struct {
	Networks []struct {
		ID string `json:"id"`
	} `json:"networks"`
}

// nodeConfigView is the part of the node config the relay-mode switch needs.
type nodeConfigView struct {
	Override map[string]any `json:"override"`
}

// EnrolledNetworkIDs lists the networks this machine has a node in.
func (c *Client) EnrolledNetworkIDs(ctx context.Context) ([]string, *apperr.Error) {
	workspaceID, aerr := c.workspaceID(ctx)
	if aerr != nil {
		return nil, aerr
	}
	machineID, err := c.store.MachineID()
	if err != nil {
		return nil, apperr.New(apperr.CodeStateUnavailable)
	}
	status, payload, aerr := c.request(ctx, http.MethodGet,
		tenantPath(workspaceID, "/machines/"+machineID), nil, "application/json", "")
	if aerr != nil {
		return nil, aerr
	}
	if status == http.StatusNotFound {
		// The device is not enrolled yet: there is no node to configure.
		return nil, nil
	}
	if status != http.StatusOK {
		return nil, apperr.New(apperr.CodeRouterNotEnrolled)
	}
	var machine machineNetworks
	if aerr := decodeJSON(payload, &machine); aerr != nil {
		return nil, apperr.New(apperr.CodeInvalidConsoleResponse)
	}
	ids := make([]string, 0, len(machine.Networks))
	for _, network := range machine.Networks {
		if config.ValidUUID(network.ID) {
			ids = append(ids, network.ID)
		}
	}
	return ids, nil
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
