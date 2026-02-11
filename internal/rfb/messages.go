package rfb

import (
	"encoding/binary"
	"fmt"
	"io"
)

// --- Client-to-Server Messages ---

// SetPixelFormatMsg is the parsed SetPixelFormat message (type 0).
type SetPixelFormatMsg struct {
	Format PixelFormat
}

// ReadSetPixelFormat reads a SetPixelFormat message from r.
// The message type byte (0) has already been read.
func ReadSetPixelFormat(r io.Reader) (SetPixelFormatMsg, error) {
	// 3 bytes padding + 16 bytes pixel format
	buf := make([]byte, 3+16)
	if _, err := io.ReadFull(r, buf); err != nil {
		return SetPixelFormatMsg{}, fmt.Errorf("failed to read SetPixelFormat: %w", err)
	}
	pf, err := UnmarshalPixelFormat(buf[3:])
	if err != nil {
		return SetPixelFormatMsg{}, err
	}
	return SetPixelFormatMsg{Format: pf}, nil
}

// SetEncodingsMsg is the parsed SetEncodings message (type 2).
type SetEncodingsMsg struct {
	Encodings []int32
}

// ReadSetEncodings reads a SetEncodings message from r.
// The message type byte (2) has already been read.
func ReadSetEncodings(r io.Reader) (SetEncodingsMsg, error) {
	// 1 byte padding + 2 bytes count
	header := make([]byte, 3)
	if _, err := io.ReadFull(r, header); err != nil {
		return SetEncodingsMsg{}, fmt.Errorf("failed to read SetEncodings header: %w", err)
	}
	count := binary.BigEndian.Uint16(header[1:3])

	if count > 1024 {
		return SetEncodingsMsg{}, fmt.Errorf("too many encodings: %d", count)
	}

	encodings := make([]int32, count)
	for i := range count {
		buf := make([]byte, 4)
		if _, err := io.ReadFull(r, buf); err != nil {
			return SetEncodingsMsg{}, fmt.Errorf("failed to read encoding %d: %w", i, err)
		}
		encodings[i] = int32(binary.BigEndian.Uint32(buf))
	}

	return SetEncodingsMsg{Encodings: encodings}, nil
}

// FramebufferUpdateRequestMsg is the parsed FramebufferUpdateRequest (type 3).
type FramebufferUpdateRequestMsg struct {
	Incremental bool
	X           uint16
	Y           uint16
	Width       uint16
	Height      uint16
}

// ReadFramebufferUpdateRequest reads a FramebufferUpdateRequest from r.
// The message type byte (3) has already been read.
func ReadFramebufferUpdateRequest(r io.Reader) (FramebufferUpdateRequestMsg, error) {
	buf := make([]byte, 9)
	if _, err := io.ReadFull(r, buf); err != nil {
		return FramebufferUpdateRequestMsg{}, fmt.Errorf("failed to read FramebufferUpdateRequest: %w", err)
	}
	return FramebufferUpdateRequestMsg{
		Incremental: buf[0] != 0,
		X:           binary.BigEndian.Uint16(buf[1:3]),
		Y:           binary.BigEndian.Uint16(buf[3:5]),
		Width:       binary.BigEndian.Uint16(buf[5:7]),
		Height:      binary.BigEndian.Uint16(buf[7:9]),
	}, nil
}

// KeyEventMsg is the parsed KeyEvent message (type 4).
type KeyEventMsg struct {
	DownFlag bool
	Key      uint32 // X11 keysym
}

// ReadKeyEvent reads a KeyEvent message from r.
// The message type byte (4) has already been read.
func ReadKeyEvent(r io.Reader) (KeyEventMsg, error) {
	buf := make([]byte, 7)
	if _, err := io.ReadFull(r, buf); err != nil {
		return KeyEventMsg{}, fmt.Errorf("failed to read KeyEvent: %w", err)
	}
	return KeyEventMsg{
		DownFlag: buf[0] != 0,
		Key:      binary.BigEndian.Uint32(buf[3:7]),
	}, nil
}

// PointerEventMsg is the parsed PointerEvent message (type 5).
type PointerEventMsg struct {
	ButtonMask uint8
	X          uint16
	Y          uint16
}

// ReadPointerEvent reads a PointerEvent message from r.
// The message type byte (5) has already been read.
func ReadPointerEvent(r io.Reader) (PointerEventMsg, error) {
	buf := make([]byte, 5)
	if _, err := io.ReadFull(r, buf); err != nil {
		return PointerEventMsg{}, fmt.Errorf("failed to read PointerEvent: %w", err)
	}
	return PointerEventMsg{
		ButtonMask: buf[0],
		X:          binary.BigEndian.Uint16(buf[1:3]),
		Y:          binary.BigEndian.Uint16(buf[3:5]),
	}, nil
}

// ClientCutTextMsg is the parsed ClientCutText message (type 6).
type ClientCutTextMsg struct {
	Text string
}

// ReadClientCutText reads a ClientCutText message from r.
// The message type byte (6) has already been read.
func ReadClientCutText(r io.Reader) (ClientCutTextMsg, error) {
	// 3 bytes padding + 4 bytes length
	header := make([]byte, 7)
	if _, err := io.ReadFull(r, header); err != nil {
		return ClientCutTextMsg{}, fmt.Errorf("failed to read ClientCutText header: %w", err)
	}
	length := binary.BigEndian.Uint32(header[3:7])
	if length > 10*1024*1024 { // 10MB limit
		return ClientCutTextMsg{}, fmt.Errorf("clipboard text too large: %d bytes", length)
	}

	text := make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(r, text); err != nil {
			return ClientCutTextMsg{}, fmt.Errorf("failed to read clipboard text: %w", err)
		}
	}
	return ClientCutTextMsg{Text: string(text)}, nil
}

// --- Server-to-Client Messages ---

// WriteFramebufferUpdate writes a FramebufferUpdate message header.
// The caller must then write the rectangle data.
func WriteFramebufferUpdate(w io.Writer, numRects uint16) error {
	buf := make([]byte, 4)
	buf[0] = MsgFramebufferUpdate
	buf[1] = 0 // padding
	binary.BigEndian.PutUint16(buf[2:4], numRects)
	_, err := w.Write(buf)
	return err
}

// WriteBell sends a Bell message to the client.
func WriteBell(w io.Writer) error {
	_, err := w.Write([]byte{MsgBell})
	return err
}

// WriteServerCutText sends a ServerCutText message to the client.
func WriteServerCutText(w io.Writer, text string) error {
	textBytes := []byte(text)
	buf := make([]byte, 8+len(textBytes))
	buf[0] = MsgServerCutText
	// bytes 1-3 padding
	binary.BigEndian.PutUint32(buf[4:8], uint32(len(textBytes)))
	copy(buf[8:], textBytes)
	_, err := w.Write(buf)
	return err
}
