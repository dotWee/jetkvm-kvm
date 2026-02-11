package vnc

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

const (
	// defaultMaxConnections is the default maximum number of concurrent VNC connections.
	defaultMaxConnections = 10

	// connReadTimeout is how long to wait for a client message before timing out.
	// This prevents goroutine leaks from idle or abandoned connections.
	connReadTimeout = 5 * time.Minute
)

var (
	ErrServerClosed = errors.New("vnc: server closed")
	ErrAuthFailed   = errors.New("vnc: authentication failed")
)

// InputEventHandler defines the interface for handling VNC input events.
type InputEventHandler interface {
	// HandleKeyEvent handles a VNC key event.
	// keysym is an X11 keysym value, pressed indicates key down (true) or up (false).
	HandleKeyEvent(keysym uint32, pressed bool)
	// HandlePointerEvent handles a VNC pointer (mouse) event.
	// buttonMask is a bitmask of pressed buttons, x and y are absolute coordinates.
	HandlePointerEvent(buttonMask uint8, x, y uint16)
}

// Server implements a VNC (RFB protocol) server.
type Server struct {
	listener     net.Listener
	framebuffer  *Framebuffer
	inputHandler InputEventHandler
	password     string
	logger       *zerolog.Logger
	maxConns     int

	mu       sync.Mutex
	conns    map[net.Conn]struct{}
	closed   bool
	closedCh chan struct{}
}

// NewServer creates a new VNC server with the specified framebuffer and input handler.
func NewServer(fb *Framebuffer, inputHandler InputEventHandler, opts ...Option) *Server {
	s := &Server{
		framebuffer:  fb,
		inputHandler: inputHandler,
		conns:        make(map[net.Conn]struct{}),
		closedCh:     make(chan struct{}),
		maxConns:     defaultMaxConnections,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Option configures a VNC server.
type Option func(*Server)

// WithPassword sets the VNC authentication password.
func WithPassword(password string) Option {
	return func(s *Server) {
		s.password = password
	}
}

// WithLogger sets the logger for the VNC server.
func WithLogger(logger *zerolog.Logger) Option {
	return func(s *Server) {
		s.logger = logger
	}
}

// WithMaxConnections sets the maximum number of concurrent VNC connections.
func WithMaxConnections(max int) Option {
	return func(s *Server) {
		if max > 0 {
			s.maxConns = max
		}
	}
}

// Serve starts accepting VNC connections on the given listener.
func (s *Server) Serve(l net.Listener) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrServerClosed
	}
	s.listener = l
	s.mu.Unlock()

	s.logInfo().Str("addr", l.Addr().String()).Msg("VNC server listening")

	for {
		conn, err := l.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed {
				return ErrServerClosed
			}
			s.logWarn().Err(err).Msg("error accepting VNC connection")
			continue
		}

		s.mu.Lock()
		if len(s.conns) >= s.maxConns {
			s.mu.Unlock()
			s.logWarn().Int("max", s.maxConns).Msg("max VNC connections reached, rejecting")
			conn.Close()
			continue
		}
		s.conns[conn] = struct{}{}
		s.mu.Unlock()

		go s.handleConnection(conn)
	}
}

// Close shuts down the VNC server and closes all connections.
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true
	close(s.closedCh)

	var firstErr error
	if s.listener != nil {
		if err := s.listener.Close(); err != nil {
			firstErr = err
		}
	}

	// Collect connections first, then close them to avoid modifying map during iteration.
	conns := make([]net.Conn, 0, len(s.conns))
	for conn := range s.conns {
		conns = append(conns, conn)
	}
	s.conns = make(map[net.Conn]struct{})
	for _, conn := range conns {
		if err := conn.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *Server) removeConn(conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conns, conn)
}

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()
	defer s.removeConn(conn)

	s.logInfo().Str("remote", conn.RemoteAddr().String()).Msg("new VNC connection")

	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)

	if err := s.handshake(r, w, conn); err != nil {
		s.logWarn().Err(err).Str("remote", conn.RemoteAddr().String()).Msg("VNC handshake failed")
		return
	}

	s.logInfo().Str("remote", conn.RemoteAddr().String()).Msg("VNC client connected")

	if err := s.handleClientMessages(r, w, conn); err != nil {
		if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) && !isTimeoutError(err) {
			s.logWarn().Err(err).Str("remote", conn.RemoteAddr().String()).Msg("VNC client error")
		}
	}

	s.logInfo().Str("remote", conn.RemoteAddr().String()).Msg("VNC client disconnected")
}

func (s *Server) handshake(r *bufio.Reader, w *bufio.Writer, conn net.Conn) error {
	// Send protocol version
	if _, err := w.WriteString(rfbProtocolVersion); err != nil {
		return fmt.Errorf("failed to send protocol version: %w", err)
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("failed to flush protocol version: %w", err)
	}

	// Read client protocol version
	clientVersion := make([]byte, 12)
	if _, err := io.ReadFull(r, clientVersion); err != nil {
		return fmt.Errorf("failed to read client version: %w", err)
	}

	// Perform authentication using a combined reader/writer that shares the buffered streams
	rw := &readWriteFlusher{r: r, w: w}
	if err := performAuth(rw, s.password); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	// Read ClientInit (shared flag)
	sharedFlag := make([]byte, 1)
	if _, err := io.ReadFull(r, sharedFlag); err != nil {
		return fmt.Errorf("failed to read client init: %w", err)
	}

	// Send ServerInit
	if err := s.sendServerInit(w); err != nil {
		return fmt.Errorf("failed to send server init: %w", err)
	}

	return w.Flush()
}

func (s *Server) sendServerInit(w *bufio.Writer) error {
	fbWidth := uint16(s.framebuffer.Width())
	fbHeight := uint16(s.framebuffer.Height())

	// Framebuffer width and height
	if err := binary.Write(w, binary.BigEndian, fbWidth); err != nil {
		return err
	}
	if err := binary.Write(w, binary.BigEndian, fbHeight); err != nil {
		return err
	}

	// Pixel format
	pf := defaultPixelFormat()
	if err := pf.write(w); err != nil {
		return err
	}

	// Desktop name
	name := "JetKVM"
	nameLen := uint32(len(name))
	if err := binary.Write(w, binary.BigEndian, nameLen); err != nil {
		return err
	}
	if _, err := w.WriteString(name); err != nil {
		return err
	}

	return nil
}

func (s *Server) handleClientMessages(r *bufio.Reader, w *bufio.Writer, conn net.Conn) error {
	for {
		// Set a read deadline to prevent goroutine leaks from idle/abandoned connections.
		if err := conn.SetReadDeadline(time.Now().Add(connReadTimeout)); err != nil {
			return err
		}

		msgType := make([]byte, 1)
		if _, err := io.ReadFull(r, msgType); err != nil {
			return err
		}

		switch msgType[0] {
		case msgSetPixelFormat:
			// Read padding (3 bytes) + pixel format (16 bytes)
			padding := make([]byte, 3)
			if _, err := io.ReadFull(r, padding); err != nil {
				return err
			}
			if _, err := readPixelFormat(r); err != nil {
				return err
			}
			// We acknowledge but always use our default format

		case msgSetEncodings:
			// Read padding (1 byte) + number of encodings (2 bytes)
			header := make([]byte, 3)
			if _, err := io.ReadFull(r, header); err != nil {
				return err
			}
			numEncodings := binary.BigEndian.Uint16(header[1:3])
			if numEncodings > maxEncodings {
				return fmt.Errorf("too many encodings requested: %d", numEncodings)
			}
			// Read and discard encoding types
			encodings := make([]byte, numEncodings*4)
			if _, err := io.ReadFull(r, encodings); err != nil {
				return err
			}

		case msgFramebufferUpdateRequest:
			req, err := readFramebufferUpdateRequest(r)
			if err != nil {
				return err
			}
			if err := s.sendFramebufferUpdate(w, req); err != nil {
				return err
			}
			if err := w.Flush(); err != nil {
				return err
			}

		case msgKeyEvent:
			evt, err := readKeyEvent(r)
			if err != nil {
				return err
			}
			if s.inputHandler != nil {
				s.inputHandler.HandleKeyEvent(evt.Key, evt.DownFlag != 0)
			}

		case msgPointerEvent:
			evt, err := readPointerEvent(r)
			if err != nil {
				return err
			}
			if s.inputHandler != nil {
				s.inputHandler.HandlePointerEvent(evt.ButtonMask, evt.X, evt.Y)
			}

		case msgClientCutText:
			// Read padding (3 bytes) + text length (4 bytes)
			header := make([]byte, 7)
			if _, err := io.ReadFull(r, header); err != nil {
				return err
			}
			textLen := binary.BigEndian.Uint32(header[3:7])
			if textLen > maxClientCutTextLen {
				return fmt.Errorf("client cut text too large: %d bytes", textLen)
			}
			// Read and discard clipboard text
			if textLen > 0 {
				text := make([]byte, textLen)
				if _, err := io.ReadFull(r, text); err != nil {
					return err
				}
			}

		default:
			return fmt.Errorf("unknown client message type: %d", msgType[0])
		}
	}
}

func (s *Server) sendFramebufferUpdate(w *bufio.Writer, req FramebufferUpdateRequest) error {
	width := req.Width
	height := req.Height
	x := req.X
	y := req.Y

	// Clamp to framebuffer bounds
	fbW := uint16(s.framebuffer.Width())
	fbH := uint16(s.framebuffer.Height())

	if x >= fbW || y >= fbH {
		return writeFramebufferUpdateHeader(w, 0)
	}
	if x+width > fbW {
		width = fbW - x
	}
	if y+height > fbH {
		height = fbH - y
	}

	if width == 0 || height == 0 {
		return writeFramebufferUpdateHeader(w, 0)
	}

	data := s.framebuffer.GetRect(int(x), int(y), int(width), int(height))
	if data == nil {
		return writeFramebufferUpdateHeader(w, 0)
	}

	if err := writeFramebufferUpdateHeader(w, 1); err != nil {
		return err
	}
	return writeRawRect(w, x, y, width, height, data)
}

func (s *Server) logInfo() *zerolog.Event {
	if s.logger != nil {
		return s.logger.Info()
	}
	l := zerolog.Nop()
	return l.Info()
}

func (s *Server) logWarn() *zerolog.Event {
	if s.logger != nil {
		return s.logger.Warn()
	}
	l := zerolog.Nop()
	return l.Warn()
}

// readWriteFlusher wraps a bufio.Reader and bufio.Writer to implement io.ReadWriter
// with automatic flushing on Write.
type readWriteFlusher struct {
	r *bufio.Reader
	w *bufio.Writer
}

func (rw *readWriteFlusher) Read(p []byte) (int, error) {
	return rw.r.Read(p)
}

func (rw *readWriteFlusher) Write(p []byte) (int, error) {
	n, err := rw.w.Write(p)
	if err != nil {
		return n, err
	}
	return n, rw.w.Flush()
}

// isTimeoutError checks if an error is a network timeout error.
func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	netErr, ok := err.(net.Error)
	return ok && netErr.Timeout()
}
