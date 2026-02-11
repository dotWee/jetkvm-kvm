package vnc

import (
	"encoding/binary"
	"io"
)

// RFB protocol constants
const (
	rfbProtocolVersion = "RFB 003.008\n"
)

// Security types
const (
	secTypeInvalid = 0
	secTypeNone    = 1
	secTypeVNCAuth = 2
)

// Client-to-server message types
const (
	msgSetPixelFormat           = 0
	msgSetEncodings             = 2
	msgFramebufferUpdateRequest = 3
	msgKeyEvent                 = 4
	msgPointerEvent             = 5
	msgClientCutText            = 6
)

// Server-to-client message types
const (
	msgFramebufferUpdate = 0
)

// Encoding types
const (
	encodingRaw = 0
)

// maxClientCutTextLen limits the clipboard text size to prevent OOM from malicious clients.
const maxClientCutTextLen = 10 * 1024 * 1024 // 10 MB

// maxEncodings limits the number of encoding types a client can request.
const maxEncodings = 256

// PixelFormat describes the pixel format for the RFB protocol.
type PixelFormat struct {
	BitsPerPixel  uint8
	Depth         uint8
	BigEndianFlag uint8
	TrueColorFlag uint8
	RedMax        uint16
	GreenMax      uint16
	BlueMax       uint16
	RedShift      uint8
	GreenShift    uint8
	BlueShift     uint8
}

// defaultPixelFormat returns the default 32-bit BGRA pixel format.
func defaultPixelFormat() PixelFormat {
	return PixelFormat{
		BitsPerPixel:  32,
		Depth:         24,
		BigEndianFlag: 0,
		TrueColorFlag: 1,
		RedMax:        255,
		GreenMax:      255,
		BlueMax:       255,
		RedShift:      16,
		GreenShift:    8,
		BlueShift:     0,
	}
}

// write serializes the pixel format to the writer (16 bytes total).
func (pf PixelFormat) write(w io.Writer) error {
	buf := make([]byte, 16)
	buf[0] = pf.BitsPerPixel
	buf[1] = pf.Depth
	buf[2] = pf.BigEndianFlag
	buf[3] = pf.TrueColorFlag
	binary.BigEndian.PutUint16(buf[4:6], pf.RedMax)
	binary.BigEndian.PutUint16(buf[6:8], pf.GreenMax)
	binary.BigEndian.PutUint16(buf[8:10], pf.BlueMax)
	buf[10] = pf.RedShift
	buf[11] = pf.GreenShift
	buf[12] = pf.BlueShift
	// bytes 13-15 are padding
	_, err := w.Write(buf)
	return err
}

// readPixelFormat reads a pixel format from the reader (16 bytes).
func readPixelFormat(r io.Reader) (PixelFormat, error) {
	buf := make([]byte, 16)
	if _, err := io.ReadFull(r, buf); err != nil {
		return PixelFormat{}, err
	}
	return PixelFormat{
		BitsPerPixel:  buf[0],
		Depth:         buf[1],
		BigEndianFlag: buf[2],
		TrueColorFlag: buf[3],
		RedMax:        binary.BigEndian.Uint16(buf[4:6]),
		GreenMax:      binary.BigEndian.Uint16(buf[6:8]),
		BlueMax:       binary.BigEndian.Uint16(buf[8:10]),
		RedShift:      buf[10],
		GreenShift:    buf[11],
		BlueShift:     buf[12],
	}, nil
}

// FramebufferUpdateRequest represents a client request for framebuffer data.
type FramebufferUpdateRequest struct {
	Incremental uint8
	X           uint16
	Y           uint16
	Width       uint16
	Height      uint16
}

// readFramebufferUpdateRequest reads a framebuffer update request from the reader.
func readFramebufferUpdateRequest(r io.Reader) (FramebufferUpdateRequest, error) {
	buf := make([]byte, 9)
	if _, err := io.ReadFull(r, buf); err != nil {
		return FramebufferUpdateRequest{}, err
	}
	return FramebufferUpdateRequest{
		Incremental: buf[0],
		X:           binary.BigEndian.Uint16(buf[1:3]),
		Y:           binary.BigEndian.Uint16(buf[3:5]),
		Width:       binary.BigEndian.Uint16(buf[5:7]),
		Height:      binary.BigEndian.Uint16(buf[7:9]),
	}, nil
}

// KeyEvent represents a VNC key event from the client.
type KeyEvent struct {
	DownFlag uint8
	Key      uint32
}

// readKeyEvent reads a key event message from the reader.
func readKeyEvent(r io.Reader) (KeyEvent, error) {
	buf := make([]byte, 7)
	if _, err := io.ReadFull(r, buf); err != nil {
		return KeyEvent{}, err
	}
	return KeyEvent{
		DownFlag: buf[0],
		Key:      binary.BigEndian.Uint32(buf[3:7]),
	}, nil
}

// PointerEvent represents a VNC pointer (mouse) event from the client.
type PointerEvent struct {
	ButtonMask uint8
	X          uint16
	Y          uint16
}

// readPointerEvent reads a pointer event message from the reader.
func readPointerEvent(r io.Reader) (PointerEvent, error) {
	buf := make([]byte, 5)
	if _, err := io.ReadFull(r, buf); err != nil {
		return PointerEvent{}, err
	}
	return PointerEvent{
		ButtonMask: buf[0],
		X:          binary.BigEndian.Uint16(buf[1:3]),
		Y:          binary.BigEndian.Uint16(buf[3:5]),
	}, nil
}

// writeFramebufferUpdateHeader writes the header for a framebuffer update message.
func writeFramebufferUpdateHeader(w io.Writer, numRects uint16) error {
	buf := make([]byte, 4)
	buf[0] = msgFramebufferUpdate
	buf[1] = 0 // padding
	binary.BigEndian.PutUint16(buf[2:4], numRects)
	_, err := w.Write(buf)
	return err
}

// writeRawRect writes a raw-encoded rectangle to the writer.
func writeRawRect(w io.Writer, x, y, width, height uint16, data []byte) error {
	header := make([]byte, 12)
	binary.BigEndian.PutUint16(header[0:2], x)
	binary.BigEndian.PutUint16(header[2:4], y)
	binary.BigEndian.PutUint16(header[4:6], width)
	binary.BigEndian.PutUint16(header[6:8], height)
	binary.BigEndian.PutUint32(header[8:12], encodingRaw)
	if _, err := w.Write(header); err != nil {
		return err
	}
	_, err := w.Write(data)
	return err
}
