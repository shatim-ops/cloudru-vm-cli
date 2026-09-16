package errors

// Error code constants defined in ADR-007.
const (
	CodeAuthRequired      = "AUTH_REQUIRED"
	CodeAuthExpired       = "AUTH_EXPIRED"
	CodeAPIUnreachable    = "API_UNREACHABLE"
	CodeAPIRateLimited    = "API_RATE_LIMITED"
	CodeAPIError          = "API_ERROR"
	CodeVMNotFound        = "VM_NOT_FOUND"
	CodeVMProvisionFailed = "VM_PROVISION_FAILED"
	CodeSSHTimeout        = "SSH_TIMEOUT"
	CodeSSHAuthFailed     = "SSH_AUTH_FAILED"
	// CodeSSHHandshakeFailed: the host accepted the TCP connection but never
	// completed the SSH protocol. Not an auth failure — usually sshd is not up
	// yet (a VM still booting) or something else answers on the port.
	CodeSSHHandshakeFailed = "SSH_HANDSHAKE_FAILED"
	CodeSSHHostKeyChanged  = "SSH_HOST_KEY_CHANGED"
	CodeComposeInvalid     = "COMPOSE_INVALID"
	CodeComposeNotFound    = "COMPOSE_NOT_FOUND"
	CodeConfigInvalid      = "CONFIG_INVALID"
	CodeStateCorrupted     = "STATE_CORRUPTED"
)
