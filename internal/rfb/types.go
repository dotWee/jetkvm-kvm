// Package rfb implements the Remote Framebuffer (RFB) protocol used by VNC,
// as specified in RFC 6143.
package rfb

import (
	"encoding/binary"
	"fmt"
	"io"
)

// ProtocolVersion is the RFB protocol version string.
const ProtocolVersion = "RFB 003.008\n"

// Security types supported by the server.
const (
	SecTypeInvalid = 0
	SecTypeNone    = 1
	SecTypeVNCAuth = 2
)

// Client-to-server message types.
const (
	MsgSetPixelFormat           = 0
	MsgSetEncodings             = 2
	MsgFramebufferUpdateRequest = 3
	MsgKeyEvent                 = 4
	MsgPointerEvent             = 5
	MsgClientCutText            = 6
)

// Server-to-client message types.
const (
	MsgFramebufferUpdate   = 0
	MsgSetColourMapEntries = 1
	MsgBell                = 2
	MsgServerCutText       = 3
)

// Encoding types.
const (
	EncodingRaw         = 0
	EncodingCopyRect    = 1
	EncodingRRE         = 2
	EncodingHextile     = 5
	EncodingTRLE        = 15
	EncodingZRLE        = 16
	EncodingCursor      = -239
	EncodingDesktopSize = -223
)

// VNC authentication challenge size.
const VNCAuthChallengeSize = 16

// PixelFormat describes the pixel format used for framebuffer data.
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

// DefaultPixelFormat returns the standard 32-bit RGBA pixel format.
func DefaultPixelFormat() PixelFormat {
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

// Marshal writes the pixel format in its 16-byte wire format (4 bytes of
// pixel format data + 3 padding bytes at offsets 4-6, totalling the standard
// RFB layout). The full wire representation is 16 bytes.
func (pf PixelFormat) Marshal() []byte {
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
	return buf
}

// UnmarshalPixelFormat reads a PixelFormat from 16 bytes of wire data.
func UnmarshalPixelFormat(data []byte) (PixelFormat, error) {
	if len(data) < 16 {
		return PixelFormat{}, fmt.Errorf("pixel format data too short: %d bytes", len(data))
	}
	return PixelFormat{
		BitsPerPixel:  data[0],
		Depth:         data[1],
		BigEndianFlag: data[2],
		TrueColorFlag: data[3],
		RedMax:        binary.BigEndian.Uint16(data[4:6]),
		GreenMax:      binary.BigEndian.Uint16(data[6:8]),
		BlueMax:       binary.BigEndian.Uint16(data[8:10]),
		RedShift:      data[10],
		GreenShift:    data[11],
		BlueShift:     data[12],
	}, nil
}

// ReadPixelFormat reads a PixelFormat from an io.Reader.
func ReadPixelFormat(r io.Reader) (PixelFormat, error) {
	buf := make([]byte, 16)
	if _, err := io.ReadFull(r, buf); err != nil {
		return PixelFormat{}, fmt.Errorf("failed to read pixel format: %w", err)
	}
	return UnmarshalPixelFormat(buf)
}

// Rectangle describes a rectangular area of the framebuffer.
type Rectangle struct {
	X            uint16
	Y            uint16
	Width        uint16
	Height       uint16
	EncodingType int32
}

// MarshalHeader writes the 12-byte rectangle header.
func (r Rectangle) MarshalHeader() []byte {
	buf := make([]byte, 12)
	binary.BigEndian.PutUint16(buf[0:2], r.X)
	binary.BigEndian.PutUint16(buf[2:4], r.Y)
	binary.BigEndian.PutUint16(buf[4:6], r.Width)
	binary.BigEndian.PutUint16(buf[6:8], r.Height)
	binary.BigEndian.PutUint32(buf[8:12], uint32(r.EncodingType))
	return buf
}

// UnmarshalRectangleHeader reads a Rectangle header from 12 bytes of wire data.
func UnmarshalRectangleHeader(data []byte) (Rectangle, error) {
	if len(data) < 12 {
		return Rectangle{}, fmt.Errorf("rectangle header too short: %d bytes", len(data))
	}
	return Rectangle{
		X:            binary.BigEndian.Uint16(data[0:2]),
		Y:            binary.BigEndian.Uint16(data[2:4]),
		Width:        binary.BigEndian.Uint16(data[4:6]),
		Height:       binary.BigEndian.Uint16(data[6:8]),
		EncodingType: int32(binary.BigEndian.Uint32(data[8:12])),
	}, nil
}

// ServerInit is the server initialization message sent after the handshake.
type ServerInit struct {
	Width       uint16
	Height      uint16
	PixelFormat PixelFormat
	Name        string
}

// Marshal writes the ServerInit message in wire format.
func (si ServerInit) Marshal() []byte {
	nameBytes := []byte(si.Name)
	buf := make([]byte, 4+16+4+len(nameBytes))
	binary.BigEndian.PutUint16(buf[0:2], si.Width)
	binary.BigEndian.PutUint16(buf[2:4], si.Height)
	copy(buf[4:20], si.PixelFormat.Marshal())
	binary.BigEndian.PutUint32(buf[20:24], uint32(len(nameBytes)))
	copy(buf[24:], nameBytes)
	return buf
}

// SecurityResult values.
const (
	SecurityResultOK     = 0
	SecurityResultFailed = 1
)

// FrameProvider provides framebuffer data for the VNC server.
type FrameProvider interface {
	// GetFrame returns the current framebuffer as RGBA pixel data.
	// Returns nil if no frame is available.
	GetFrame() *FrameData

	// GetSize returns the current framebuffer dimensions.
	GetSize() (width, height uint16)
}

// FrameData holds a decoded framebuffer frame.
type FrameData struct {
	// Pix holds the pixel data in RGBA format, 4 bytes per pixel.
	Pix []uint8
	// Stride is the number of bytes per row.
	Stride int
	// Width of the frame in pixels.
	Width int
	// Height of the frame in pixels.
	Height int
}

// InputHandler receives keyboard and mouse events from VNC clients.
type InputHandler interface {
	// KeyEvent is called when a key is pressed or released.
	// keysym is the X11 keysym, down is true for press.
	KeyEvent(keysym uint32, down bool)

	// PointerEvent is called when the pointer moves or buttons change.
	// buttonMask is a bitmask (bit 0 = left, bit 1 = middle, bit 2 = right,
	// bits 3-4 = scroll up/down).
	// x, y are pixel coordinates.
	PointerEvent(buttonMask uint8, x, y uint16)
}
