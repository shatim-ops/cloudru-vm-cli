package engine

import "time"

// Production waits used by Deploy. Engine.Timings overrides them per field.
const (
	vmPollInterval       = 5 * time.Second
	vmPollTimeout        = 5 * time.Minute
	ifacePollInterval    = 5 * time.Second
	fipRetryInterval     = 10 * time.Second
	fipPollInterval      = 5 * time.Second
	sshRetryInterval     = 5 * time.Second
	verifySettleDelay    = 2 * time.Second
	verifyTimeout        = 10 * time.Second
	ifaceMaxAttempts     = 24 // × ifacePollInterval ≈ 2 minutes
	ifaceFallbackAttempt = 12 // after this, accept a primary interface in any state
)

// Timings holds the waits Deploy uses between polls and retries. A zero field
// means the production default from the constants above; tests set
// milliseconds so that no test sleeps for real.
type Timings struct {
	VMPollInterval    time.Duration // GetVM polling until the VM is active
	VMPollTimeout     time.Duration // give up waiting for the VM to become active
	IfacePollInterval time.Duration // waiting for the primary interface to be "created"
	FIPRetryInterval  time.Duration // between CreateFloatingIP attempts
	FIPPollInterval   time.Duration // waiting for the floating IP to become in_use
	SSHRetryInterval  time.Duration // between SSH connect attempts
	SettleDelay       time.Duration // pause before health checks after compose up
	VerifyTimeout     time.Duration // per-port HTTP probe timeout
}

// withDefaults fills zero fields with the production defaults.
func (t Timings) withDefaults() Timings {
	def := func(v *time.Duration, d time.Duration) {
		if *v <= 0 {
			*v = d
		}
	}
	def(&t.VMPollInterval, vmPollInterval)
	def(&t.VMPollTimeout, vmPollTimeout)
	def(&t.IfacePollInterval, ifacePollInterval)
	def(&t.FIPRetryInterval, fipRetryInterval)
	def(&t.FIPPollInterval, fipPollInterval)
	def(&t.SSHRetryInterval, sshRetryInterval)
	def(&t.SettleDelay, verifySettleDelay)
	def(&t.VerifyTimeout, verifyTimeout)
	return t
}

func (e *Engine) timings() Timings {
	return e.Timings.withDefaults()
}
