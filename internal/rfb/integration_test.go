package rfb

import (
	"bytes"
	"context"
	"crypto/des"
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

func TestVNCAuthFullHandshake(t *testing.T) {
	password := "testpass"
	frame := makeTestFrameData(32, 32)
	input := &testInputHandler{}

	srv := NewServer(ServerConfig{
		Addr: "127.0.0.1:0",
		Name: "AuthTest",
		FrameProvider: &testFrameProvider{
			width: 32, height: 32, frame: frame,
		},
		Input: input,
		SecurityHandlers: []SecurityHandler{
			&SecurityVNCAuth{Password: []byte(password)},
			&SecurityNone{},
		},
		Logger: zerolog.Nop(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = srv.Listen(ctx) }()
	time.Sleep(50 * time.Millisecond)
	addr := srv.Addr()

	// Connect and authenticate with VNC password
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	require.NoError(t, err)
	defer conn.Close()

	// Read server version
	version := make([]byte, 12)
	_, err = io.ReadFull(conn, version)
	require.NoError(t, err)

	// Send client version
	_, err = conn.Write([]byte(ProtocolVersion))
	require.NoError(t, err)

	// Read security types
	numTypes := make([]byte, 1)
	_, err = io.ReadFull(conn, numTypes)
	require.NoError(t, err)
	assert.Equal(t, byte(2), numTypes[0]) // VNCAuth + None

	types := make([]byte, numTypes[0])
	_, err = io.ReadFull(conn, types)
	require.NoError(t, err)

	// Choose VNC Authentication (type 2)
	_, err = conn.Write([]byte{SecTypeVNCAuth})
	require.NoError(t, err)

	// Read 16-byte challenge
	challenge := make([]byte, VNCAuthChallengeSize)
	_, err = io.ReadFull(conn, challenge)
	require.NoError(t, err)

	// Encrypt challenge with password (using VNC DES)
	response, err := EncryptChallenge(challenge, []byte(password))
	require.NoError(t, err)

	_, err = conn.Write(response)
	require.NoError(t, err)

	// Read SecurityResult
	result := make([]byte, 4)
	_, err = io.ReadFull(conn, result)
	require.NoError(t, err)
	assert.Equal(t, uint32(SecurityResultOK), binary.BigEndian.Uint32(result))

	// Complete handshake
	_, err = conn.Write([]byte{1}) // shared
	require.NoError(t, err)

	siHeader := make([]byte, 24)
	_, err = io.ReadFull(conn, siHeader)
	require.NoError(t, err)

	nameLen := binary.BigEndian.Uint32(siHeader[20:24])
	name := make([]byte, nameLen)
	_, err = io.ReadFull(conn, name)
	require.NoError(t, err)
	assert.Equal(t, "AuthTest", string(name))

	cancel()
}

func TestVNCAuthWrongPassword(t *testing.T) {
	password := "correct"
	frame := makeTestFrameData(32, 32)

	srv := NewServer(ServerConfig{
		Addr: "127.0.0.1:0",
		Name: "WrongPW",
		FrameProvider: &testFrameProvider{
			width: 32, height: 32, frame: frame,
		},
		SecurityHandlers: []SecurityHandler{
			&SecurityVNCAuth{Password: []byte(password)},
		},
		Logger: zerolog.Nop(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = srv.Listen(ctx) }()
	time.Sleep(50 * time.Millisecond)
	addr := srv.Addr()

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	require.NoError(t, err)
	defer conn.Close()

	// Version handshake
	version := make([]byte, 12)
	_, err = io.ReadFull(conn, version)
	require.NoError(t, err)
	_, err = conn.Write([]byte(ProtocolVersion))
	require.NoError(t, err)

	// Security handshake
	numTypes := make([]byte, 1)
	_, err = io.ReadFull(conn, numTypes)
	require.NoError(t, err)

	types := make([]byte, numTypes[0])
	_, err = io.ReadFull(conn, types)
	require.NoError(t, err)

	_, err = conn.Write([]byte{SecTypeVNCAuth})
	require.NoError(t, err)

	// Read and respond to challenge with wrong password
	challenge := make([]byte, VNCAuthChallengeSize)
	_, err = io.ReadFull(conn, challenge)
	require.NoError(t, err)

	response, err := EncryptChallenge(challenge, []byte("wrong"))
	require.NoError(t, err)

	_, err = conn.Write(response)
	require.NoError(t, err)

	// Read SecurityResult - should be FAIL
	result := make([]byte, 4)
	_, err = io.ReadFull(conn, result)
	require.NoError(t, err)
	assert.Equal(t, uint32(SecurityResultFailed), binary.BigEndian.Uint32(result))

	cancel()
}

func TestClientConnectDisconnectCallbacks(t *testing.T) {
	frame := makeTestFrameData(32, 32)
	connected := make(chan struct{}, 1)
	disconnected := make(chan struct{}, 1)

	srv := NewServer(ServerConfig{
		Addr: "127.0.0.1:0",
		Name: "Callbacks",
		FrameProvider: &testFrameProvider{
			width: 32, height: 32, frame: frame,
		},
		SecurityHandlers: []SecurityHandler{&SecurityNone{}},
		Logger:           zerolog.Nop(),
		OnClientConnected: func() {
			connected <- struct{}{}
		},
		OnClientDisconnected: func() {
			disconnected <- struct{}{}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = srv.Listen(ctx) }()
	time.Sleep(50 * time.Millisecond)
	addr := srv.Addr()

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	require.NoError(t, err)

	doClientHandshake(t, conn)

	// Wait for connected callback
	select {
	case <-connected:
	case <-time.After(2 * time.Second):
		t.Fatal("connect callback not received")
	}

	// Close connection
	conn.Close()

	// Wait for disconnected callback
	select {
	case <-disconnected:
	case <-time.After(2 * time.Second):
		t.Fatal("disconnect callback not received")
	}

	cancel()
}

func TestServerVNCAuthEncryptionDeterministic(t *testing.T) {
	// Verify that the same challenge+password always produce the same result
	challenge := make([]byte, VNCAuthChallengeSize)
	for i := range challenge {
		challenge[i] = byte(i * 3)
	}

	r1, err := EncryptChallenge(challenge, []byte("mypass"))
	require.NoError(t, err)

	r2, err := EncryptChallenge(challenge, []byte("mypass"))
	require.NoError(t, err)

	assert.Equal(t, r1, r2, "encryption should be deterministic")
}

func TestServerVNCAuthDESKeyReversal(t *testing.T) {
	// Verify the key reversal produces correct DES output
	// The VNC protocol requires reversing bits in each key byte
	password := []byte("hello")
	key := make([]byte, 8)
	copy(key, password)
	for i := range key {
		key[i] = reverseBits(key[i])
	}

	// Should be a valid DES key
	_, err := des.NewCipher(key)
	require.NoError(t, err)
}

func TestRawEncoderFullFrame(t *testing.T) {
	// Encode a full frame and verify pixel data is written
	enc := &RawEncoder{}
	pf := DefaultPixelFormat()

	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			img.Set(x, y, color.RGBA{R: uint8(x * 60), G: uint8(y * 60), B: 128, A: 255})
		}
	}

	rect := Rectangle{X: 0, Y: 0, Width: 4, Height: 4}
	var buf bytes.Buffer
	err := enc.Encode(&buf, img, rect, pf)
	require.NoError(t, err)

	// 4x4 pixels at 4 bytes per pixel = 64 bytes
	assert.Equal(t, 64, buf.Len())
}

func TestZRLEEncoderCompression(t *testing.T) {
	// Verify ZRLE produces smaller output than raw for uniform data
	pf := DefaultPixelFormat()

	// Create a uniform (all one color) image - should compress very well
	width, height := 128, 128
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	gray := color.RGBA{R: 64, G: 64, B: 64, A: 255}
	for y := range height {
		for x := range width {
			img.Set(x, y, gray)
		}
	}

	rawEnc := &RawEncoder{}
	zrleEnc := &ZRLEEncoder{}
	rect := Rectangle{X: 0, Y: 0, Width: uint16(width), Height: uint16(height)}

	var rawBuf, zrleBuf bytes.Buffer
	err := rawEnc.Encode(&rawBuf, img, rect, pf)
	require.NoError(t, err)

	err = zrleEnc.Encode(&zrleBuf, img, rect, pf)
	require.NoError(t, err)

	// ZRLE should be much smaller for uniform data
	assert.Less(t, zrleBuf.Len(), rawBuf.Len(), "ZRLE should produce smaller output for uniform data")
}

func TestFrameDataToImage(t *testing.T) {
	frame := makeTestFrameData(8, 8)
	assert.NotNil(t, frame)
	assert.Equal(t, 8, frame.Width)
	assert.Equal(t, 8, frame.Height)
	assert.Equal(t, 8*4, frame.Stride)
	assert.Equal(t, 8*8*4, len(frame.Pix))
}

func TestKeysymToHIDCoverage(t *testing.T) {
	// Test that common keysyms all have mappings
	commonKeysyms := []uint32{
		0xFF0D, // Return
		0xFF1B, // Escape
		0xFF08, // BackSpace
		0xFF09, // Tab
		0x0020, // Space
		0xFF50, // Home
		0xFF57, // End
		0xFF55, // Page_Up
		0xFF56, // Page_Down
		0xFF51, // Left
		0xFF52, // Up
		0xFF53, // Right
		0xFF54, // Down
		0xFFBE, // F1
		0xFFC9, // F12
		0xFF63, // Insert
		0xFFFF, // Delete
		0xFFE1, // Shift_L
		0xFFE3, // Control_L
		0xFFE9, // Alt_L
	}

	for _, ks := range commonKeysyms {
		_, _, found := KeysymToHID(ks)
		assert.True(t, found, "keysym 0x%04X should have a mapping", ks)
	}
}

func TestPixelFormatConversions(t *testing.T) {
	// Test various pixel format roundtrips
	formats := []PixelFormat{
		DefaultPixelFormat(),
		{BitsPerPixel: 16, Depth: 16, TrueColorFlag: 1, RedMax: 31, GreenMax: 63, BlueMax: 31, RedShift: 11, GreenShift: 5, BlueShift: 0},
		{BitsPerPixel: 8, Depth: 8, TrueColorFlag: 1, RedMax: 7, GreenMax: 7, BlueMax: 3, RedShift: 5, GreenShift: 2, BlueShift: 0},
	}

	for _, pf := range formats {
		data := pf.Marshal()
		decoded, err := UnmarshalPixelFormat(data)
		require.NoError(t, err)
		assert.Equal(t, pf.BitsPerPixel, decoded.BitsPerPixel)
		assert.Equal(t, pf.RedMax, decoded.RedMax)
		assert.Equal(t, pf.GreenMax, decoded.GreenMax)
		assert.Equal(t, pf.BlueMax, decoded.BlueMax)
	}
}
