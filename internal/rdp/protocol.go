package rdp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
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

	// RDP Input event types.
	InputEventSync     = 0x0000
	InputEventScancode = 0x0004
	InputEventUnicode  = 0x0005
	InputEventMouse    = 0x8001

	// Mouse event flags.
	MouseFlagMove       = 0x0800
	MouseFlagButton1    = 0x1000
	MouseFlagButton2    = 0x2000
	MouseFlagButton3    = 0x4000
	MouseFlagDown       = 0x8000
	MouseFlagWheelUp    = 0x0200
	MouseFlagWheelDown  = 0x0400
	MouseFlagWheelMask  = 0x01FF

	// Keyboard event flags.
	KeyFlagExtended = 0x0100
	KeyFlagDown     = 0x0000
	KeyFlagRelease  = 0x8000

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

func readTPKT(r io.Reader) (tpktHeader, []byte, error) {
	var hdr tpktHeader
	if err := binary.Read(r, binary.BigEndian, &hdr); err != nil {
		return hdr, nil, fmt.Errorf("read TPKT header: %w", err)
	}
	if hdr.Version != tpktVersion {
		return hdr, nil, ErrInvalidTPKT
	}
	if hdr.Length < tpktHeaderLen {
		return hdr, nil, ErrInvalidPacketLen
	}
	payload := make([]byte, hdr.Length-tpktHeaderLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return hdr, nil, fmt.Errorf("read TPKT payload: %w", err)
	}
	return hdr, payload, nil
}

func writeTPKT(w io.Writer, payload []byte) error {
	hdr := tpktHeader{
		Version:  tpktVersion,
		Reserved: 0,
		Length:   uint16(tpktHeaderLen + len(payload)),
	}
	if err := binary.Write(w, binary.BigEndian, &hdr); err != nil {
		return fmt.Errorf("write TPKT header: %w", err)
	}
	if _, err := w.Write(payload); err != nil {
		return fmt.Errorf("write TPKT payload: %w", err)
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
	remaining := data[hdr.Length:]
	if int(hdr.Length) < len(data) {
		remaining = data[hdr.Length:]
	}

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

	_ = remaining
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
