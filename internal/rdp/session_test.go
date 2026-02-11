package rdp

import (
	"image"
	"image/color"
	"testing"
)

func TestSessionStateString(t *testing.T) {
	tests := []struct {
		state SessionState
		want  string
	}{
		{StateNegotiation, "negotiation"},
		{StateMCSConnect, "mcs_connect"},
		{StateMCSSetup, "mcs_setup"},
		{StateCapabilityExchange, "capability_exchange"},
		{StateActive, "active"},
		{StateClosed, "closed"},
		{SessionState(99), "unknown"},
	}

	for _, tc := range tests {
		got := tc.state.String()
		if got != tc.want {
			t.Errorf("SessionState(%d).String() = %q, want %q", tc.state, got, tc.want)
		}
	}
}

func TestNewSession(t *testing.T) {
	client, server := newTestConnPair(t)
	defer client.Close()
	defer server.Close()

	cfg := SessionConfig{
		Width:    1920,
		Height:   1080,
		TileSize: 64,
	}

	sess := newSession(server, cfg)
	if sess == nil {
		t.Fatal("newSession returned nil")
	}

	if sess.State() != StateNegotiation {
		t.Errorf("initial state = %v, want %v", sess.State(), StateNegotiation)
	}

	if sess.width != 1920 || sess.height != 1080 {
		t.Errorf("dimensions = %dx%d, want 1920x1080", sess.width, sess.height)
	}

	if sess.tileSize != 64 {
		t.Errorf("tileSize = %d, want 64", sess.tileSize)
	}

	if sess.RemoteAddr() == "" {
		t.Error("RemoteAddr() is empty")
	}
}

func TestNewSessionDefaults(t *testing.T) {
	client, server := newTestConnPair(t)
	defer client.Close()
	defer server.Close()

	sess := newSession(server, SessionConfig{})

	if sess.tileSize != 64 {
		t.Errorf("default tileSize = %d, want 64", sess.tileSize)
	}
	if sess.frameRate != 30 {
		t.Errorf("default frameRate = %d, want 30", sess.frameRate)
	}
}

func TestSessionClose(t *testing.T) {
	client, server := newTestConnPair(t)
	defer client.Close()

	sess := newSession(server, SessionConfig{})

	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if sess.State() != StateClosed {
		t.Errorf("state after close = %v, want %v", sess.State(), StateClosed)
	}

	// Double close should be OK
	if err := sess.Close(); err != nil {
		t.Errorf("double Close: %v", err)
	}
}

func TestBuildMCSConnectResponse(t *testing.T) {
	client, server := newTestConnPair(t)
	defer client.Close()
	defer server.Close()

	sess := newSession(server, SessionConfig{Width: 1920, Height: 1080})
	data := sess.buildMCSConnectResponse()

	if len(data) == 0 {
		t.Fatal("empty MCS Connect Response")
	}

	// Check BER tag
	if data[0] != 0x7F || data[1] != 0x66 {
		t.Errorf("MCS response tag = [0x%02X, 0x%02X], want [0x7F, 0x66]", data[0], data[1])
	}
}

func TestBuildAttachUserConfirm(t *testing.T) {
	client, server := newTestConnPair(t)
	defer client.Close()
	defer server.Close()

	sess := newSession(server, SessionConfig{})
	sess.userID = 1001
	data := sess.buildAttachUserConfirm()

	if len(data) < 4 {
		t.Fatalf("attach user confirm too short: %d bytes", len(data))
	}

	tag := data[0]
	expectedTag := byte(mcsTypeAttachUserConf << 2)
	if tag != expectedTag {
		t.Errorf("tag = 0x%02X, want 0x%02X", tag, expectedTag)
	}

	result := data[1]
	if result != 0 {
		t.Errorf("result = %d, want 0 (success)", result)
	}
}

func TestBuildChannelJoinConfirm(t *testing.T) {
	client, server := newTestConnPair(t)
	defer client.Close()
	defer server.Close()

	sess := newSession(server, SessionConfig{})
	sess.userID = 1001
	data := sess.buildChannelJoinConfirm(0x03EB)

	if len(data) < 8 {
		t.Fatalf("channel join confirm too short: %d bytes", len(data))
	}

	tag := data[0]
	expectedTag := byte(mcsTypeChannelJoinConf << 2)
	if tag != expectedTag {
		t.Errorf("tag = 0x%02X, want 0x%02X", tag, expectedTag)
	}
}

func TestBuildDemandActivePDU(t *testing.T) {
	client, server := newTestConnPair(t)
	defer client.Close()
	defer server.Close()

	sess := newSession(server, SessionConfig{Width: 1920, Height: 1080})
	data := sess.buildDemandActivePDU()

	if len(data) == 0 {
		t.Fatal("empty Demand Active PDU")
	}

	// Verify PDU type
	pduType := getU16LE(data, 2)
	if pduType != pduTypeDemandActive {
		t.Errorf("pduType = 0x%04X, want 0x%04X", pduType, pduTypeDemandActive)
	}

	// Verify shareId
	shareID := getU32LE(data, 6)
	if shareID != sess.shareID {
		t.Errorf("shareID = 0x%08X, want 0x%08X", shareID, sess.shareID)
	}
}

func TestBuildCapabilitySets(t *testing.T) {
	client, server := newTestConnPair(t)
	defer client.Close()
	defer server.Close()

	sess := newSession(server, SessionConfig{Width: 1920, Height: 1080})
	caps := sess.buildCapabilitySets()

	if len(caps) == 0 {
		t.Fatal("empty capability sets")
	}

	// Should contain 5 capability sets: general, bitmap, order, pointer, input
	// Each has a 4-byte header (type + length), verify we can parse them
	offset := 0
	count := 0
	for offset < len(caps) {
		if offset+4 > len(caps) {
			break
		}
		capLen := int(getU16LE(caps, offset+2))
		if capLen < 4 || offset+capLen > len(caps) {
			t.Errorf("invalid capability set at offset %d, length %d", offset, capLen)
			break
		}
		offset += capLen
		count++
	}

	if count != 5 {
		t.Errorf("found %d capability sets, want 5", count)
	}
}

func TestAppendBERLength(t *testing.T) {
	tests := []struct {
		length int
		want   []byte
	}{
		{0, []byte{0}},
		{127, []byte{127}},
		{128, []byte{0x81, 128}},
		{255, []byte{0x81, 255}},
		{256, []byte{0x82, 1, 0}},
	}

	for _, tc := range tests {
		got := appendBERLength(nil, tc.length)
		if len(got) != len(tc.want) {
			t.Errorf("length %d: got %v, want %v", tc.length, got, tc.want)
			continue
		}
		for i, b := range got {
			if b != tc.want[i] {
				t.Errorf("length %d: byte %d = 0x%02X, want 0x%02X", tc.length, i, b, tc.want[i])
			}
		}
	}
}

// mockFrameProvider implements FrameProvider for testing.
type mockFrameProvider struct {
	frame  *image.RGBA
	width  uint16
	height uint16
}

func (m *mockFrameProvider) GetFrame() *image.RGBA {
	return m.frame
}

func (m *mockFrameProvider) GetResolution() (width, height uint16) {
	return m.width, m.height
}

func TestFrameProvider(t *testing.T) {
	img := NewTestImage(640, 480, color.RGBA{R: 255, A: 255})
	fp := &mockFrameProvider{frame: img, width: 640, height: 480}

	frame := fp.GetFrame()
	if frame == nil {
		t.Fatal("GetFrame() returned nil")
	}

	w, h := fp.GetResolution()
	if w != 640 || h != 480 {
		t.Errorf("resolution = %dx%d, want 640x480", w, h)
	}
}

func TestDefaultLogger(t *testing.T) {
	// Verify defaultLogger doesn't panic
	log := defaultLogger{}
	log.Debug("test %d", 1)
	log.Info("test %d", 2)
	log.Warn("test %d", 3)
	log.Error("test %d", 4)
}

func TestBuildGCCResponse(t *testing.T) {
	client, server := newTestConnPair(t)
	defer client.Close()
	defer server.Close()

	sess := newSession(server, SessionConfig{Width: 1920, Height: 1080})
	data := sess.buildGCCResponse()

	if len(data) == 0 {
		t.Fatal("empty GCC response")
	}
}

func TestIsSecurityOrInfoPDU(t *testing.T) {
	client, server := newTestConnPair(t)
	defer client.Close()
	defer server.Close()

	sess := newSession(server, SessionConfig{})

	// Short data should return false
	if sess.isSecurityOrInfoPDU([]byte{0x01}) {
		t.Error("expected false for short data")
	}

	// MCS Send Data Request pattern
	data := make([]byte, 8)
	data[0] = byte(mcsTypeSendDataRequest>>2) << 2
	if !sess.isSecurityOrInfoPDU(data) {
		t.Error("expected true for send data request")
	}
}

func TestProcessClientDataShort(t *testing.T) {
	client, server := newTestConnPair(t)
	defer client.Close()
	defer server.Close()

	sess := newSession(server, SessionConfig{})

	// Should not panic with short data
	sess.processClientData(nil)
	sess.processClientData([]byte{0x01})
	sess.processClientData([]byte{0x01, 0x02, 0x03})
}
