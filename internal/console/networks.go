package console

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/apperr"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/config"
)

// NetworksResult lists the workspace networks plus this machine's state.
type NetworksResult struct {
	WorkspaceID string          `json:"workspace_id"`
	MachineID   string          `json:"machine_id"`
	Enrolled    bool            `json:"enrolled"`
	Machine     json.RawMessage `json:"machine"`
	Networks    json.RawMessage `json:"networks"`
}

// NodesResult lists the nodes of one network.
type NodesResult struct {
	NetworkID string `json:"network_id"`
	Nodes     []Node `json:"nodes"`
}

// LeaveResult reports the Console answer of leaving a network. AlreadyOutside
// is set when this machine had no node in the network to begin with.
type LeaveResult struct {
	Result         json.RawMessage
	AlreadyOutside bool
}

// Networks lists the networks of the active workspace and reports whether this
// machine is enrolled yet.
func (c *Client) Networks(ctx context.Context) (NetworksResult, *apperr.Error) {
	workspaceID, aerr := c.workspaceID(ctx)
	if aerr != nil {
		return NetworksResult{}, aerr
	}
	machineID, err := c.store.MachineID()
	if err != nil {
		return NetworksResult{}, apperr.New(apperr.CodeStateUnavailable)
	}
	status, networks, aerr := c.request(ctx, http.MethodGet, tenantPath(workspaceID, "/networks"), nil, "application/json", "")
	if aerr != nil {
		return NetworksResult{}, aerr
	}
	if status != http.StatusOK {
		return NetworksResult{}, apperr.New(apperr.CodeNetworkLookupFailed)
	}
	result := NetworksResult{
		WorkspaceID: workspaceID,
		MachineID:   machineID,
		Machine:     json.RawMessage("null"),
		Networks:    json.RawMessage(networks),
	}
	status, machine, aerr := c.request(ctx, http.MethodGet, tenantPath(workspaceID, "/machines/"+machineID), nil, "application/json", "")
	if aerr == nil && status == http.StatusOK {
		result.Enrolled = true
		result.Machine = json.RawMessage(machine)
	}
	return result, nil
}

// NetworkNodes lists the nodes of one network in the active workspace.
func (c *Client) NetworkNodes(ctx context.Context, networkID string) (NodesResult, *apperr.Error) {
	if !config.ValidUUID(networkID) {
		return NodesResult{}, apperr.New(apperr.CodeInvalidNetwork)
	}
	workspaceID, aerr := c.workspaceID(ctx)
	if aerr != nil {
		return NodesResult{}, aerr
	}
	status, body, aerr := c.request(ctx, http.MethodGet,
		tenantPath(workspaceID, "/networks/"+networkID+"/nodes"), nil, "application/json", "")
	if aerr != nil {
		return NodesResult{}, aerr
	}
	if status != http.StatusOK {
		return NodesResult{}, apperr.New(apperr.CodeNodeLookupFailed)
	}
	var raw []rawNode
	if aerr := decodeJSON(body, &raw); aerr != nil {
		return NodesResult{}, apperr.New(apperr.CodeInvalidConsoleResponse)
	}
	result := NodesResult{NetworkID: networkID, Nodes: make([]Node, 0, len(raw))}
	for index := range raw {
		node := raw[index].Node
		if !config.ValidUUID(node.ID) || !config.ValidUUID(node.MachineID) {
			return NodesResult{}, apperr.New(apperr.CodeInvalidConsoleResponse)
		}
		node.DisplayName = raw[index].Device.DisplayName
		node.ConnectivityState = raw[index].Device.ConnectivityState
		result.Nodes = append(result.Nodes, node)
	}
	return result, nil
}

// NetworkJoin adds this machine to a network.
func (c *Client) NetworkJoin(ctx context.Context, networkID string) (json.RawMessage, *apperr.Error) {
	if !config.ValidUUID(networkID) {
		return nil, apperr.New(apperr.CodeInvalidNetwork)
	}
	workspaceID, aerr := c.workspaceID(ctx)
	if aerr != nil {
		return nil, aerr
	}
	machineID, err := c.store.MachineID()
	if err != nil {
		return nil, apperr.New(apperr.CodeStateUnavailable)
	}
	status, machine, aerr := c.request(ctx, http.MethodGet, tenantPath(workspaceID, "/machines/"+machineID), nil, "application/json", "")
	if aerr != nil {
		return nil, aerr
	}
	if status != http.StatusOK {
		return nil, apperr.New(apperr.CodeRouterNotEnrolled)
	}
	var machinePayload struct {
		Device struct {
			ID string `json:"id"`
		} `json:"device"`
	}
	if aerr := decodeJSON(machine, &machinePayload); aerr != nil {
		return nil, apperr.New(apperr.CodeInvalidConsoleResponse)
	}
	if !config.ValidUUID(machinePayload.Device.ID) {
		return nil, apperr.New(apperr.CodeInvalidConsoleResponse)
	}
	body, err := json.Marshal(map[string]string{"device_id": machinePayload.Device.ID})
	if err != nil {
		return nil, apperr.New(apperr.CodeStateUnavailable)
	}
	status, response, aerr := c.request(ctx, http.MethodPost,
		tenantPath(workspaceID, "/networks/"+networkID+"/nodes"), body, "application/json",
		"join-"+machineID+"-"+networkID)
	if aerr != nil {
		return nil, aerr
	}
	if !isSuccess(status) {
		return nil, apperr.New(apperr.CodeNetworkJoinFailed)
	}
	return json.RawMessage(response), nil
}

// NetworkLeave removes this machine's node from a network.
func (c *Client) NetworkLeave(ctx context.Context, networkID string) (LeaveResult, *apperr.Error) {
	if !config.ValidUUID(networkID) {
		return LeaveResult{}, apperr.New(apperr.CodeInvalidNetwork)
	}
	workspaceID, aerr := c.workspaceID(ctx)
	if aerr != nil {
		return LeaveResult{}, aerr
	}
	machineID, err := c.store.MachineID()
	if err != nil {
		return LeaveResult{}, apperr.New(apperr.CodeStateUnavailable)
	}
	status, body, aerr := c.request(ctx, http.MethodGet,
		tenantPath(workspaceID, "/networks/"+networkID+"/nodes"), nil, "application/json", "")
	if aerr != nil {
		return LeaveResult{}, aerr
	}
	if status != http.StatusOK {
		return LeaveResult{}, apperr.New(apperr.CodeNetworkLookupFailed)
	}
	var nodes []Node
	if aerr := decodeJSON(body, &nodes); aerr != nil {
		return LeaveResult{}, apperr.New(apperr.CodeInvalidConsoleResponse)
	}
	nodeID := ""
	for _, node := range nodes {
		if node.MachineID == machineID && config.ValidUUID(node.ID) {
			nodeID = node.ID
			break
		}
	}
	if nodeID == "" {
		return LeaveResult{AlreadyOutside: true}, nil
	}
	status, response, aerr := c.request(ctx, http.MethodPost,
		tenantPath(workspaceID, "/nodes/"+nodeID+"/remove"), nil, "application/json",
		"leave-"+machineID+"-"+networkID)
	if aerr != nil {
		return LeaveResult{}, aerr
	}
	if !isSuccess(status) {
		return LeaveResult{}, apperr.New(apperr.CodeNetworkLeaveFailed)
	}
	return LeaveResult{Result: json.RawMessage(response)}, nil
}
