package rdp

import "golang.org/x/crypto/bcrypt"

const (
	LocalAuthModePassword   = "password"
	LocalAuthModeNoPassword = "noPassword"
)

// AuthConfigProvider exposes local auth settings from the host application.
type AuthConfigProvider interface {
	LocalAuthMode() string
	HashedPassword() string
}

// StaticAuthConfig is a helper implementation for tests or static runtime config.
type StaticAuthConfig struct {
	Mode         string
	PasswordHash string
}

func (s StaticAuthConfig) LocalAuthMode() string {
	return s.Mode
}

func (s StaticAuthConfig) HashedPassword() string {
	return s.PasswordHash
}

// Authenticator validates RDP credentials against the device local auth config.
type Authenticator struct {
	provider AuthConfigProvider
	metrics  Metrics
}

func NewAuthenticator(provider AuthConfigProvider, metrics Metrics) *Authenticator {
	if metrics == nil {
		metrics = noopMetrics{}
	}
	return &Authenticator{provider: provider, metrics: metrics}
}

// ValidatePassword validates a plaintext password using the current local auth mode.
// In noPassword mode, this always succeeds.
func (a *Authenticator) ValidatePassword(password string) bool {
	if a == nil || a.provider == nil {
		return false
	}

	switch a.provider.LocalAuthMode() {
	case LocalAuthModeNoPassword:
		return true
	case LocalAuthModePassword:
		if !ValidatePasswordHash(a.provider.HashedPassword(), password) {
			a.metrics.IncAuthFailures()
			return false
		}
		return true
	default:
		a.metrics.IncAuthFailures()
		return false
	}
}

// ValidatePasswordHash compares a bcrypt hash and plaintext password.
func ValidatePasswordHash(hash string, password string) bool {
	if hash == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
