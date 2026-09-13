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
	workspaceID, aerr := c.workspaceID()
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

// SetNodeNoTun turns relay mode on or off for this machine's node in one
// network. It reports whether the Console setting had to be changed.
//
// The Console builds the runtime configuration of a node from the network
// defaults merged with the node's own override, and no_tun is part of that
// projection: the core itself cannot be told to run without a TUN device on
// the command line, because in secure mode the instance configuration comes
// from the Console. Making the device a relay therefore means changing this
// setting on the Console.
func (c *Client) SetNodeNoTun(ctx context.Context, networkID string, enabled bool) (bool, *apperr.Error) {
	if !config.ValidUUID(networkID) {
		return false, apperr.New(apperr.CodeInvalidNetwork)
	}
	workspaceID, aerr := c.workspaceID()
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
	current, _ := override["no_tun"].(bool)
	if current == enabled {
		return false, nil
	}
	if enabled {
		override["no_tun"] = true
	} else {
		delete(override, "no_tun")
	}
	body, err := json.Marshal(override)
	if err != nil {
		return false, apperr.New(apperr.CodeStateUnavailable)
	}
	status, _, aerr = c.request(ctx, http.MethodPut,
		tenantPath(workspaceID, "/nodes/"+nodeID+"/config"), body, "application/json",
		newIdempotencyKey("relay", machineID, networkID))
	if aerr != nil {
		return false, aerr
	}
	if !isSuccess(status) {
		return false, apperr.New(apperr.CodeRelayModeFailed)
	}
	return true, nil
}
