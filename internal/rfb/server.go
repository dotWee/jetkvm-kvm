package rfb

import (
	"context"
	"fmt"
	"image"
	"io"
	"net"
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog"
)

// ServerConfig holds configuration for the VNC server.
type ServerConfig struct {
	// Addr is the TCP address to listen on (e.g., ":5900").
	Addr string

	// Name is the desktop name sent to clients.
	Name string

	// FrameProvider provides framebuffer data.
	FrameProvider FrameProvider

	// Input handles keyboard and mouse events from clients.
	Input InputHandler

	// SecurityHandlers lists the security types to offer.
	// If empty, only SecurityNone is offered.
	SecurityHandlers []SecurityHandler

	// Logger for the VNC server.
	Logger zerolog.Logger

	// OnClientConnected is called when a new client completes the handshake.
	OnClientConnected func()

	// OnClientDisconnected is called when a client disconnects.
	OnClientDisconnected func()

	// OnClientCutText is called when a client sends clipboard text (ClientCutText).
	// This can be used to implement clipboard sync or forward clipboard contents.
	OnClientCutText func(text string)
}

// Server is a VNC/RFB server that listens for TCP connections.
type Server struct {
	config   ServerConfig
	listener net.Listener
	clients  sync.Map // map[string]*ClientConn
	clientID atomic.Int64
	mu       sync.Mutex
}

// BroadcastCutText sends a ServerCutText message to all connected clients.
func (s *Server) BroadcastCutText(text string) {
	s.clients.Range(func(_, v any) bool {
		cc, ok := v.(*ClientConn)
		if !ok || cc == nil {
			return true
		}
		if err := cc.sendServerCutText(text); err != nil {
			cc.logger.Debug().Err(err).Msg("failed to broadcast clipboard to client")
		}
		return true
	})
}

// NewServer creates a new VNC server with the given configuration.
func NewServer(config ServerConfig) *Server {
	if len(config.SecurityHandlers) == 0 {
		config.SecurityHandlers = []SecurityHandler{&SecurityNone{}}
	}
	return &Server{
		config: config,
	}
}

// Listen starts the server and accepts connections until the context is cancelled.
func (s *Server) Listen(ctx context.Context) error {
	var err error
	s.listener, err = net.Listen("tcp", s.config.Addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", s.config.Addr, err)
	}

	s.config.Logger.Info().Str("addr", s.config.Addr).Msg("VNC server listening")

	// Close listener when context is cancelled
	go func() {
		<-ctx.Done()
		s.listener.Close()
	}()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil // clean shutdown
			default:
				s.config.Logger.Warn().Err(err).Msg("failed to accept connection")
				continue
			}
		}

		id := s.clientID.Add(1)
		clientLogger := s.config.Logger.With().Int64("client_id", id).Str("remote", conn.RemoteAddr().String()).Logger()

		cc := newClientConn(conn, id, s, clientLogger)
		s.clients.Store(id, cc)

		go func() {
			defer func() {
				s.clients.Delete(id)
				conn.Close()
			}()
			cc.serve(ctx)
		}()
	}
}

// Close shuts down the server and disconnects all clients.
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

// Addr returns the listener address, or empty string if not listening.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return ""
}

// ConnectedClients returns the number of connected VNC clients.
func (s *Server) ConnectedClients() int {
	count := 0
	s.clients.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

// ClientConn represents a single VNC client connection.
type ClientConn struct {
	conn   net.Conn
	id     int64
	server *Server
	logger zerolog.Logger

	pixelFormat PixelFormat
	encodings   []int32
	encoder     Encoder
	fbWidth     uint16
	fbHeight    uint16
	mu          sync.Mutex
	writeMu     sync.Mutex
}

func newClientConn(conn net.Conn, id int64, server *Server, logger zerolog.Logger) *ClientConn {
	return &ClientConn{
		conn:        conn,
		id:          id,
		server:      server,
		logger:      logger,
		pixelFormat: DefaultPixelFormat(),
		encoder:     &RawEncoder{},
	}
}

func (cc *ClientConn) serve(ctx context.Context) {
	cc.logger.Info().Msg("client connected")

	if err := cc.handshake(); err != nil {
		cc.logger.Warn().Err(err).Msg("handshake failed")
		return
	}

	cc.logger.Info().Msg("handshake completed")

	// Notify that client is fully connected
	if cc.server.config.OnClientConnected != nil {
		cc.server.config.OnClientConnected()
	}

	defer func() {
		cc.logger.Info().Msg("client disconnected")
		if cc.server.config.OnClientDisconnected != nil {
			cc.server.config.OnClientDisconnected()
		}
	}()

	cc.messageLoop(ctx)
}

func (cc *ClientConn) handshake() error {
	// 1. Protocol version handshake
	if err := cc.negotiateVersion(); err != nil {
		return fmt.Errorf("version negotiation failed: %w", err)
	}

	// 2. Security handshake
	if err := NegotiateSecurity(cc.conn, cc.server.config.SecurityHandlers); err != nil {
		return fmt.Errorf("security negotiation failed: %w", err)
	}

	// 3. ClientInit / ServerInit
	if err := cc.initExchange(); err != nil {
		return fmt.Errorf("init exchange failed: %w", err)
	}

	return nil
}

func (cc *ClientConn) negotiateVersion() error {
	// Send server protocol version
	if _, err := io.WriteString(cc.conn, ProtocolVersion); err != nil {
		return fmt.Errorf("failed to send version: %w", err)
	}

	// Read client protocol version
	buf := make([]byte, 12)
	if _, err := io.ReadFull(cc.conn, buf); err != nil {
		return fmt.Errorf("failed to read client version: %w", err)
	}

	clientVersion := string(buf)
	cc.logger.Debug().Str("client_version", clientVersion).Msg("client version received")

	// We accept any 3.x version
	if len(clientVersion) < 4 || clientVersion[:4] != "RFB " {
		return fmt.Errorf("invalid protocol version: %q", clientVersion)
	}

	return nil
}

func (cc *ClientConn) initExchange() error {
	// Read ClientInit (1 byte: shared-flag)
	sharedBuf := make([]byte, 1)
	if _, err := io.ReadFull(cc.conn, sharedBuf); err != nil {
		return fmt.Errorf("failed to read ClientInit: %w", err)
	}

	// Get framebuffer size
	width, height := cc.server.config.FrameProvider.GetSize()
	if width == 0 || height == 0 {
		width = 1920
		height = 1080
	}
	cc.fbWidth = width
	cc.fbHeight = height

	// Send ServerInit
	serverInit := ServerInit{
		Width:       width,
		Height:      height,
		PixelFormat: DefaultPixelFormat(),
		Name:        cc.server.config.Name,
	}

	if _, err := cc.conn.Write(serverInit.Marshal()); err != nil {
		return fmt.Errorf("failed to send ServerInit: %w", err)
	}

	return nil
}

func (cc *ClientConn) messageLoop(ctx context.Context) {
	typeBuf := make([]byte, 1)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if _, err := io.ReadFull(cc.conn, typeBuf); err != nil {
			if err != io.EOF {
				cc.logger.Debug().Err(err).Msg("failed to read message type")
			}
			return
		}

		switch typeBuf[0] {
		case MsgSetPixelFormat:
			msg, err := ReadSetPixelFormat(cc.conn)
			if err != nil {
				cc.logger.Warn().Err(err).Msg("failed to read SetPixelFormat")
				return
			}
			cc.handleSetPixelFormat(msg)

		case MsgSetEncodings:
			msg, err := ReadSetEncodings(cc.conn)
			if err != nil {
				cc.logger.Warn().Err(err).Msg("failed to read SetEncodings")
				return
			}
			cc.handleSetEncodings(msg)

		case MsgFramebufferUpdateRequest:
			msg, err := ReadFramebufferUpdateRequest(cc.conn)
			if err != nil {
				cc.logger.Warn().Err(err).Msg("failed to read FramebufferUpdateRequest")
				return
			}
			if err := cc.handleFramebufferUpdateRequest(msg); err != nil {
				cc.logger.Warn().Err(err).Msg("failed to handle FramebufferUpdateRequest")
				return
			}

		case MsgKeyEvent:
			msg, err := ReadKeyEvent(cc.conn)
			if err != nil {
				cc.logger.Warn().Err(err).Msg("failed to read KeyEvent")
				return
			}
			cc.handleKeyEvent(msg)

		case MsgPointerEvent:
			msg, err := ReadPointerEvent(cc.conn)
			if err != nil {
				cc.logger.Warn().Err(err).Msg("failed to read PointerEvent")
				return
			}
			cc.handlePointerEvent(msg)

		case MsgClientCutText:
			msg, err := ReadClientCutText(cc.conn)
			if err != nil {
				cc.logger.Warn().Err(err).Msg("failed to read ClientCutText")
				return
			}
			if cc.server.config.OnClientCutText != nil {
				cc.server.config.OnClientCutText(msg.Text)
			}

		default:
			cc.logger.Warn().Uint8("type", typeBuf[0]).Msg("unknown message type")
			return
		}
	}
}

func (cc *ClientConn) handleSetPixelFormat(msg SetPixelFormatMsg) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	cc.pixelFormat = msg.Format
	cc.logger.Debug().
		Uint8("bpp", msg.Format.BitsPerPixel).
		Uint8("depth", msg.Format.Depth).
		Msg("client set pixel format")
}

func (cc *ClientConn) handleSetEncodings(msg SetEncodingsMsg) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	cc.encodings = msg.Encodings
	cc.encoder = SelectEncoder(msg.Encodings)
	cc.logger.Debug().
		Ints("encodings", func() []int {
			r := make([]int, len(msg.Encodings))
			for i, e := range msg.Encodings {
				r[i] = int(e)
			}
			return r
		}()).
		Str("selected", fmt.Sprintf("%T", cc.encoder)).
		Msg("client set encodings")
}

func (cc *ClientConn) handleFramebufferUpdateRequest(msg FramebufferUpdateRequestMsg) error {
	cc.mu.Lock()
	pf := cc.pixelFormat
	encoder := cc.encoder
	cc.mu.Unlock()

	frame := cc.server.config.FrameProvider.GetFrame()
	if frame == nil {
		// No frame available, send empty update
		cc.writeMu.Lock()
		defer cc.writeMu.Unlock()
		return WriteFramebufferUpdate(cc.conn, 0)
	}

	// Create image.RGBA from FrameData
	img := frameDataToRGBA(frame)

	// Check if framebuffer size changed
	newWidth := uint16(frame.Width)
	newHeight := uint16(frame.Height)
	if newWidth != cc.fbWidth || newHeight != cc.fbHeight {
		cc.fbWidth = newWidth
		cc.fbHeight = newHeight
		// Send desktop size pseudo-encoding
		if err := cc.sendDesktopSizeUpdate(newWidth, newHeight); err != nil {
			return err
		}
	}

	// Determine rectangle to send
	rect := Rectangle{
		X:            msg.X,
		Y:            msg.Y,
		Width:        msg.Width,
		Height:       msg.Height,
		EncodingType: encoder.Type(),
	}

	// Clamp to frame bounds
	if int(rect.X+rect.Width) > frame.Width {
		rect.Width = uint16(frame.Width) - rect.X
	}
	if int(rect.Y+rect.Height) > frame.Height {
		rect.Height = uint16(frame.Height) - rect.Y
	}

	cc.writeMu.Lock()
	defer cc.writeMu.Unlock()

	// Write framebuffer update
	if err := WriteFramebufferUpdate(cc.conn, 1); err != nil {
		return err
	}

	// Write rectangle header
	if _, err := cc.conn.Write(rect.MarshalHeader()); err != nil {
		return err
	}

	// Write encoded pixel data
	return encoder.Encode(cc.conn, img, rect, pf)
}

func (cc *ClientConn) sendDesktopSizeUpdate(width, height uint16) error {
	cc.writeMu.Lock()
	defer cc.writeMu.Unlock()

	if err := WriteFramebufferUpdate(cc.conn, 1); err != nil {
		return err
	}
	rect := Rectangle{
		X:            0,
		Y:            0,
		Width:        width,
		Height:       height,
		EncodingType: EncodingDesktopSize,
	}
	_, err := cc.conn.Write(rect.MarshalHeader())
	return err
}

func (cc *ClientConn) sendServerCutText(text string) error {
	cc.writeMu.Lock()
	defer cc.writeMu.Unlock()
	return WriteServerCutText(cc.conn, text)
}

func (cc *ClientConn) handleKeyEvent(msg KeyEventMsg) {
	if cc.server.config.Input != nil {
		cc.server.config.Input.KeyEvent(msg.Key, msg.DownFlag)
	}
}

func (cc *ClientConn) handlePointerEvent(msg PointerEventMsg) {
	if cc.server.config.Input != nil {
		cc.server.config.Input.PointerEvent(msg.ButtonMask, msg.X, msg.Y)
	}
}

// frameDataToRGBA converts FrameData to an image.RGBA for use with encoders.
func frameDataToRGBA(f *FrameData) *image.RGBA {
	return &image.RGBA{
		Pix:    f.Pix,
		Stride: f.Stride,
		Rect:   image.Rect(0, 0, f.Width, f.Height),
	}
}
