package rdp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/nakagami/grdp/core"
	"github.com/nakagami/grdp/protocol/pdu"
)

// RDP protocol constants.
const (
	// Default RDP port.
	DefaultPort = 3389

	// TPKT header length.
	tpktHeaderLen = 4
	tpktVersion   = 3

	// X.224 message types.
	x224TypeConnectionRequest = 0xE0
	x224TypeConnectionConfirm = 0xD0
	x224TypeData              = 0xF0

	// RDP Negotiation types.
	negoTypeRequest  = 0x01
	negoTypeResponse = 0x02

	// RDP security protocols (bitmask).
	protoRDP       = 0x00000000
	protoTLS       = 0x00000001
	protoCredSSP   = 0x00000003
	protoHybridEx  = 0x00000008
	protoRDSTLS    = 0x00000010

	// MCS message types (T.125).
	mcsTypeConnectInitial  = 0x7F65
	mcsTypeConnectResponse = 0x7F66
	mcsTypeErectDomain     = 0x04
	mcsTypeAttachUser      = 0x28
	mcsTypeAttachUserConf  = 0x2E
	mcsTypeChannelJoin     = 0x38
	mcsTypeChannelJoinConf = 0x3E
	mcsTypeSendDataRequest = 0x64
	mcsTypeSendDataIndic   = 0x68

	// RDP Security header types.
	secExchangePkt = 0x0001
	secInfoPkt     = 0x0040
	secLicensePkt  = 0x0080

	// Share control PDU types.
	pduTypeDemandActive  = 0x0001
	pduTypeConfirmActive = 0x0003
	pduTypeData          = 0x0007

	// Share data PDU types.
	pduTypeDataSynchronize = 31
	pduTypeDataControl     = 20
	pduTypeDataFontList    = 39
	pduTypeDataFontMap     = 40
	pduTypeDataInput       = 28
	pduTypeDataBitmapUp    = 33
	pduTypeDataSuppress    = 35
	pduTypeDataShutReq     = 36
	pduTypeDataShutDenied  = 37

	// Capability set types.
	capGeneral     = 0x0001
	capBitmap      = 0x0002
	capOrder       = 0x0003
	capPointer     = 0x0008
	capInput       = 0x000D
	capVirtualChan = 0x0014

	// General capability flags.
	generalExtraFlags = 0x0001

	// Bitmap encoding types.
	bitmapCompNone = 0x0000
)

// Input event type constants — aliased from grdp/protocol/pdu.
const (
	InputEventSync     = pdu.INPUT_EVENT_SYNC
	InputEventScancode = pdu.INPUT_EVENT_SCANCODE
	InputEventUnicode  = pdu.INPUT_EVENT_UNICODE
	InputEventMouse    = pdu.INPUT_EVENT_MOUSE
)

// Mouse event flag constants — aliased from grdp/protocol/pdu.
const (
	MouseFlagMove      = pdu.PTRFLAGS_MOVE
	MouseFlagButton1   = pdu.PTRFLAGS_BUTTON1
	MouseFlagButton2   = pdu.PTRFLAGS_BUTTON2
	MouseFlagButton3   = pdu.PTRFLAGS_BUTTON3
	MouseFlagDown      = pdu.PTRFLAGS_DOWN
	MouseFlagWheelUp   = pdu.PTRFLAGS_WHEEL
	MouseFlagWheelDown = pdu.PTRFLAGS_HWHEEL
	MouseFlagWheelMask = pdu.WheelRotationMask
)

// Keyboard event flag constants — aliased from grdp/protocol/pdu.
const (
	KeyFlagExtended = pdu.KBDFLAGS_EXTENDED
	KeyFlagDown     = 0x0000
	KeyFlagRelease  = pdu.KBDFLAGS_RELEASE
)

// Errors.
var (
	ErrInvalidTPKT       = errors.New("rdp: invalid TPKT header")
	ErrInvalidX224       = errors.New("rdp: invalid X.224 PDU")
	ErrInvalidMCS        = errors.New("rdp: invalid MCS PDU")
	ErrUnsupportedProto  = errors.New("rdp: unsupported security protocol")
	ErrSessionClosed     = errors.New("rdp: session closed")
	ErrInvalidPacketLen  = errors.New("rdp: invalid packet length")
)

// tpktHeader represents a TPKT header (RFC 1006).
type tpktHeader struct {
	Version  uint8
	Reserved uint8
	Length   uint16
}

// readTPKT reads a TPKT packet using grdp/core I/O utilities.
func readTPKT(r io.Reader) (tpktHeader, []byte, error) {
	// Read 4-byte TPKT header using core I/O. We read all 4 bytes at once
	// and parse them, since grdp's ReadUInt8 can panic on EOF.
	hdrBytes, err := core.ReadBytes(tpktHeaderLen, r)
	if err != nil {
		return tpktHeader{}, nil, fmt.Errorf("read TPKT header: %w", err)
	}
	version := hdrBytes[0]
	if version != tpktVersion {
		return tpktHeader{}, nil, ErrInvalidTPKT
	}
	length := binary.BigEndian.Uint16(hdrBytes[2:4])
	hdr := tpktHeader{Version: version, Reserved: hdrBytes[1], Length: length}
	if hdr.Length < tpktHeaderLen {
		return hdr, nil, ErrInvalidPacketLen
	}
	payload, err := core.ReadBytes(int(hdr.Length-tpktHeaderLen), r)
	if err != nil {
		return hdr, nil, fmt.Errorf("read TPKT payload: %w", err)
	}
	return hdr, payload, nil
}

// writeTPKT writes a TPKT packet using grdp/core I/O utilities.
func writeTPKT(w io.Writer, payload []byte) error {
	buf := &bytes.Buffer{}
	core.WriteUInt8(tpktVersion, buf)
	core.WriteUInt8(0, buf) // reserved
	core.WriteUInt16BE(uint16(tpktHeaderLen+len(payload)), buf)
	buf.Write(payload)
	if _, err := w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("write TPKT: %w", err)
	}
	return nil
}

// x224Header represents an X.224 Connection Request/Confirm header.
type x224Header struct {
	Length  uint8
	Type    uint8
	DstRef  uint16
	SrcRef  uint16
	Class   uint8
}

// negoRequest represents an RDP Negotiation Request.
type negoRequest struct {
	Type     uint8
	Flags    uint8
	Length   uint16
	Protocol uint32
}

// negoResponse represents an RDP Negotiation Response.
type negoResponse struct {
	Type     uint8
	Flags    uint8
	Length   uint16
	Protocol uint32
}

func parseX224ConnectionRequest(data []byte) (x224Header, *negoRequest, error) {
	if len(data) < 6 {
		return x224Header{}, nil, ErrInvalidX224
	}
	hdr := x224Header{
		Length: data[0],
		Type:   data[1] & 0xF0,
		DstRef: binary.BigEndian.Uint16(data[2:4]),
		SrcRef: binary.BigEndian.Uint16(data[4:6]),
	}
	if len(data) > 6 {
		hdr.Class = data[6]
	}
	if hdr.Type != x224TypeConnectionRequest {
		return hdr, nil, ErrInvalidX224
	}

	// Look for RDP Negotiation Request after any cookie/routing token
	var nego *negoRequest

	// Search from offset 7 for the negotiation request (type 0x01)
	for i := 7; i+7 < len(data); i++ {
		if data[i] == negoTypeRequest {
			if i+8 <= len(data) {
				nego = &negoRequest{
					Type:     data[i],
					Flags:    data[i+1],
					Length:   binary.LittleEndian.Uint16(data[i+2 : i+4]),
					Protocol: binary.LittleEndian.Uint32(data[i+4 : i+8]),
				}
				break
			}
		}
	}

	return hdr, nego, nil
}

func buildX224ConnectionConfirm(selectedProto uint32) []byte {
	// X.224 CC + RDP Negotiation Response
	buf := make([]byte, 0, 20)

	// X.224 header
	buf = append(buf,
		14,   // length (rest of X.224 header)
		x224TypeConnectionConfirm, // type
		0, 0, // dst-ref
		0, 0, // src-ref
		0, // class
	)

	// RDP Negotiation Response
	buf = append(buf, negoTypeResponse) // type
	buf = append(buf, 0)               // flags
	buf = appendU16LE(buf, 8)          // length
	buf = appendU32LE(buf, selectedProto)

	return buf
}

func buildX224Data(payload []byte) []byte {
	buf := make([]byte, 0, 3+len(payload))
	buf = append(buf,
		2,              // length
		x224TypeData,   // type
		0x80,           // EOT
	)
	buf = append(buf, payload...)
	return buf
}

// appendU16LE appends a uint16 in little-endian byte order.
func appendU16LE(buf []byte, v uint16) []byte {
	return append(buf, byte(v), byte(v>>8))
}

// appendU32LE appends a uint32 in little-endian byte order.
func appendU32LE(buf []byte, v uint32) []byte {
	return append(buf, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}

// putU16LE writes a uint16 in little-endian byte order at the given offset.
func putU16LE(buf []byte, offset int, v uint16) {
	buf[offset] = byte(v)
	buf[offset+1] = byte(v >> 8)
}

// putU32LE writes a uint32 in little-endian byte order at the given offset.
func putU32LE(buf []byte, offset int, v uint32) {
	buf[offset] = byte(v)
	buf[offset+1] = byte(v >> 8)
	buf[offset+2] = byte(v >> 16)
	buf[offset+3] = byte(v >> 24)
}

// getU16LE reads a uint16 in little-endian byte order.
func getU16LE(buf []byte, offset int) uint16 {
	return binary.LittleEndian.Uint16(buf[offset : offset+2])
}

// getU32LE reads a uint32 in little-endian byte order.
func getU32LE(buf []byte, offset int) uint32 {
	return binary.LittleEndian.Uint32(buf[offset : offset+4])
}
