// Package apperr carries the API error contract shared by the local backend:
// every failure is a stable code plus a user-facing Chinese message.
//
// The codes mirror the ones the EasyTier Pro OpenWrt client returns so both
// clients share one vocabulary.
package apperr

const (
	CodeInvalidRequest              = "invalid_request"
	CodeUnknownError                = "unknown_error"
	CodeStateUnavailable            = "state_unavailable"
	CodeMethodNotFound              = "method_not_found"
	CodeServiceNotRunning           = "service_not_running"
	CodeServiceStartFailed          = "service_start_failed"
	CodeServiceRestartFailed        = "service_restart_failed"
	CodeNoBootstrapToken            = "no_bootstrap_token"
	CodeInvalidBootstrapToken       = "invalid_bootstrap_token"
	CodeInvalidConfigServer         = "invalid_config_server"
	CodeInvalidConsoleURL           = "invalid_console_url"
	CodeInvalidInstallDir           = "invalid_install_dir"
	CodeInvalidAction               = "invalid_action"
	CodeCoreNotInstalled            = "core_not_installed"
	CodeConsoleUnreachable          = "console_unreachable"
	CodeNotAuthenticated            = "not_authenticated"
	CodeDeviceAuthFailed            = "device_auth_failed"
	CodeNoDeviceAuth                = "no_device_auth"
	CodeInvalidAuthState            = "invalid_auth_state"
	CodeExpiredToken                = "expired_token"
	CodeAccessDenied                = "access_denied"
	CodeAccountLookupFailed         = "account_lookup_failed"
	CodeInvalidConsoleResponse      = "invalid_console_response"
	CodeReleaseLookupFailed         = "release_lookup_failed"
	CodeInvalidWorkspace            = "invalid_workspace"
	CodeWorkspaceAccessDenied       = "workspace_access_denied"
	CodeInvalidEnrollmentMode       = "invalid_enrollment_mode"
	CodeEnrollmentChoiceRequired    = "enrollment_choice_required"
	CodeEnrollmentFailed            = "enrollment_failed"
	CodeEnrollmentKeyUnavailable    = "enrollment_key_unavailable"
	CodeEnrollmentLookupFailed      = "enrollment_lookup_failed"
	CodeRouterNotEnrolled           = "router_not_enrolled"
	CodeInvalidNetwork              = "invalid_network"
	CodeNetworkJoinFailed           = "network_join_failed"
	CodeNetworkLeaveFailed          = "network_leave_failed"
	CodeNetworkLookupFailed         = "network_lookup_failed"
	CodeNodeLookupFailed            = "node_lookup_failed"
	CodeNoWorkspace                 = "no_workspace"
	CodeDownloadBusy                = "download_busy"
	CodeConnectionChangeBusy        = "connection_change_busy"
	CodeInvalidConnectionOperation  = "invalid_connection_operation"
	CodeConnectionChangeNotFound    = "connection_change_not_found"
	CodeConnectionChangeStartFailed = "connection_change_start_failed"
	CodeConnectionChangeInterrupted = "connection_change_interrupted"
	CodeDSMAuthRequired             = "dsm_auth_required"
	CodeDSMAuthForbidden            = "dsm_auth_forbidden"
)

var messages = map[string]string{
	CodeInvalidRequest:              "请求无效。",
	CodeUnknownError:                "操作失败。",
	CodeStateUnavailable:            "本机状态存储不可用。",
	CodeMethodNotFound:              "不支持的操作。",
	CodeServiceNotRunning:           "EasyTier Pro 未运行。",
	CodeServiceStartFailed:          "EasyTier Pro 启动失败。",
	CodeServiceRestartFailed:        "EasyTier Pro 重启失败。",
	CodeNoBootstrapToken:            "请先连接本机。",
	CodeInvalidBootstrapToken:       "请输入有效的设备注册令牌。",
	CodeInvalidConfigServer:         "配置服务器地址无效。",
	CodeInvalidConsoleURL:           "Console 地址无效。",
	CodeInvalidInstallDir:           "运行目录无效。",
	CodeInvalidAction:               "服务操作无效。",
	CodeCoreNotInstalled:            "请先安装 EasyTier 运行时。",
	CodeConsoleUnreachable:          "无法连接 EasyTier Console。",
	CodeNotAuthenticated:            "请先登录 EasyTier Console。",
	CodeDeviceAuthFailed:            "设备授权失败。",
	CodeNoDeviceAuth:                "请先开始 Console 登录。",
	CodeInvalidAuthState:            "登录状态无效，请重新登录。",
	CodeExpiredToken:                "设备码已过期。",
	CodeAccessDenied:                "Console 拒绝了本次操作。",
	CodeAccountLookupFailed:         "已登录，但无法读取账号信息。",
	CodeInvalidConsoleResponse:      "EasyTier Console 返回了无效响应。",
	CodeReleaseLookupFailed:         "无法读取 EasyTier 稳定版本。",
	CodeInvalidWorkspace:            "请选择有效的 Console 工作空间。",
	CodeWorkspaceAccessDenied:       "当前账号无法使用所选工作空间。",
	CodeInvalidEnrollmentMode:       "请选择注册密钥的使用方式。",
	CodeEnrollmentChoiceRequired:    "请选择注册密钥。",
	CodeEnrollmentFailed:            "无法创建设备注册密钥。",
	CodeEnrollmentKeyUnavailable:    "所选注册密钥已不可用。",
	CodeEnrollmentLookupFailed:      "无法从 EasyTier Console 读取注册密钥。",
	CodeRouterNotEnrolled:           "请先启动 EasyTier Pro 并等待本机注册。",
	CodeInvalidNetwork:              "请选择有效的网络。",
	CodeNetworkJoinFailed:           "Console 无法把本机加入该网络。",
	CodeNetworkLeaveFailed:          "Console 无法把本机移出该网络。",
	CodeNetworkLookupFailed:         "无法读取工作空间网络。",
	CodeNodeLookupFailed:            "无法读取网络节点。",
	CodeNoWorkspace:                 "请先选择 Console 工作空间。",
	CodeDownloadBusy:                "已有更新在进行中。",
	CodeConnectionChangeBusy:        "已有本机设置变更在进行中。",
	CodeInvalidConnectionOperation:  "该操作编号无效。",
	CodeConnectionChangeNotFound:    "该操作记录已不存在。",
	CodeConnectionChangeStartFailed: "无法在后台启动本机设置变更。",
	CodeConnectionChangeInterrupted: "上次设置变更被中断，请重试。",
	CodeDSMAuthRequired:             "请先登录 DSM。",
	CodeDSMAuthForbidden:            "只有 DSM 管理员可以使用该功能。",
}

// Error is an API-level failure.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string {
	return e.Code + ": " + e.Message
}

// New builds an Error from a code using the canonical message.
func New(code string) *Error {
	message, ok := messages[code]
	if !ok {
		code = CodeUnknownError
		message = messages[code]
	}
	return &Error{Code: code, Message: message}
}

// WithMessage builds an Error from a code and an explicit message.
func WithMessage(code, message string) *Error {
	if message == "" {
		return New(code)
	}
	return &Error{Code: code, Message: message}
}
