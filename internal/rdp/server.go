package rdp

import (
	"context"
	"fmt"
	"net"
	"sync"
)

// ServerConfig holds configuration for the RDP server.
type ServerConfig struct {
	// Port is the TCP port to listen on (default: 3389).
	Port int `json:"port"`

	// Enabled controls whether the RDP server is running.
	Enabled bool `json:"enabled"`

	// Width is the desktop width in pixels (default: 1920).
	Width uint16 `json:"width"`

	// Height is the desktop height in pixels (default: 1080).
	Height uint16 `json:"height"`

	// TileSize is the bitmap tile size for updates (default: 64).
	TileSize int `json:"tile_size"`

	// MaxSessions limits concurrent RDP sessions (default: 1).
	MaxSessions int `json:"max_sessions"`
}

// DefaultConfig returns a ServerConfig with default values.
func DefaultConfig() ServerConfig {
	return ServerConfig{
		Port:        DefaultPort,
		Enabled:     false,
		Width:       1920,
		Height:      1080,
		TileSize:    64,
		MaxSessions: 1,
	}
}

// Validate checks the configuration and applies defaults for invalid values.
func (c *ServerConfig) Validate() {
	if c.Port <= 0 || c.Port > 65535 {
		c.Port = DefaultPort
	}
	if c.Width == 0 {
		c.Width = 1920
	}
	if c.Height == 0 {
		c.Height = 1080
	}
	if c.TileSize <= 0 {
		c.TileSize = 64
	}
	if c.MaxSessions <= 0 {
		c.MaxSessions = 1
	}
}

// Server is the main RDP server that listens for connections.
type Server struct {
	config   ServerConfig
	listener net.Listener

	inputHandler  InputHandler
	frameProvider FrameProvider
	log           Logger

	sessions sync.Map // map[string]*Session
	mu       sync.Mutex
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup

	sessionCount int
}

// NewServer creates a new RDP server with the given configuration.
func NewServer(cfg ServerConfig, inputHandler InputHandler, frameProvider FrameProvider, log Logger) *Server {
	cfg.Validate()
	if log == nil {
		log = defaultLogger{}
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Server{
		config:        cfg,
		inputHandler:  inputHandler,
		frameProvider: frameProvider,
		log:           log,
		ctx:           ctx,
		cancel:        cancel,
	}
}

// Start begins listening for RDP connections.
func (s *Server) Start() error {
	addr := fmt.Sprintf(":%d", s.config.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}

	s.mu.Lock()
	s.listener = listener
	s.mu.Unlock()

	s.log.Info("RDP server listening on %s", addr)

	s.wg.Add(1)
	go s.acceptLoop()

	return nil
}

// Stop gracefully shuts down the RDP server.
func (s *Server) Stop() error {
	s.cancel()

	s.mu.Lock()
	listener := s.listener
	s.listener = nil
	s.mu.Unlock()

	if listener != nil {
		if err := listener.Close(); err != nil {
			s.log.Warn("error closing listener: %s", err)
		}
	}

	// Close all active sessions
	s.sessions.Range(func(key, value any) bool {
		if sess, ok := value.(*Session); ok {
			sess.Close()
		}
		return true
	})

	s.wg.Wait()
	s.log.Info("RDP server stopped")
	return nil
}

// ActiveSessions returns the number of active sessions.
func (s *Server) ActiveSessions() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionCount
}

// Addr returns the listener address, or empty if not listening.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return ""
}

// acceptLoop accepts new TCP connections and creates sessions.
func (s *Server) acceptLoop() {
	defer s.wg.Done()

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				return
			default:
				s.log.Warn("accept error: %s", err)
				continue
			}
		}

		s.mu.Lock()
		if s.sessionCount >= s.config.MaxSessions {
			s.mu.Unlock()
			s.log.Warn("max sessions reached, rejecting %s", conn.RemoteAddr())
			conn.Close()
			continue
		}
		s.sessionCount++
		s.mu.Unlock()

		s.wg.Add(1)
		go s.handleConnection(conn)
	}
}

// handleConnection manages a single RDP session.
func (s *Server) handleConnection(conn net.Conn) {
	defer s.wg.Done()
	defer func() {
		s.mu.Lock()
		s.sessionCount--
		s.mu.Unlock()
	}()

	remoteAddr := conn.RemoteAddr().String()
	s.log.Info("new connection from %s", remoteAddr)

	sess := newSession(conn, SessionConfig{
		Width:         s.config.Width,
		Height:        s.config.Height,
		TileSize:      s.config.TileSize,
		InputHandler:  s.inputHandler,
		FrameProvider: s.frameProvider,
		Log:           s.log,
	})

	s.sessions.Store(remoteAddr, sess)
	defer s.sessions.Delete(remoteAddr)

	// Run session with context cancellation
	done := make(chan error, 1)
	go func() {
		done <- sess.Run()
	}()

	select {
	case err := <-done:
		if err != nil {
			s.log.Warn("session %s ended with error: %s", remoteAddr, err)
		} else {
			s.log.Info("session %s ended normally", remoteAddr)
		}
	case <-s.ctx.Done():
		sess.Close()
		<-done
	}
}

// UpdateConfig updates the server configuration dynamically.
func (s *Server) UpdateConfig(cfg ServerConfig) {
	cfg.Validate()
	s.mu.Lock()
	s.config = cfg
	s.mu.Unlock()
}

// Config returns the current server configuration.
func (s *Server) Config() ServerConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.config
}
