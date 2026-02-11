package rdp

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

type metricsSpy struct {
	connections      int
	authFailures     int
	sessionTakeovers int
	activeSessions   int
}

func (m *metricsSpy) IncConnections()      { m.connections++ }
func (m *metricsSpy) IncAuthFailures()     { m.authFailures++ }
func (m *metricsSpy) IncSessionTakeovers() { m.sessionTakeovers++ }
func (m *metricsSpy) SetActiveSessions(v int) {
	m.activeSessions = v
}

func TestAuthenticatorPasswordMode(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("secret123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	metrics := &metricsSpy{}
	a := NewAuthenticator(StaticAuthConfig{Mode: LocalAuthModePassword, PasswordHash: string(hash)}, metrics)

	if !a.ValidatePassword("secret123") {
		t.Fatalf("expected valid password to be accepted")
	}

	if a.ValidatePassword("bad-password") {
		t.Fatalf("expected invalid password to be rejected")
	}
	if metrics.authFailures != 1 {
		t.Fatalf("expected auth failure metric incremented once, got %d", metrics.authFailures)
	}
}

func TestAuthenticatorNoPasswordMode(t *testing.T) {
	metrics := &metricsSpy{}
	a := NewAuthenticator(StaticAuthConfig{Mode: LocalAuthModeNoPassword}, metrics)

	if !a.ValidatePassword("") {
		t.Fatalf("expected noPassword mode to accept empty password")
	}
	if !a.ValidatePassword("anything") {
		t.Fatalf("expected noPassword mode to accept any password")
	}
	if metrics.authFailures != 0 {
		t.Fatalf("expected no auth failures in noPassword mode, got %d", metrics.authFailures)
	}
}
