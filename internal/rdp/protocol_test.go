package rdp

import (
	"bytes"
	"testing"
)

func TestReadWriteTPKT(t *testing.T) {
	payload := []byte{0x01, 0x02, 0x03}
	var buf bytes.Buffer
	if err := writeTPKT(&buf, payload); err != nil {
		t.Fatalf("writeTPKT: %v", err)
	}

	hdr, data, err := readTPKT(&buf)
	if err != nil {
		t.Fatalf("readTPKT: %v", err)
	}

	if hdr.Version != tpktVersion {
		t.Errorf("version = %d, want %d", hdr.Version, tpktVersion)
	}
	if hdr.Length != uint16(tpktHeaderLen+len(payload)) {
		t.Errorf("length = %d, want %d", hdr.Length, tpktHeaderLen+len(payload))
	}
	if !bytes.Equal(data, payload) {
		t.Errorf("payload = %v, want %v", data, payload)
	}
}

func TestReadTPKTInvalidVersion(t *testing.T) {
	data := []byte{0x02, 0x00, 0x00, 0x05, 0xFF}
	_, _, err := readTPKT(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error for invalid TPKT version")
	}
}

func TestReadTPKTShortLength(t *testing.T) {
	data := []byte{0x03, 0x00, 0x00, 0x02}
	_, _, err := readTPKT(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error for short TPKT length")
	}
}

func TestParseX224ConnectionRequest(t *testing.T) {
	// Build a minimal X.224 Connection Request
	data := []byte{
		6,                         // length
		x224TypeConnectionRequest, // type
		0x00, 0x00, // dst-ref
		0x00, 0x00, // src-ref
		0x00, // class
	}

	hdr, nego, err := parseX224ConnectionRequest(data)
	if err != nil {
		t.Fatalf("parseX224ConnectionRequest: %v", err)
	}
	if hdr.Type != x224TypeConnectionRequest {
		t.Errorf("type = 0x%02X, want 0x%02X", hdr.Type, x224TypeConnectionRequest)
	}
	if nego != nil {
		t.Error("expected nil negotiation for minimal request")
	}
}

func TestParseX224ConnectionRequestWithNego(t *testing.T) {
	data := []byte{
		14,                        // length
		x224TypeConnectionRequest, // type
		0x00, 0x00, // dst-ref
		0x00, 0x00, // src-ref
		0x00,            // class
		negoTypeRequest, // negotiation type
		0x00,            // flags
		0x08, 0x00, // length (8)
		0x03, 0x00, 0x00, 0x00, // protocol (TLS | CredSSP)
	}

	hdr, nego, err := parseX224ConnectionRequest(data)
	if err != nil {
		t.Fatalf("parseX224ConnectionRequest: %v", err)
	}
	if hdr.Type != x224TypeConnectionRequest {
		t.Errorf("type = 0x%02X, want 0x%02X", hdr.Type, x224TypeConnectionRequest)
	}
	if nego == nil {
		t.Fatal("expected negotiation request")
	}
	if nego.Protocol != protoCredSSP {
		t.Errorf("protocol = 0x%08X, want 0x%08X", nego.Protocol, protoCredSSP)
	}
}

func TestBuildX224ConnectionConfirm(t *testing.T) {
	data := buildX224ConnectionConfirm(protoRDP)

	if len(data) == 0 {
		t.Fatal("empty connection confirm")
	}
	if data[1] != x224TypeConnectionConfirm {
		t.Errorf("type = 0x%02X, want 0x%02X", data[1], x224TypeConnectionConfirm)
	}
}

func TestBuildX224Data(t *testing.T) {
	payload := []byte{0xAA, 0xBB}
	data := buildX224Data(payload)

	if data[0] != 2 {
		t.Errorf("length = %d, want 2", data[0])
	}
	if data[1] != x224TypeData {
		t.Errorf("type = 0x%02X, want 0x%02X", data[1], x224TypeData)
	}
	if data[2] != 0x80 {
		t.Errorf("EOT = 0x%02X, want 0x80", data[2])
	}
	if !bytes.Equal(data[3:], payload) {
		t.Errorf("payload = %v, want %v", data[3:], payload)
	}
}

func TestAppendHelpers(t *testing.T) {
	var buf []byte
	buf = appendU16LE(buf, 0x0201)
	if buf[0] != 0x01 || buf[1] != 0x02 {
		t.Errorf("appendU16LE = %v, want [0x01, 0x02]", buf)
	}

	buf = nil
	buf = appendU32LE(buf, 0x04030201)
	if buf[0] != 0x01 || buf[1] != 0x02 || buf[2] != 0x03 || buf[3] != 0x04 {
		t.Errorf("appendU32LE = %v, want [0x01, 0x02, 0x03, 0x04]", buf)
	}
}

func TestPutGetHelpers(t *testing.T) {
	buf := make([]byte, 8)

	putU16LE(buf, 0, 0xBEEF)
	got16 := getU16LE(buf, 0)
	if got16 != 0xBEEF {
		t.Errorf("getU16LE = 0x%04X, want 0xBEEF", got16)
	}

	putU32LE(buf, 2, 0xDEADBEEF)
	got32 := getU32LE(buf, 2)
	if got32 != 0xDEADBEEF {
		t.Errorf("getU32LE = 0x%08X, want 0xDEADBEEF", got32)
	}
}

func TestTPKTRoundTrip(t *testing.T) {
	testCases := [][]byte{
		{},
		{0xFF},
		make([]byte, 256),
	}

	for i, payload := range testCases {
		var buf bytes.Buffer
		if err := writeTPKT(&buf, payload); err != nil {
			t.Fatalf("case %d: writeTPKT: %v", i, err)
		}
		_, got, err := readTPKT(&buf)
		if err != nil {
			t.Fatalf("case %d: readTPKT: %v", i, err)
		}
		if !bytes.Equal(got, payload) {
			t.Errorf("case %d: payload mismatch", i)
		}
	}
}
