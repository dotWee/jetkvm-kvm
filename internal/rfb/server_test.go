package rfb

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"io"
	"net"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testFrameProvider implements FrameProvider for testing.
type testFrameProvider struct {
	width, height uint16
	frame         *FrameData
}

func (p *testFrameProvider) GetFrame() *FrameData {
	return p.frame
}

func (p *testFrameProvider) GetSize() (uint16, uint16) {
	return p.width, p.height
}

// testInputHandler records input events for testing.
type testInputHandler struct {
	keyEvents     []KeyEventMsg
	pointerEvents []PointerEventMsg
}

func (h *testInputHandler) KeyEvent(keysym uint32, down bool) {
	h.keyEvents = append(h.keyEvents, KeyEventMsg{Key: keysym, DownFlag: down})
}

func (h *testInputHandler) PointerEvent(buttonMask uint8, x, y uint16) {
	h.pointerEvents = append(h.pointerEvents, PointerEventMsg{ButtonMask: buttonMask, X: x, Y: y})
}

func makeTestFrameData(width, height int) *FrameData {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	return &FrameData{
		Pix:    img.Pix,
		Stride: img.Stride,
		Width:  width,
		Height: height,
	}
}

func TestServerNewServer(t *testing.T) {
	srv := NewServer(ServerConfig{
		Addr:          ":0",
		Name:          "Test",
		FrameProvider: &testFrameProvider{width: 640, height: 480},
		Logger:        zerolog.Nop(),
	})
	assert.NotNil(t, srv)
	// Default to SecurityNone
	assert.Len(t, srv.config.SecurityHandlers, 1)
	assert.Equal(t, uint8(SecTypeNone), srv.config.SecurityHandlers[0].Type())
}

func TestServerListenAndConnect(t *testing.T) {
	frame := makeTestFrameData(64, 48)
	input := &testInputHandler{}

	srv := NewServer(ServerConfig{
		Addr: "127.0.0.1:0",
		Name: "TestKVM",
		FrameProvider: &testFrameProvider{
			width:  64,
			height: 48,
			frame:  frame,
		},
		Input:            input,
		SecurityHandlers: []SecurityHandler{&SecurityNone{}},
		Logger:           zerolog.Nop(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start server
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Listen(ctx)
	}()

	// Wait for listener to be ready
	time.Sleep(50 * time.Millisecond)
	addr := srv.Addr()
	require.NotEmpty(t, addr)

	// Connect as a VNC client
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	require.NoError(t, err)
	defer conn.Close()

	// 1. Read server version
	version := make([]byte, 12)
	_, err = io.ReadFull(conn, version)
	require.NoError(t, err)
	assert.Equal(t, ProtocolVersion, string(version))

	// 2. Send client version
	_, err = conn.Write([]byte(ProtocolVersion))
	require.NoError(t, err)

	// 3. Read security types
	numTypes := make([]byte, 1)
	_, err = io.ReadFull(conn, numTypes)
	require.NoError(t, err)
	assert.Equal(t, byte(1), numTypes[0])

	// Read type list
	types := make([]byte, numTypes[0])
	_, err = io.ReadFull(conn, types)
	require.NoError(t, err)
	assert.Equal(t, byte(SecTypeNone), types[0])

	// 4. Choose SecurityNone
	_, err = conn.Write([]byte{SecTypeNone})
	require.NoError(t, err)

	// 5. Read SecurityResult
	result := make([]byte, 4)
	_, err = io.ReadFull(conn, result)
	require.NoError(t, err)
	assert.Equal(t, uint32(SecurityResultOK), binary.BigEndian.Uint32(result))

	// 6. Send ClientInit (shared=1)
	_, err = conn.Write([]byte{1})
	require.NoError(t, err)

	// 7. Read ServerInit
	serverInitHeader := make([]byte, 24)
	_, err = io.ReadFull(conn, serverInitHeader)
	require.NoError(t, err)

	siWidth := binary.BigEndian.Uint16(serverInitHeader[0:2])
	siHeight := binary.BigEndian.Uint16(serverInitHeader[2:4])
	assert.Equal(t, uint16(64), siWidth)
	assert.Equal(t, uint16(48), siHeight)

	nameLen := binary.BigEndian.Uint32(serverInitHeader[20:24])
	name := make([]byte, nameLen)
	_, err = io.ReadFull(conn, name)
	require.NoError(t, err)
	assert.Equal(t, "TestKVM", string(name))

	// 8. Send a FramebufferUpdateRequest
	fbReq := make([]byte, 10)
	fbReq[0] = MsgFramebufferUpdateRequest
	fbReq[1] = 0 // non-incremental
	binary.BigEndian.PutUint16(fbReq[2:4], 0)
	binary.BigEndian.PutUint16(fbReq[4:6], 0)
	binary.BigEndian.PutUint16(fbReq[6:8], 64)
	binary.BigEndian.PutUint16(fbReq[8:10], 48)
	_, err = conn.Write(fbReq)
	require.NoError(t, err)

	// 9. Read FramebufferUpdate response
	fbUpdateHeader := make([]byte, 4)
	_, err = io.ReadFull(conn, fbUpdateHeader)
	require.NoError(t, err)
	assert.Equal(t, byte(MsgFramebufferUpdate), fbUpdateHeader[0])
	numRects := binary.BigEndian.Uint16(fbUpdateHeader[2:4])
	assert.Equal(t, uint16(1), numRects)

	// Read rectangle header
	rectHeader := make([]byte, 12)
	_, err = io.ReadFull(conn, rectHeader)
	require.NoError(t, err)
	rectW := binary.BigEndian.Uint16(rectHeader[4:6])
	rectH := binary.BigEndian.Uint16(rectHeader[6:8])
	assert.Equal(t, uint16(64), rectW)
	assert.Equal(t, uint16(48), rectH)

	// Read raw pixel data (64 * 48 * 4 bytes for 32bpp)
	pixelData := make([]byte, 64*48*4)
	_, err = io.ReadFull(conn, pixelData)
	require.NoError(t, err)
	assert.Len(t, pixelData, 64*48*4)

	// 10. Send KeyEvent
	keyBuf := make([]byte, 8)
	keyBuf[0] = MsgKeyEvent
	keyBuf[1] = 1                                   // down
	binary.BigEndian.PutUint32(keyBuf[4:8], 0xFF0D) // Return key
	_, err = conn.Write(keyBuf)
	require.NoError(t, err)

	// 11. Send PointerEvent
	ptrBuf := make([]byte, 6)
	ptrBuf[0] = MsgPointerEvent
	ptrBuf[1] = 0x01 // left button
	binary.BigEndian.PutUint16(ptrBuf[2:4], 32)
	binary.BigEndian.PutUint16(ptrBuf[4:6], 24)
	_, err = conn.Write(ptrBuf)
	require.NoError(t, err)

	// Give server time to process events
	time.Sleep(100 * time.Millisecond)

	// Verify input events were received
	assert.GreaterOrEqual(t, len(input.keyEvents), 1)
	assert.Equal(t, uint32(0xFF0D), input.keyEvents[0].Key)
	assert.True(t, input.keyEvents[0].DownFlag)

	assert.GreaterOrEqual(t, len(input.pointerEvents), 1)
	assert.Equal(t, uint8(0x01), input.pointerEvents[0].ButtonMask)
	assert.Equal(t, uint16(32), input.pointerEvents[0].X)
	assert.Equal(t, uint16(24), input.pointerEvents[0].Y)

	// Verify connected client count
	assert.Equal(t, 1, srv.ConnectedClients())

	// 12. Shutdown
	cancel()
	time.Sleep(100 * time.Millisecond)
}

func TestServerNoFrame(t *testing.T) {
	// Server that returns nil frame
	srv := NewServer(ServerConfig{
		Addr: "127.0.0.1:0",
		Name: "NoFrame",
		FrameProvider: &testFrameProvider{
			width:  640,
			height: 480,
			frame:  nil, // no frame
		},
		SecurityHandlers: []SecurityHandler{&SecurityNone{}},
		Logger:           zerolog.Nop(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = srv.Listen(ctx) }()
	time.Sleep(50 * time.Millisecond)
	addr := srv.Addr()
	require.NotEmpty(t, addr)

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	require.NoError(t, err)
	defer conn.Close()

	// Do handshake
	doClientHandshake(t, conn)

	// Send FramebufferUpdateRequest
	fbReq := make([]byte, 10)
	fbReq[0] = MsgFramebufferUpdateRequest
	binary.BigEndian.PutUint16(fbReq[6:8], 640)
	binary.BigEndian.PutUint16(fbReq[8:10], 480)
	_, err = conn.Write(fbReq)
	require.NoError(t, err)

	// Read FramebufferUpdate with 0 rectangles
	fbUpdate := make([]byte, 4)
	_, err = io.ReadFull(conn, fbUpdate)
	require.NoError(t, err)
	numRects := binary.BigEndian.Uint16(fbUpdate[2:4])
	assert.Equal(t, uint16(0), numRects)

	cancel()
}

func TestServerMultipleClients(t *testing.T) {
	frame := makeTestFrameData(32, 32)
	srv := NewServer(ServerConfig{
		Addr: "127.0.0.1:0",
		Name: "Multi",
		FrameProvider: &testFrameProvider{
			width: 32, height: 32, frame: frame,
		},
		SecurityHandlers: []SecurityHandler{&SecurityNone{}},
		Logger:           zerolog.Nop(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = srv.Listen(ctx) }()
	time.Sleep(50 * time.Millisecond)
	addr := srv.Addr()

	// Connect two clients
	conn1, err := net.DialTimeout("tcp", addr, 2*time.Second)
	require.NoError(t, err)
	defer conn1.Close()
	doClientHandshake(t, conn1)

	conn2, err := net.DialTimeout("tcp", addr, 2*time.Second)
	require.NoError(t, err)
	defer conn2.Close()
	doClientHandshake(t, conn2)

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 2, srv.ConnectedClients())

	cancel()
}

func TestServerGracefulShutdown(t *testing.T) {
	srv := NewServer(ServerConfig{
		Addr: "127.0.0.1:0",
		Name: "Shutdown",
		FrameProvider: &testFrameProvider{
			width: 32, height: 32,
			frame: makeTestFrameData(32, 32),
		},
		SecurityHandlers: []SecurityHandler{&SecurityNone{}},
		Logger:           zerolog.Nop(),
	})

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Listen(ctx)
	}()

	time.Sleep(50 * time.Millisecond)
	addr := srv.Addr()
	require.NotEmpty(t, addr)

	cancel()

	select {
	case err := <-errCh:
		assert.NoError(t, err) // Should return nil on clean shutdown
	case <-time.After(2 * time.Second):
		t.Fatal("server did not shut down in time")
	}
}

// doClientHandshake performs a minimal VNC client handshake for testing.
func doClientHandshake(t *testing.T, conn net.Conn) {
	t.Helper()

	// Read server version
	version := make([]byte, 12)
	_, err := io.ReadFull(conn, version)
	require.NoError(t, err)

	// Send client version
	_, err = conn.Write([]byte(ProtocolVersion))
	require.NoError(t, err)

	// Read security types
	numTypes := make([]byte, 1)
	_, err = io.ReadFull(conn, numTypes)
	require.NoError(t, err)

	types := make([]byte, numTypes[0])
	_, err = io.ReadFull(conn, types)
	require.NoError(t, err)

	// Choose first type
	_, err = conn.Write([]byte{types[0]})
	require.NoError(t, err)

	// Read SecurityResult
	result := make([]byte, 4)
	_, err = io.ReadFull(conn, result)
	require.NoError(t, err)

	// Send ClientInit
	_, err = conn.Write([]byte{1})
	require.NoError(t, err)

	// Read ServerInit header + name
	siHeader := make([]byte, 24)
	_, err = io.ReadFull(conn, siHeader)
	require.NoError(t, err)

	nameLen := binary.BigEndian.Uint32(siHeader[20:24])
	if nameLen > 0 {
		name := make([]byte, nameLen)
		_, err = io.ReadFull(conn, name)
		require.NoError(t, err)
	}
}

func TestVersionNegotiation(t *testing.T) {
	frame := makeTestFrameData(32, 32)
	srv := NewServer(ServerConfig{
		Addr: "127.0.0.1:0",
		Name: "VersionTest",
		FrameProvider: &testFrameProvider{
			width: 32, height: 32, frame: frame,
		},
		SecurityHandlers: []SecurityHandler{&SecurityNone{}},
		Logger:           zerolog.Nop(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = srv.Listen(ctx) }()
	time.Sleep(50 * time.Millisecond)
	addr := srv.Addr()

	// Test with older version string
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	require.NoError(t, err)
	defer conn.Close()

	// Read server version
	version := make([]byte, 12)
	_, err = io.ReadFull(conn, version)
	require.NoError(t, err)
	assert.Equal(t, "RFB 003.008\n", string(version))

	// Send older version (3.3) - should still be accepted
	_, err = conn.Write([]byte("RFB 003.003\n"))
	require.NoError(t, err)

	// Should continue with security negotiation
	// (RFB 3.3 has different security flow, but our server accepts any 3.x)
	// Just verify the connection wasn't immediately closed
	buf := make([]byte, 1)
	conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, err = conn.Read(buf)
	assert.NoError(t, err, "server should continue after receiving 3.3 version")

	cancel()
}

func TestSetPixelFormatDuringSession(t *testing.T) {
	frame := makeTestFrameData(32, 32)
	srv := NewServer(ServerConfig{
		Addr: "127.0.0.1:0",
		Name: "PFTest",
		FrameProvider: &testFrameProvider{
			width: 32, height: 32, frame: frame,
		},
		SecurityHandlers: []SecurityHandler{&SecurityNone{}},
		Logger:           zerolog.Nop(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = srv.Listen(ctx) }()
	time.Sleep(50 * time.Millisecond)
	addr := srv.Addr()

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	require.NoError(t, err)
	defer conn.Close()

	doClientHandshake(t, conn)

	// Send SetPixelFormat for RGB565
	pf := PixelFormat{
		BitsPerPixel: 16, Depth: 16, BigEndianFlag: 0, TrueColorFlag: 1,
		RedMax: 31, GreenMax: 63, BlueMax: 31,
		RedShift: 11, GreenShift: 5, BlueShift: 0,
	}

	spfBuf := make([]byte, 1+3+16) // type + padding + pixel format
	spfBuf[0] = MsgSetPixelFormat
	copy(spfBuf[4:], pf.Marshal())
	_, err = conn.Write(spfBuf)
	require.NoError(t, err)

	// Request framebuffer update in the new pixel format
	fbReq := make([]byte, 10)
	fbReq[0] = MsgFramebufferUpdateRequest
	binary.BigEndian.PutUint16(fbReq[6:8], 32)
	binary.BigEndian.PutUint16(fbReq[8:10], 32)
	_, err = conn.Write(fbReq)
	require.NoError(t, err)

	// Read response - should be 16bpp (32*32*2 = 2048 bytes of pixel data)
	fbUpdate := make([]byte, 4)
	_, err = io.ReadFull(conn, fbUpdate)
	require.NoError(t, err)

	rectHeader := make([]byte, 12)
	_, err = io.ReadFull(conn, rectHeader)
	require.NoError(t, err)

	// Read pixel data - should be 2048 bytes for 16bpp
	pixelData := make([]byte, 32*32*2)
	_, err = io.ReadFull(conn, pixelData)
	require.NoError(t, err)

	cancel()
}

func TestSetEncodings(t *testing.T) {
	frame := makeTestFrameData(32, 32)
	srv := NewServer(ServerConfig{
		Addr: "127.0.0.1:0",
		Name: "EncTest",
		FrameProvider: &testFrameProvider{
			width: 32, height: 32, frame: frame,
		},
		SecurityHandlers: []SecurityHandler{&SecurityNone{}},
		Logger:           zerolog.Nop(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = srv.Listen(ctx) }()
	time.Sleep(50 * time.Millisecond)
	addr := srv.Addr()

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	require.NoError(t, err)
	defer conn.Close()

	doClientHandshake(t, conn)

	// Send SetEncodings requesting ZRLE, Raw
	var encBuf bytes.Buffer
	encBuf.WriteByte(MsgSetEncodings)
	encBuf.WriteByte(0) // padding
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, 2) // 2 encodings
	encBuf.Write(b)
	enc := make([]byte, 4)
	binary.BigEndian.PutUint32(enc, uint32(EncodingZRLE))
	encBuf.Write(enc)
	binary.BigEndian.PutUint32(enc, uint32(EncodingRaw))
	encBuf.Write(enc)
	_, err = conn.Write(encBuf.Bytes())
	require.NoError(t, err)

	// Request framebuffer update
	fbReq := make([]byte, 10)
	fbReq[0] = MsgFramebufferUpdateRequest
	binary.BigEndian.PutUint16(fbReq[6:8], 32)
	binary.BigEndian.PutUint16(fbReq[8:10], 32)
	_, err = conn.Write(fbReq)
	require.NoError(t, err)

	// Read response - should be ZRLE encoded
	fbUpdate := make([]byte, 4)
	_, err = io.ReadFull(conn, fbUpdate)
	require.NoError(t, err)

	rectHeader := make([]byte, 12)
	_, err = io.ReadFull(conn, rectHeader)
	require.NoError(t, err)

	encType := int32(binary.BigEndian.Uint32(rectHeader[8:12]))
	assert.Equal(t, int32(EncodingZRLE), encType)

	cancel()
}
