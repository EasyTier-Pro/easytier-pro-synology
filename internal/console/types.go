package console

import "encoding/json"

// Session is the stored OAuth session for EasyTier Console. Only the access
// and refresh tokens are secrets; the rest describes their lifetime.
type Session struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
	Scope        string `json:"scope,omitempty"`
	ExpiresIn    int64  `json:"expires_in"`
	ObtainedAt   int64  `json:"obtained_at"`
	ExpiresAt    int64  `json:"expires_at"`
}

// DeviceAuth is the pending device authorization code flow state.
type DeviceAuth struct {
	DeviceCode string `json:"device_code"`
	ExpiresAt  int64  `json:"expires_at"`
	Interval   int64  `json:"interval"`
	NextPoll   int64  `json:"next_poll"`
}

// Artifact describes one published runtime archive.
type Artifact struct {
	SHA256 string      `json:"sha256"`
	Size   json.Number `json:"size"`
}

// VersionInfo is one release channel of EasyTier.
type VersionInfo struct {
	Version   string              `json:"version"`
	Artifacts map[string]Artifact `json:"artifacts"`
}

// Release is the Console answer to GET /api/v1/releases/latest.
type Release struct {
	Stable             VersionInfo `json:"stable"`
	Testing            VersionInfo `json:"testing"`
	WebConfigServerURL string      `json:"web_config_server_url"`
}

// Device is one registered machine inside a workspace.
type Device struct {
	ID              string `json:"id"`
	MachineID       string `json:"machine_id"`
	EnrollmentKeyID string `json:"enrollment_key_id"`
}

// EnrollmentKey is one device enrollment key of a workspace.
type EnrollmentKey struct {
	ID             string `json:"id"`
	DisplayName    string `json:"display_name"`
	KeyCode        string `json:"key_code"`
	Reusable       bool   `json:"reusable"`
	PreApproved    bool   `json:"pre_approved"`
	Revoked        bool   `json:"revoked"`
	LifecycleState string `json:"lifecycle_state"`
	DesiredState   string `json:"desired_state"`
	UsedCount      int64  `json:"used_count"`
	ExpiresAt      string `json:"expires_at"`
}

// EnrollmentKeyView is the public projection of an enrollment key.
type EnrollmentKeyView struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
	KeyCode     string `json:"key_code,omitempty"`
	Reusable    bool   `json:"reusable"`
	PreApproved bool   `json:"pre_approved"`
	UsedCount   int64  `json:"used_count"`
	ExpiresAt   string `json:"expires_at,omitempty"`
}

// EnrollmentOptions describes how this machine can enroll into a workspace.
type EnrollmentOptions struct {
	WorkspaceID           string              `json:"workspace_id"`
	MachineID             string              `json:"machine_id"`
	LocalConnection       bool                `json:"local_connection"`
	ExistingDevice        bool                `json:"existing_device"`
	CurrentKeyUnavailable bool                `json:"current_key_unavailable"`
	CurrentKey            *EnrollmentKeyView  `json:"current_key"`
	ReusableKeys          []EnrollmentKeyView `json:"reusable_keys"`
}

// Node is the whitelisted projection of one network node.
type Node struct {
	ID                  string `json:"id"`
	MachineID           string `json:"machine_id"`
	Hostname            string `json:"hostname,omitempty"`
	IPv4Addr            string `json:"ipv4_addr,omitempty"`
	DNSName             string `json:"dns_name,omitempty"`
	OS                  string `json:"os,omitempty"`
	OSVersion           string `json:"os_version,omitempty"`
	OSDistribution      string `json:"os_distribution,omitempty"`
	Version             string `json:"version,omitempty"`
	Status              string `json:"status,omitempty"`
	LifecycleState      string `json:"lifecycle_state,omitempty"`
	ProvisioningState   string `json:"provisioning_state,omitempty"`
	RuntimeHealthStatus string `json:"runtime_health_status,omitempty"`
	LastSeen            string `json:"last_seen,omitempty"`
	LastRuntimeCheckAt  string `json:"last_runtime_check_at,omitempty"`
	ConfigApplyStatus   string `json:"config_apply_status,omitempty"`
	ConfigDriftStatus   string `json:"config_drift_status,omitempty"`
	DisplayName         string `json:"display_name,omitempty"`
	ConnectivityState   string `json:"connectivity_state,omitempty"`
	IsExitNode          bool   `json:"is_exit_node"`
	IsSubnetRouter      bool   `json:"is_subnet_router"`
}

// rawNode mirrors the Console node payload before projection.
type rawNode struct {
	Node
	Device struct {
		DisplayName       string `json:"display_name"`
		ConnectivityState string `json:"connectivity_state"`
	} `json:"device"`
}

// activatePlan is what the Console part of a workspace activation produced.
type activatePlan struct {
	BootstrapToken string
	ConfigServer   string
	WorkspaceID    string
	MachineID      string
	ExistingDevice bool
}
