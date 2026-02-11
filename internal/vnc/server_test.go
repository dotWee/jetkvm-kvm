package vnc

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockInputHandler records input events for testing.
type mockInputHandler struct {
	mu            sync.Mutex
	keyEvents     []KeyEvent
	pointerEvents []PointerEvent
}

func (m *mockInputHandler) HandleKeyEvent(keysym uint32, pressed bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	down := uint8(0)
	if pressed {
		down = 1
	}
	m.keyEvents = append(m.keyEvents, KeyEvent{DownFlag: down, Key: keysym})
}

func (m *mockInputHandler) HandlePointerEvent(buttonMask uint8, x, y uint16) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pointerEvents = append(m.pointerEvents, PointerEvent{ButtonMask: buttonMask, X: x, Y: y})
}

func (m *mockInputHandler) getKeyEvents() []KeyEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	events := make([]KeyEvent, len(m.keyEvents))
	copy(events, m.keyEvents)
	return events
}

func (m *mockInputHandler) getPointerEvents() []PointerEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	events := make([]PointerEvent, len(m.pointerEvents))
	copy(events, m.pointerEvents)
	return events
}

// --- Framebuffer Tests ---

func TestNewFramebuffer(t *testing.T) {
	fb := NewFramebuffer(1920, 1080)
	assert.Equal(t, 1920, fb.Width())
	assert.Equal(t, 1080, fb.Height())
}

func TestFramebufferUpdate(t *testing.T) {
	fb := NewFramebuffer(2, 2)

	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.SetRGBA(0, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	img.SetRGBA(1, 0, color.RGBA{R: 0, G: 255, B: 0, A: 255})
	img.SetRGBA(0, 1, color.RGBA{R: 0, G: 0, B: 255, A: 255})
	img.SetRGBA(1, 1, color.RGBA{R: 255, G: 255, B: 255, A: 255})

	fb.Update(img)

	// GetRect returns BGRA data
	data := fb.GetRect(0, 0, 2, 2)
	require.Len(t, data, 16)

	// Pixel (0,0): RGBA(255,0,0,255) -> BGRA(0,0,255,255)
	assert.Equal(t, byte(0), data[0])   // B
	assert.Equal(t, byte(0), data[1])   // G
	assert.Equal(t, byte(255), data[2]) // R
	assert.Equal(t, byte(255), data[3]) // A

	// Pixel (1,0): RGBA(0,255,0,255) -> BGRA(0,255,0,255)
	assert.Equal(t, byte(0), data[4])   // B
	assert.Equal(t, byte(255), data[5]) // G
	assert.Equal(t, byte(0), data[6])   // R
	assert.Equal(t, byte(255), data[7]) // A
}

func TestFramebufferResize(t *testing.T) {
	fb := NewFramebuffer(100, 100)
	assert.Equal(t, 100, fb.Width())
	assert.Equal(t, 100, fb.Height())

	fb.Resize(200, 150)
	assert.Equal(t, 200, fb.Width())
	assert.Equal(t, 150, fb.Height())
}

func TestFramebufferUpdateWithResize(t *testing.T) {
	fb := NewFramebuffer(100, 100)

	// Update with a differently sized image should resize
	img := image.NewRGBA(image.Rect(0, 0, 50, 50))
	fb.Update(img)

	assert.Equal(t, 50, fb.Width())
	assert.Equal(t, 50, fb.Height())
}

func TestFramebufferGetRectPartial(t *testing.T) {
	fb := NewFramebuffer(4, 4)

	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for x := 0; x < 4; x++ {
		for y := 0; y < 4; y++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 64), G: uint8(y * 64), B: 128, A: 255})
		}
	}
	fb.Update(img)

	// Get a 2x2 sub-region
	data := fb.GetRect(1, 1, 2, 2)
	require.Len(t, data, 16)
}

func TestFramebufferConcurrentAccess(t *testing.T) {
	fb := NewFramebuffer(100, 100)
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			img := image.NewRGBA(image.Rect(0, 0, 100, 100))
			fb.Update(img)
		}()
		go func() {
			defer wg.Done()
			_ = fb.GetRect(0, 0, 100, 100)
		}()
	}
	wg.Wait()
}

// --- Auth Tests ---

func TestReverseBits(t *testing.T) {
	tests := []struct {
		input    byte
		expected byte
	}{
		{0b10000000, 0b00000001},
		{0b11001100, 0b00110011},
		{0b00000000, 0b00000000},
		{0b11111111, 0b11111111},
		{0b10101010, 0b01010101},
	}

	for _, tt := range tests {
		result := reverseBits(tt.input)
		assert.Equal(t, tt.expected, result, "reverseBits(0x%02x)", tt.input)
	}
}

func TestVNCAuthEncryptDecrypt(t *testing.T) {
	password := "secret"
	challenge := make([]byte, 16)
	for i := range challenge {
		challenge[i] = byte(i)
	}

	encrypted := vncAuthEncrypt(challenge, password)
	require.Len(t, encrypted, 16)

	// Should verify correctly
	assert.True(t, vncAuthVerify(challenge, encrypted, password))

	// Should fail with wrong password
	assert.False(t, vncAuthVerify(challenge, encrypted, "wrong"))
}

func TestVNCAuthPasswordTruncation(t *testing.T) {
	challenge := make([]byte, 16)
	for i := range challenge {
		challenge[i] = byte(i * 3)
	}

	// Passwords longer than 8 chars should be truncated
	enc1 := vncAuthEncrypt(challenge, "12345678")
	enc2 := vncAuthEncrypt(challenge, "12345678extra")
	assert.Equal(t, enc1, enc2, "passwords longer than 8 chars should produce same result")
}

func TestAuthNoPassword(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	errCh := make(chan error, 1)
	go func() {
		rw := &readWriteFlusher{
			r: bufio.NewReader(server),
			w: bufio.NewWriter(server),
		}
		errCh <- performAuth(rw, "")
	}()

	// Client side: read security types
	secTypes := make([]byte, 2)
	_, err := io.ReadFull(client, secTypes)
	require.NoError(t, err)
	assert.Equal(t, byte(1), secTypes[0])        // number of security types
	assert.Equal(t, byte(secTypeNone), secTypes[1]) // None

	// Client: choose security type None
	_, err = client.Write([]byte{secTypeNone})
	require.NoError(t, err)

	// Client: read SecurityResult
	result := make([]byte, 4)
	_, err = io.ReadFull(client, result)
	require.NoError(t, err)
	assert.Equal(t, []byte{0, 0, 0, 0}, result) // success

	require.NoError(t, <-errCh)
}

func TestAuthWithPassword(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	password := "testpass"
	errCh := make(chan error, 1)
	go func() {
		rw := &readWriteFlusher{
			r: bufio.NewReader(server),
			w: bufio.NewWriter(server),
		}
		errCh <- performAuth(rw, password)
	}()

	// Client: read security types
	secTypes := make([]byte, 2)
	_, err := io.ReadFull(client, secTypes)
	require.NoError(t, err)
	assert.Equal(t, byte(secTypeVNCAuth), secTypes[1])

	// Client: choose VNC auth
	_, err = client.Write([]byte{secTypeVNCAuth})
	require.NoError(t, err)

	// Client: read challenge
	challenge := make([]byte, 16)
	_, err = io.ReadFull(client, challenge)
	require.NoError(t, err)

	// Client: encrypt and send response
	response := vncAuthEncrypt(challenge, password)
	_, err = client.Write(response)
	require.NoError(t, err)

	// Client: read SecurityResult
	result := make([]byte, 4)
	_, err = io.ReadFull(client, result)
	require.NoError(t, err)
	assert.Equal(t, []byte{0, 0, 0, 0}, result) // success

	require.NoError(t, <-errCh)
}

func TestAuthWithWrongPassword(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	errCh := make(chan error, 1)
	go func() {
		rw := &readWriteFlusher{
			r: bufio.NewReader(server),
			w: bufio.NewWriter(server),
		}
		errCh <- performAuth(rw, "correct")
	}()

	// Client: read security types
	secTypes := make([]byte, 2)
	_, err := io.ReadFull(client, secTypes)
	require.NoError(t, err)

	// Client: choose VNC auth
	_, err = client.Write([]byte{secTypeVNCAuth})
	require.NoError(t, err)

	// Client: read challenge
	challenge := make([]byte, 16)
	_, err = io.ReadFull(client, challenge)
	require.NoError(t, err)

	// Client: encrypt with WRONG password
	response := vncAuthEncrypt(challenge, "wrong")
	_, err = client.Write(response)
	require.NoError(t, err)

	// Client: read SecurityResult
	result := make([]byte, 4)
	_, err = io.ReadFull(client, result)
	require.NoError(t, err)
	assert.Equal(t, []byte{0, 0, 0, 1}, result) // failure

	require.ErrorIs(t, <-errCh, ErrAuthFailed)
}

// --- Message Tests ---

func TestPixelFormatRoundTrip(t *testing.T) {
	pf := defaultPixelFormat()

	var buf bytes.Buffer
	require.NoError(t, pf.write(&buf))

	parsed, err := readPixelFormat(&buf)
	require.NoError(t, err)

	assert.Equal(t, pf.BitsPerPixel, parsed.BitsPerPixel)
	assert.Equal(t, pf.Depth, parsed.Depth)
	assert.Equal(t, pf.TrueColorFlag, parsed.TrueColorFlag)
	assert.Equal(t, pf.RedMax, parsed.RedMax)
	assert.Equal(t, pf.GreenMax, parsed.GreenMax)
	assert.Equal(t, pf.BlueMax, parsed.BlueMax)
	assert.Equal(t, pf.RedShift, parsed.RedShift)
	assert.Equal(t, pf.GreenShift, parsed.GreenShift)
	assert.Equal(t, pf.BlueShift, parsed.BlueShift)
}

func TestReadKeyEvent(t *testing.T) {
	buf := make([]byte, 7)
	buf[0] = 1 // down
	// bytes 1-2 padding
	binary.BigEndian.PutUint32(buf[3:7], 0xff51) // XK_Left

	evt, err := readKeyEvent(bytes.NewReader(buf))
	require.NoError(t, err)
	assert.Equal(t, uint8(1), evt.DownFlag)
	assert.Equal(t, uint32(0xff51), evt.Key)
}

func TestReadPointerEvent(t *testing.T) {
	buf := make([]byte, 5)
	buf[0] = 1 // left button
	binary.BigEndian.PutUint16(buf[1:3], 100)
	binary.BigEndian.PutUint16(buf[3:5], 200)

	evt, err := readPointerEvent(bytes.NewReader(buf))
	require.NoError(t, err)
	assert.Equal(t, uint8(1), evt.ButtonMask)
	assert.Equal(t, uint16(100), evt.X)
	assert.Equal(t, uint16(200), evt.Y)
}

func TestReadFramebufferUpdateRequest(t *testing.T) {
	buf := make([]byte, 9)
	buf[0] = 0 // not incremental
	binary.BigEndian.PutUint16(buf[1:3], 10)
	binary.BigEndian.PutUint16(buf[3:5], 20)
	binary.BigEndian.PutUint16(buf[5:7], 640)
	binary.BigEndian.PutUint16(buf[7:9], 480)

	req, err := readFramebufferUpdateRequest(bytes.NewReader(buf))
	require.NoError(t, err)
	assert.Equal(t, uint8(0), req.Incremental)
	assert.Equal(t, uint16(10), req.X)
	assert.Equal(t, uint16(20), req.Y)
	assert.Equal(t, uint16(640), req.Width)
	assert.Equal(t, uint16(480), req.Height)
}

func TestWriteFramebufferUpdateHeader(t *testing.T) {
	var buf bytes.Buffer
	err := writeFramebufferUpdateHeader(&buf, 3)
	require.NoError(t, err)

	data := buf.Bytes()
	assert.Equal(t, byte(msgFramebufferUpdate), data[0])
	assert.Equal(t, uint16(3), binary.BigEndian.Uint16(data[2:4]))
}

func TestWriteRawRect(t *testing.T) {
	var buf bytes.Buffer
	pixels := make([]byte, 8) // 2x1 pixels, 4 bytes each
	pixels[0] = 0xFF          // first pixel B
	pixels[4] = 0xAA          // second pixel B

	err := writeRawRect(&buf, 10, 20, 2, 1, pixels)
	require.NoError(t, err)

	data := buf.Bytes()
	// Header: x(2) + y(2) + width(2) + height(2) + encoding(4) = 12 bytes
	assert.Equal(t, uint16(10), binary.BigEndian.Uint16(data[0:2]))
	assert.Equal(t, uint16(20), binary.BigEndian.Uint16(data[2:4]))
	assert.Equal(t, uint16(2), binary.BigEndian.Uint16(data[4:6]))
	assert.Equal(t, uint16(1), binary.BigEndian.Uint16(data[6:8]))
	assert.Equal(t, uint32(encodingRaw), binary.BigEndian.Uint32(data[8:12]))
	assert.Equal(t, pixels, data[12:])
}

// --- Server Integration Tests ---

// waitForServer polls until a TCP connection can be established, or fails the test.
func waitForServer(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("server did not start in time")
}

func TestServerStartStop(t *testing.T) {
	fb := NewFramebuffer(800, 600)
	server := NewServer(fb, nil)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(listener)
	}()

	// Verify server is accepting connections by connecting
	waitForServer(t, listener.Addr().String())

	err = server.Close()
	require.NoError(t, err)

	err = <-errCh
	assert.ErrorIs(t, err, ErrServerClosed)
}

func TestServerDoubleClose(t *testing.T) {
	fb := NewFramebuffer(800, 600)
	server := NewServer(fb, nil)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() { _ = server.Serve(listener) }()
	waitForServer(t, listener.Addr().String())

	require.NoError(t, server.Close())
	require.NoError(t, server.Close()) // second close should not error
}

func TestServerServeAfterClose(t *testing.T) {
	fb := NewFramebuffer(800, 600)
	server := NewServer(fb, nil)
	server.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	err = server.Serve(listener)
	assert.ErrorIs(t, err, ErrServerClosed)
}

// vncClientHelper simulates a VNC client for testing.
type vncClientHelper struct {
	conn net.Conn
	t    *testing.T
}

func newVNCClient(t *testing.T, addr string) *vncClientHelper {
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	require.NoError(t, err)
	return &vncClientHelper{conn: conn, t: t}
}

func (c *vncClientHelper) close() {
	c.conn.Close()
}

func (c *vncClientHelper) doHandshake(password string) {
	t := c.t

	// Read server protocol version
	version := make([]byte, 12)
	_, err := io.ReadFull(c.conn, version)
	require.NoError(t, err)
	assert.Equal(t, rfbProtocolVersion, string(version))

	// Send client protocol version
	_, err = c.conn.Write([]byte(rfbProtocolVersion))
	require.NoError(t, err)

	// Read security types
	numTypes := make([]byte, 1)
	_, err = io.ReadFull(c.conn, numTypes)
	require.NoError(t, err)

	secTypes := make([]byte, numTypes[0])
	_, err = io.ReadFull(c.conn, secTypes)
	require.NoError(t, err)

	if password == "" {
		// Choose None
		_, err = c.conn.Write([]byte{secTypeNone})
		require.NoError(t, err)
	} else {
		// Choose VNC Auth
		_, err = c.conn.Write([]byte{secTypeVNCAuth})
		require.NoError(t, err)

		// Read challenge
		challenge := make([]byte, 16)
		_, err = io.ReadFull(c.conn, challenge)
		require.NoError(t, err)

		// Send response
		response := vncAuthEncrypt(challenge, password)
		_, err = c.conn.Write(response)
		require.NoError(t, err)
	}

	// Read SecurityResult
	result := make([]byte, 4)
	_, err = io.ReadFull(c.conn, result)
	require.NoError(t, err)
	assert.Equal(t, []byte{0, 0, 0, 0}, result)

	// Send ClientInit (shared = 1)
	_, err = c.conn.Write([]byte{1})
	require.NoError(t, err)

	// Read ServerInit
	// width (2) + height (2) + pixel format (16) + name length (4) + name
	header := make([]byte, 24)
	_, err = io.ReadFull(c.conn, header)
	require.NoError(t, err)

	nameLen := binary.BigEndian.Uint32(header[20:24])
	name := make([]byte, nameLen)
	_, err = io.ReadFull(c.conn, name)
	require.NoError(t, err)
	assert.Equal(t, "JetKVM", string(name))
}

func (c *vncClientHelper) sendFramebufferUpdateRequest(incremental bool, x, y, w, h uint16) {
	buf := make([]byte, 10)
	buf[0] = msgFramebufferUpdateRequest
	if incremental {
		buf[1] = 1
	}
	binary.BigEndian.PutUint16(buf[2:4], x)
	binary.BigEndian.PutUint16(buf[4:6], y)
	binary.BigEndian.PutUint16(buf[6:8], w)
	binary.BigEndian.PutUint16(buf[8:10], h)
	_, err := c.conn.Write(buf)
	require.NoError(c.t, err)
}

func (c *vncClientHelper) readFramebufferUpdate() (numRects uint16, err error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(c.conn, header); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(header[2:4]), nil
}

func (c *vncClientHelper) readRawRect() (x, y, w, h uint16, data []byte, err error) {
	header := make([]byte, 12)
	if _, err := io.ReadFull(c.conn, header); err != nil {
		return 0, 0, 0, 0, nil, err
	}
	x = binary.BigEndian.Uint16(header[0:2])
	y = binary.BigEndian.Uint16(header[2:4])
	w = binary.BigEndian.Uint16(header[4:6])
	h = binary.BigEndian.Uint16(header[6:8])
	encoding := binary.BigEndian.Uint32(header[8:12])
	assert.Equal(c.t, uint32(encodingRaw), encoding)

	dataLen := int(w) * int(h) * 4
	data = make([]byte, dataLen)
	if _, err := io.ReadFull(c.conn, data); err != nil {
		return 0, 0, 0, 0, nil, err
	}
	return x, y, w, h, data, nil
}

func (c *vncClientHelper) sendKeyEvent(down bool, key uint32) {
	buf := make([]byte, 8)
	buf[0] = msgKeyEvent
	if down {
		buf[1] = 1
	}
	// bytes 2-3 padding
	binary.BigEndian.PutUint32(buf[4:8], key)
	_, err := c.conn.Write(buf)
	require.NoError(c.t, err)
}

func (c *vncClientHelper) sendPointerEvent(buttonMask uint8, x, y uint16) {
	buf := make([]byte, 6)
	buf[0] = msgPointerEvent
	buf[1] = buttonMask
	binary.BigEndian.PutUint16(buf[2:4], x)
	binary.BigEndian.PutUint16(buf[4:6], y)
	_, err := c.conn.Write(buf)
	require.NoError(c.t, err)
}

func TestServerFullConnection(t *testing.T) {
	fb := NewFramebuffer(800, 600)
	handler := &mockInputHandler{}
	server := NewServer(fb, handler)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	client := newVNCClient(t, listener.Addr().String())
	defer client.close()

	client.doHandshake("")

	// Request full framebuffer update
	client.sendFramebufferUpdateRequest(false, 0, 0, 800, 600)

	numRects, err := client.readFramebufferUpdate()
	require.NoError(t, err)
	assert.Equal(t, uint16(1), numRects)

	x, y, w, h, data, err := client.readRawRect()
	require.NoError(t, err)
	assert.Equal(t, uint16(0), x)
	assert.Equal(t, uint16(0), y)
	assert.Equal(t, uint16(800), w)
	assert.Equal(t, uint16(600), h)
	assert.Len(t, data, 800*600*4)
}

func TestServerWithPassword(t *testing.T) {
	fb := NewFramebuffer(640, 480)
	server := NewServer(fb, nil, WithPassword("mypasswd"))

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	client := newVNCClient(t, listener.Addr().String())
	defer client.close()

	client.doHandshake("mypasswd")

	// Request update to verify connection works
	client.sendFramebufferUpdateRequest(false, 0, 0, 640, 480)

	numRects, err := client.readFramebufferUpdate()
	require.NoError(t, err)
	assert.Equal(t, uint16(1), numRects)
}

func TestServerKeyEvents(t *testing.T) {
	fb := NewFramebuffer(800, 600)
	handler := &mockInputHandler{}
	server := NewServer(fb, handler)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	client := newVNCClient(t, listener.Addr().String())
	defer client.close()

	client.doHandshake("")

	// Send key events
	client.sendKeyEvent(true, 0xff51)  // Left arrow down
	client.sendKeyEvent(false, 0xff51) // Left arrow up
	client.sendKeyEvent(true, 0x0061)  // 'a' down

	// Request a framebuffer update to ensure all events are processed
	client.sendFramebufferUpdateRequest(false, 0, 0, 1, 1)
	_, err = client.readFramebufferUpdate()
	require.NoError(t, err)
	// Read the rect data
	_, _, _, _, _, err = client.readRawRect()
	require.NoError(t, err)

	// Poll for events to be processed
	require.Eventually(t, func() bool {
		return len(handler.getKeyEvents()) == 3
	}, 2*time.Second, 10*time.Millisecond)

	events := handler.getKeyEvents()
	require.Len(t, events, 3)
	assert.Equal(t, uint32(0xff51), events[0].Key)
	assert.Equal(t, uint8(1), events[0].DownFlag)
	assert.Equal(t, uint32(0xff51), events[1].Key)
	assert.Equal(t, uint8(0), events[1].DownFlag)
	assert.Equal(t, uint32(0x0061), events[2].Key)
	assert.Equal(t, uint8(1), events[2].DownFlag)
}

func TestServerPointerEvents(t *testing.T) {
	fb := NewFramebuffer(800, 600)
	handler := &mockInputHandler{}
	server := NewServer(fb, handler)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	client := newVNCClient(t, listener.Addr().String())
	defer client.close()

	client.doHandshake("")

	// Send pointer events
	client.sendPointerEvent(0, 100, 200)    // move
	client.sendPointerEvent(1, 100, 200)    // left click
	client.sendPointerEvent(0, 300, 400)    // release + move

	// Request a framebuffer update to ensure all events are processed
	client.sendFramebufferUpdateRequest(false, 0, 0, 1, 1)
	_, err = client.readFramebufferUpdate()
	require.NoError(t, err)
	_, _, _, _, _, err = client.readRawRect()
	require.NoError(t, err)

	// Poll for events to be processed
	require.Eventually(t, func() bool {
		return len(handler.getPointerEvents()) == 3
	}, 2*time.Second, 10*time.Millisecond)

	events := handler.getPointerEvents()
	require.Len(t, events, 3)
	assert.Equal(t, uint16(100), events[0].X)
	assert.Equal(t, uint16(200), events[0].Y)
	assert.Equal(t, uint8(0), events[0].ButtonMask)
	assert.Equal(t, uint8(1), events[1].ButtonMask)
	assert.Equal(t, uint16(300), events[2].X)
	assert.Equal(t, uint16(400), events[2].Y)
}

func TestServerFramebufferContent(t *testing.T) {
	fb := NewFramebuffer(2, 2)

	// Fill with known pattern
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.SetRGBA(0, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	img.SetRGBA(1, 0, color.RGBA{R: 0, G: 255, B: 0, A: 255})
	img.SetRGBA(0, 1, color.RGBA{R: 0, G: 0, B: 255, A: 255})
	img.SetRGBA(1, 1, color.RGBA{R: 128, G: 128, B: 128, A: 255})
	fb.Update(img)

	server := NewServer(fb, nil)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	client := newVNCClient(t, listener.Addr().String())
	defer client.close()

	client.doHandshake("")

	client.sendFramebufferUpdateRequest(false, 0, 0, 2, 2)

	numRects, err := client.readFramebufferUpdate()
	require.NoError(t, err)
	assert.Equal(t, uint16(1), numRects)

	_, _, _, _, data, err := client.readRawRect()
	require.NoError(t, err)

	// Pixel (0,0): RGBA(255,0,0,255) -> BGRA(0,0,255,255)
	assert.Equal(t, byte(0), data[0])   // B
	assert.Equal(t, byte(0), data[1])   // G
	assert.Equal(t, byte(255), data[2]) // R
	assert.Equal(t, byte(255), data[3]) // A

	// Pixel (1,0): RGBA(0,255,0,255) -> BGRA(0,255,0,255)
	assert.Equal(t, byte(0), data[4])   // B
	assert.Equal(t, byte(255), data[5]) // G
	assert.Equal(t, byte(0), data[6])   // R
	assert.Equal(t, byte(255), data[7]) // A
}

func TestServerSubregionRequest(t *testing.T) {
	fb := NewFramebuffer(100, 100)

	server := NewServer(fb, nil)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	client := newVNCClient(t, listener.Addr().String())
	defer client.close()

	client.doHandshake("")

	// Request a sub-region
	client.sendFramebufferUpdateRequest(false, 10, 10, 50, 50)

	numRects, err := client.readFramebufferUpdate()
	require.NoError(t, err)
	assert.Equal(t, uint16(1), numRects)

	x, y, w, h, data, err := client.readRawRect()
	require.NoError(t, err)
	assert.Equal(t, uint16(10), x)
	assert.Equal(t, uint16(10), y)
	assert.Equal(t, uint16(50), w)
	assert.Equal(t, uint16(50), h)
	assert.Len(t, data, 50*50*4)
}

func TestServerMultipleClients(t *testing.T) {
	fb := NewFramebuffer(320, 240)
	server := NewServer(fb, nil)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	// Connect two clients
	client1 := newVNCClient(t, listener.Addr().String())
	defer client1.close()
	client1.doHandshake("")

	client2 := newVNCClient(t, listener.Addr().String())
	defer client2.close()
	client2.doHandshake("")

	// Both should be able to request framebuffer updates
	client1.sendFramebufferUpdateRequest(false, 0, 0, 320, 240)
	client2.sendFramebufferUpdateRequest(false, 0, 0, 320, 240)

	numRects1, err := client1.readFramebufferUpdate()
	require.NoError(t, err)
	assert.Equal(t, uint16(1), numRects1)

	numRects2, err := client2.readFramebufferUpdate()
	require.NoError(t, err)
	assert.Equal(t, uint16(1), numRects2)
}

// --- Edge Case Tests ---

func TestFramebufferGetRectOutOfBounds(t *testing.T) {
	fb := NewFramebuffer(10, 10)

	// Request completely outside bounds
	data := fb.GetRect(20, 20, 5, 5)
	assert.Nil(t, data)

	// Request partially outside bounds (clamped)
	data = fb.GetRect(8, 8, 5, 5)
	assert.NotNil(t, data)
	// Should be clamped to 2x2
	assert.Len(t, data, 2*2*4)

	// Request with negative coordinates
	data = fb.GetRect(-1, -1, 5, 5)
	assert.NotNil(t, data)

	// Zero-size request
	data = fb.GetRect(0, 0, 0, 5)
	assert.Nil(t, data)
}

func TestFramebufferGetRectExactBounds(t *testing.T) {
	fb := NewFramebuffer(10, 10)
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for x := 0; x < 10; x++ {
		for y := 0; y < 10; y++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}
	fb.Update(img)

	// Exact full framebuffer request should work
	data := fb.GetRect(0, 0, 10, 10)
	require.NotNil(t, data)
	assert.Len(t, data, 10*10*4)

	// Request at far edge
	data = fb.GetRect(9, 9, 1, 1)
	require.NotNil(t, data)
	assert.Len(t, data, 4)
}

func TestVNCAuthVerifyConstantTime(t *testing.T) {
	password := "testpass"
	challenge := make([]byte, 16)
	for i := range challenge {
		challenge[i] = byte(i)
	}

	correctResponse := vncAuthEncrypt(challenge, password)

	// Correct password should verify
	assert.True(t, vncAuthVerify(challenge, correctResponse, password))

	// Wrong response should fail
	wrongResponse := make([]byte, 16)
	copy(wrongResponse, correctResponse)
	wrongResponse[0] ^= 0xFF
	assert.False(t, vncAuthVerify(challenge, wrongResponse, password))

	// Wrong length response should fail
	assert.False(t, vncAuthVerify(challenge, correctResponse[:8], password))

	// Empty response should fail
	assert.False(t, vncAuthVerify(challenge, []byte{}, password))
}

func TestVNCAuthEmptyPassword(t *testing.T) {
	challenge := make([]byte, 16)
	for i := range challenge {
		challenge[i] = byte(i)
	}

	// Empty password should still produce a valid encryption
	encrypted := vncAuthEncrypt(challenge, "")
	require.Len(t, encrypted, 16)

	assert.True(t, vncAuthVerify(challenge, encrypted, ""))
	assert.False(t, vncAuthVerify(challenge, encrypted, "notempty"))
}

func TestServerRequestBeyondFramebuffer(t *testing.T) {
	fb := NewFramebuffer(100, 100)
	server := NewServer(fb, nil)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	client := newVNCClient(t, listener.Addr().String())
	defer client.close()

	client.doHandshake("")

	// Request extends beyond framebuffer bounds
	client.sendFramebufferUpdateRequest(false, 90, 90, 100, 100)

	numRects, err := client.readFramebufferUpdate()
	require.NoError(t, err)
	assert.Equal(t, uint16(1), numRects)

	x, y, w, h, data, err := client.readRawRect()
	require.NoError(t, err)
	assert.Equal(t, uint16(90), x)
	assert.Equal(t, uint16(90), y)
	// Should be clamped to 10x10
	assert.Equal(t, uint16(10), w)
	assert.Equal(t, uint16(10), h)
	assert.Len(t, data, 10*10*4)
}

func TestServerRequestCompletelyOutOfBounds(t *testing.T) {
	fb := NewFramebuffer(100, 100)
	server := NewServer(fb, nil)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	client := newVNCClient(t, listener.Addr().String())
	defer client.close()

	client.doHandshake("")

	// Request completely outside framebuffer
	client.sendFramebufferUpdateRequest(false, 200, 200, 50, 50)

	numRects, err := client.readFramebufferUpdate()
	require.NoError(t, err)
	// Should return 0 rects for out-of-bounds requests
	assert.Equal(t, uint16(0), numRects)
}

func (c *vncClientHelper) sendClientCutText(text string) {
	buf := make([]byte, 8+len(text))
	buf[0] = msgClientCutText
	// bytes 1-3 padding
	binary.BigEndian.PutUint32(buf[4:8], uint32(len(text)))
	copy(buf[8:], text)
	_, err := c.conn.Write(buf)
	require.NoError(c.t, err)
}

func TestServerClientCutText(t *testing.T) {
	fb := NewFramebuffer(100, 100)
	server := NewServer(fb, nil)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	client := newVNCClient(t, listener.Addr().String())
	defer client.close()

	client.doHandshake("")

	// Send clipboard text (should not crash)
	client.sendClientCutText("hello clipboard")

	// Connection should still be alive after cut text
	client.sendFramebufferUpdateRequest(false, 0, 0, 1, 1)
	numRects, err := client.readFramebufferUpdate()
	require.NoError(t, err)
	assert.Equal(t, uint16(1), numRects)
}

func (c *vncClientHelper) sendSetPixelFormat() {
	buf := make([]byte, 20)
	buf[0] = msgSetPixelFormat
	// bytes 1-3 padding
	// bytes 4-19: pixel format (16 bytes) - write defaults into correct region
	var pfBuf bytes.Buffer
	pf := defaultPixelFormat()
	pf.write(&pfBuf)
	copy(buf[4:20], pfBuf.Bytes())
	_, err := c.conn.Write(buf)
	require.NoError(c.t, err)
}

func TestServerSetPixelFormat(t *testing.T) {
	fb := NewFramebuffer(100, 100)
	server := NewServer(fb, nil)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	client := newVNCClient(t, listener.Addr().String())
	defer client.close()

	client.doHandshake("")

	// Send SetPixelFormat (should not crash)
	client.sendSetPixelFormat()

	// Connection should still work
	client.sendFramebufferUpdateRequest(false, 0, 0, 1, 1)
	numRects, err := client.readFramebufferUpdate()
	require.NoError(t, err)
	assert.Equal(t, uint16(1), numRects)
}

func (c *vncClientHelper) sendSetEncodings(encodingTypes []int32) {
	buf := make([]byte, 4+len(encodingTypes)*4)
	buf[0] = msgSetEncodings
	// byte 1: padding
	binary.BigEndian.PutUint16(buf[2:4], uint16(len(encodingTypes)))
	for i, enc := range encodingTypes {
		binary.BigEndian.PutUint32(buf[4+i*4:8+i*4], uint32(enc))
	}
	_, err := c.conn.Write(buf)
	require.NoError(c.t, err)
}

func TestServerSetEncodings(t *testing.T) {
	fb := NewFramebuffer(100, 100)
	server := NewServer(fb, nil)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	client := newVNCClient(t, listener.Addr().String())
	defer client.close()

	client.doHandshake("")

	// Send SetEncodings with raw encoding
	client.sendSetEncodings([]int32{encodingRaw})

	// Connection should still work
	client.sendFramebufferUpdateRequest(false, 0, 0, 1, 1)
	numRects, err := client.readFramebufferUpdate()
	require.NoError(t, err)
	assert.Equal(t, uint16(1), numRects)
}

func TestNewServerWithOptions(t *testing.T) {
	fb := NewFramebuffer(100, 100)

	// Test with no options
	s1 := NewServer(fb, nil)
	assert.NotNil(t, s1)
	assert.Empty(t, s1.password)
	assert.Nil(t, s1.logger)
	assert.Equal(t, defaultMaxConnections, s1.maxConns)

	// Test with password option
	s2 := NewServer(fb, nil, WithPassword("secret"))
	assert.Equal(t, "secret", s2.password)

	// Test with multiple options
	handler := &mockInputHandler{}
	s3 := NewServer(fb, handler, WithPassword("pass"), WithMaxConnections(5))
	assert.Equal(t, "pass", s3.password)
	assert.NotNil(t, s3.inputHandler)
	assert.Equal(t, 5, s3.maxConns)
}

func TestServerMaxConnections(t *testing.T) {
	fb := NewFramebuffer(100, 100)
	server := NewServer(fb, nil, WithMaxConnections(2))

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	addr := listener.Addr().String()

	// Connect first two clients - should succeed
	client1 := newVNCClient(t, addr)
	defer client1.close()
	client1.doHandshake("")

	client2 := newVNCClient(t, addr)
	defer client2.close()
	client2.doHandshake("")

	// Third connection should be rejected - server closes it immediately
	conn3, err := net.DialTimeout("tcp", addr, 2*time.Second)
	require.NoError(t, err)
	defer conn3.Close()

	// Try to read from the rejected connection - it should be closed by the server
	conn3.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 12)
	_, err = io.ReadFull(conn3, buf)
	assert.Error(t, err, "third connection should fail since max connections is 2")

	// First two clients should still work
	client1.sendFramebufferUpdateRequest(false, 0, 0, 1, 1)
	numRects, err := client1.readFramebufferUpdate()
	require.NoError(t, err)
	assert.Equal(t, uint16(1), numRects)
}

func TestServerMaxConnectionsZeroIsIgnored(t *testing.T) {
	fb := NewFramebuffer(100, 100)
	// WithMaxConnections(0) should be ignored, keeping default
	server := NewServer(fb, nil, WithMaxConnections(0))
	assert.Equal(t, defaultMaxConnections, server.maxConns)
}

func TestAuthInvalidSecurityTypeNone(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	errCh := make(chan error, 1)
	go func() {
		rw := &readWriteFlusher{
			r: bufio.NewReader(server),
			w: bufio.NewWriter(server),
		}
		errCh <- performAuth(rw, "")
	}()

	// Client: read security types
	secTypes := make([]byte, 2)
	_, err := io.ReadFull(client, secTypes)
	require.NoError(t, err)

	// Client: choose WRONG security type (VNCAuth instead of None)
	_, err = client.Write([]byte{secTypeVNCAuth})
	require.NoError(t, err)

	// Server should return an error
	authErr := <-errCh
	assert.Error(t, authErr)
	assert.Contains(t, authErr.Error(), "unsupported security type")
}

func TestAuthInvalidSecurityTypeVNCAuth(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	errCh := make(chan error, 1)
	go func() {
		rw := &readWriteFlusher{
			r: bufio.NewReader(server),
			w: bufio.NewWriter(server),
		}
		errCh <- performAuth(rw, "password")
	}()

	// Client: read security types
	secTypes := make([]byte, 2)
	_, err := io.ReadFull(client, secTypes)
	require.NoError(t, err)

	// Client: choose WRONG security type (None instead of VNCAuth)
	_, err = client.Write([]byte{secTypeNone})
	require.NoError(t, err)

	// Server should return an error
	authErr := <-errCh
	assert.Error(t, authErr)
	assert.Contains(t, authErr.Error(), "unsupported security type")
}
