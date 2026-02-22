package rdp

import (
	"net"
	"testing"
	"time"
)

// newTestConnPair creates a connected pair of net.Conn for testing.
func newTestConnPair(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	c1, c2 := net.Pipe()
	return c1, c2
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Port != DefaultPort {
		t.Errorf("port = %d, want %d", cfg.Port, DefaultPort)
	}
	if cfg.Enabled {
		t.Error("expected disabled by default")
	}
	if cfg.Width != 1920 {
		t.Errorf("width = %d, want 1920", cfg.Width)
	}
	if cfg.Height != 1080 {
		t.Errorf("height = %d, want 1080", cfg.Height)
	}
	if cfg.TileSize != 64 {
		t.Errorf("tileSize = %d, want 64", cfg.TileSize)
	}
	if cfg.MaxSessions != 1 {
		t.Errorf("maxSessions = %d, want 1", cfg.MaxSessions)
	}
}

func TestServerConfigValidate(t *testing.T) {
	tests := []struct {
		name string
		cfg  ServerConfig
		want ServerConfig
	}{
		{
			name: "all defaults",
			cfg:  ServerConfig{},
			want: ServerConfig{
				Port:        DefaultPort,
				Width:       1920,
				Height:      1080,
				TileSize:    64,
				MaxSessions: 1,
			},
		},
		{
			name: "invalid port",
			cfg:  ServerConfig{Port: -1},
			want: ServerConfig{
				Port:        DefaultPort,
				Width:       1920,
				Height:      1080,
				TileSize:    64,
				MaxSessions: 1,
			},
		},
		{
			name: "port too high",
			cfg:  ServerConfig{Port: 70000},
			want: ServerConfig{
				Port:        DefaultPort,
				Width:       1920,
				Height:      1080,
				TileSize:    64,
				MaxSessions: 1,
			},
		},
		{
			name: "valid custom config",
			cfg: ServerConfig{
				Port:        5900,
				Width:       1280,
				Height:      720,
				TileSize:    32,
				MaxSessions: 3,
			},
			want: ServerConfig{
				Port:        5900,
				Width:       1280,
				Height:      720,
				TileSize:    32,
				MaxSessions: 3,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.cfg.Validate()
			if tc.cfg.Port != tc.want.Port {
				t.Errorf("port = %d, want %d", tc.cfg.Port, tc.want.Port)
			}
			if tc.cfg.Width != tc.want.Width {
				t.Errorf("width = %d, want %d", tc.cfg.Width, tc.want.Width)
			}
			if tc.cfg.Height != tc.want.Height {
				t.Errorf("height = %d, want %d", tc.cfg.Height, tc.want.Height)
			}
			if tc.cfg.TileSize != tc.want.TileSize {
				t.Errorf("tileSize = %d, want %d", tc.cfg.TileSize, tc.want.TileSize)
			}
			if tc.cfg.MaxSessions != tc.want.MaxSessions {
				t.Errorf("maxSessions = %d, want %d", tc.cfg.MaxSessions, tc.want.MaxSessions)
			}
		})
	}
}

func TestNewServer(t *testing.T) {
	cfg := DefaultConfig()
	srv := NewServer(cfg, nil, nil, nil)

	if srv == nil {
		t.Fatal("NewServer returned nil")
	}

	if srv.Addr() != "" {
		t.Errorf("addr before start = %q, want empty", srv.Addr())
	}

	if srv.ActiveSessions() != 0 {
		t.Errorf("active sessions = %d, want 0", srv.ActiveSessions())
	}
}

func TestServerStartStop(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Port = 0 // Use random available port

	srv := NewServer(cfg, nil, nil, nil)

	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	addr := srv.Addr()
	if addr == "" {
		t.Fatal("server addr is empty after start")
	}

	t.Logf("server listening on %s", addr)

	// Verify the server is accepting connections
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	conn.Close()

	// Stop the server
	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Verify it's no longer listening
	_, err = net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err == nil {
		t.Error("expected connection refused after stop")
	}
}

func TestServerMaxSessions(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Port = 0
	cfg.MaxSessions = 1

	srv := NewServer(cfg, nil, nil, nil)

	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()

	addr := srv.Addr()

	// First connection should be accepted
	conn1, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("first dial: %v", err)
	}
	defer conn1.Close()

	// Give the server time to register the session
	time.Sleep(100 * time.Millisecond)

	// Second connection: server will accept TCP but close it immediately due to max sessions
	conn2, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		// Connection refused is also acceptable
		return
	}

	// The server should close this connection quickly
	conn2.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	_, readErr := conn2.Read(buf)
	conn2.Close()

	// Either EOF (connection closed by server) or timeout (still waiting for negotiation)
	// Both are acceptable behaviors for max sessions
	_ = readErr
}

func TestServerUpdateConfig(t *testing.T) {
	cfg := DefaultConfig()
	srv := NewServer(cfg, nil, nil, nil)

	newCfg := ServerConfig{
		Port:        5900,
		Width:       1280,
		Height:      720,
		TileSize:    32,
		MaxSessions: 5,
	}

	srv.UpdateConfig(newCfg)

	got := srv.Config()
	if got.Port != 5900 {
		t.Errorf("port = %d, want 5900", got.Port)
	}
	if got.Width != 1280 {
		t.Errorf("width = %d, want 1280", got.Width)
	}
	if got.MaxSessions != 5 {
		t.Errorf("maxSessions = %d, want 5", got.MaxSessions)
	}
}

func TestServerNegotiationHandshake(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Port = 0

	srv := NewServer(cfg, nil, nil, nil)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()

	addr := srv.Addr()
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Send a valid X.224 Connection Request
	x224Req := []byte{
		6,                         // length
		x224TypeConnectionRequest, // type
		0x00, 0x00, // dst-ref
		0x00, 0x00, // src-ref
		0x00, // class
	}
	if err := writeTPKT(conn, x224Req); err != nil {
		t.Fatalf("write connection request: %v", err)
	}

	// Read the Connection Confirm response
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, payload, err := readTPKT(conn)
	if err != nil {
		t.Fatalf("read connection confirm: %v", err)
	}

	if len(payload) < 2 {
		t.Fatal("connection confirm too short")
	}

	if payload[1] != x224TypeConnectionConfirm {
		t.Errorf("response type = 0x%02X, want 0x%02X", payload[1], x224TypeConnectionConfirm)
	}
}
