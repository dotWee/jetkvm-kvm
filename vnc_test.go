package kvm

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestRPCGetVNCState_DefaultConfig verifies that rpcGetVNCState reflects the
// current configuration when no server is running.
func TestRPCGetVNCState_DefaultConfig(t *testing.T) {
	ensureConfigLoaded()

	// Reset VNC-related config to known values.
	config.VNCEnabled = false
	config.VNCPort = 5900
	config.VNCPassword = ""

	vncServer = nil
	appCtx = context.Background()

	state, err := rpcGetVNCState()
	if err != nil {
		t.Fatalf("rpcGetVNCState returned error: %v", err)
	}

	if state.Enabled {
		t.Fatalf("expected Enabled=false, got true")
	}
	if state.Port != 5900 {
		t.Fatalf("expected Port=5900, got %d", state.Port)
	}
	if state.HasPassword {
		t.Fatalf("expected HasPassword=false, got true")
	}
	if state.ConnectedClients != 0 {
		t.Fatalf("expected ConnectedClients=0, got %d", state.ConnectedClients)
	}
}

// TestRPCSetVNCPortValidation ensures invalid port values are rejected.
func TestRPCSetVNCPortValidation(t *testing.T) {
	ensureConfigLoaded()

	tests := []int{0, -1, 70000}
	for _, port := range tests {
		err := rpcSetVNCPort(port)
		if err == nil {
			t.Fatalf("expected error for invalid port %d, got nil", port)
		}
	}
}

// TestHandleVNCSettings_Success verifies that handleVNCSettings applies the
// provided settings and returns the updated state.
func TestHandleVNCSettings_Success(t *testing.T) {
	ensureConfigLoaded()

	// Start from a known baseline.
	config.VNCEnabled = false
	config.VNCPort = 5900
	config.VNCPassword = ""
	appCtx = context.Background()

	// Redirect config writes to a temp file.
	tmpDir := t.TempDir()
	configPath = filepath.Join(tmpDir, "kvm_config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatalf("failed to create temp config dir: %v", err)
	}

	payload := map[string]any{
		"enabled":  true,
		"port":     5959,
		"password": "secret",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/vnc/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	// Use a bare gin context so we can exercise the handler directly.
	c, _ := ginCreateTestContext(w, req)
	handleVNCSettings(c)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var state VNCState
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if !state.Enabled {
		t.Fatalf("expected Enabled=true, got false")
	}
	if state.Port != 5959 {
		t.Fatalf("expected Port=5959, got %d", state.Port)
	}
	if !state.HasPassword {
		t.Fatalf("expected HasPassword=true, got false")
	}
}

// TestHandleVNCSettings_InvalidBody verifies that invalid JSON produces a 400.
func TestHandleVNCSettings_InvalidBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPut, "/vnc/settings", bytes.NewReader([]byte("{invalid")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	c, _ := ginCreateTestContext(w, req)
	handleVNCSettings(c)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Result().StatusCode)
	}
}

// TestRPCSetVNCEnabled_StartStop verifies that enabling starts a server instance
// (if not already running) and disabling stops it.
func TestRPCSetVNCEnabled_StartStop(t *testing.T) {
	ensureConfigLoaded()
	appCtx = context.Background()

	tmpDir := t.TempDir()
	configPath = filepath.Join(tmpDir, "kvm_config.json")

	// Keep VNC safely bound.
	config.LocalLoopbackOnly = true
	config.VNCPort = 0
	config.VNCPassword = ""

	// Ensure clean state.
	stopVNC()
	config.VNCEnabled = false

	if err := rpcSetVNCEnabled(true); err != nil {
		t.Fatalf("expected enable to succeed, got error: %v", err)
	}
	if vncServer == nil {
		t.Fatalf("expected server to be started")
	}

	if err := rpcSetVNCEnabled(false); err != nil {
		t.Fatalf("expected disable to succeed, got error: %v", err)
	}
	if vncServer != nil {
		t.Fatalf("expected server to be stopped")
	}
}

// TestRPCSetVNCPassword_RestartOnChange ensures password updates trigger restart when enabled.
func TestRPCSetVNCPassword_RestartOnChange(t *testing.T) {
	ensureConfigLoaded()
	appCtx = context.Background()

	tmpDir := t.TempDir()
	configPath = filepath.Join(tmpDir, "kvm_config.json")

	config.LocalLoopbackOnly = true
	config.VNCPort = 0

	stopVNC()
	config.VNCEnabled = false
	if err := rpcSetVNCEnabled(true); err != nil {
		t.Fatalf("enable failed: %v", err)
	}
	first := vncServer
	if first == nil {
		t.Fatalf("expected server started")
	}

	if err := rpcSetVNCPassword("abc"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	if vncServer == nil {
		t.Fatalf("expected server still running")
	}
	if vncServer == first {
		t.Fatalf("expected server to be restarted on password change")
	}
}

// TestRPCSetLocalLoopbackOnly_RestartVNC ensures toggling loopback-only restarts VNC when enabled.
func TestRPCSetLocalLoopbackOnly_RestartVNC(t *testing.T) {
	ensureConfigLoaded()
	appCtx = context.Background()

	tmpDir := t.TempDir()
	configPath = filepath.Join(tmpDir, "kvm_config.json")

	config.VNCPort = 0
	config.VNCPassword = ""

	stopVNC()
	config.VNCEnabled = false
	config.LocalLoopbackOnly = false
	if err := rpcSetVNCEnabled(true); err != nil {
		t.Fatalf("enable failed: %v", err)
	}
	first := vncServer
	if first == nil {
		t.Fatalf("expected server started")
	}

	if err := rpcSetLocalLoopbackOnly(true); err != nil {
		t.Fatalf("rpcSetLocalLoopbackOnly failed: %v", err)
	}
	if vncServer == nil {
		t.Fatalf("expected server still running")
	}
	if vncServer == first {
		t.Fatalf("expected server to be restarted on loopback-only change")
	}
}
