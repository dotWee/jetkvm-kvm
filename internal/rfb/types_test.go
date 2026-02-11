package rfb

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultPixelFormat(t *testing.T) {
	pf := DefaultPixelFormat()
	assert.Equal(t, uint8(32), pf.BitsPerPixel)
	assert.Equal(t, uint8(24), pf.Depth)
	assert.Equal(t, uint8(0), pf.BigEndianFlag)
	assert.Equal(t, uint8(1), pf.TrueColorFlag)
	assert.Equal(t, uint16(255), pf.RedMax)
	assert.Equal(t, uint16(255), pf.GreenMax)
	assert.Equal(t, uint16(255), pf.BlueMax)
	assert.Equal(t, uint8(16), pf.RedShift)
	assert.Equal(t, uint8(8), pf.GreenShift)
	assert.Equal(t, uint8(0), pf.BlueShift)
}

func TestPixelFormatMarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name string
		pf   PixelFormat
	}{
		{"default_32bpp", DefaultPixelFormat()},
		{"16bpp_rgb565", PixelFormat{
			BitsPerPixel: 16, Depth: 16, BigEndianFlag: 0, TrueColorFlag: 1,
			RedMax: 31, GreenMax: 63, BlueMax: 31,
			RedShift: 11, GreenShift: 5, BlueShift: 0,
		}},
		{"8bpp_332", PixelFormat{
			BitsPerPixel: 8, Depth: 8, BigEndianFlag: 0, TrueColorFlag: 1,
			RedMax: 7, GreenMax: 7, BlueMax: 3,
			RedShift: 5, GreenShift: 2, BlueShift: 0,
		}},
		{"32bpp_bgr", PixelFormat{
			BitsPerPixel: 32, Depth: 24, BigEndianFlag: 1, TrueColorFlag: 1,
			RedMax: 255, GreenMax: 255, BlueMax: 255,
			RedShift: 0, GreenShift: 8, BlueShift: 16,
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := tt.pf.Marshal()
			assert.Len(t, data, 16, "marshalled pixel format should be 16 bytes")

			restored, err := UnmarshalPixelFormat(data)
			require.NoError(t, err)
			assert.Equal(t, tt.pf, restored)
		})
	}
}

func TestPixelFormatMarshalWireFormat(t *testing.T) {
	pf := DefaultPixelFormat()
	data := pf.Marshal()

	// Verify specific byte offsets per RFC 6143
	assert.Equal(t, byte(32), data[0], "bits-per-pixel")
	assert.Equal(t, byte(24), data[1], "depth")
	assert.Equal(t, byte(0), data[2], "big-endian-flag")
	assert.Equal(t, byte(1), data[3], "true-colour-flag")

	redMax := binary.BigEndian.Uint16(data[4:6])
	assert.Equal(t, uint16(255), redMax, "red-max")

	greenMax := binary.BigEndian.Uint16(data[6:8])
	assert.Equal(t, uint16(255), greenMax, "green-max")

	blueMax := binary.BigEndian.Uint16(data[8:10])
	assert.Equal(t, uint16(255), blueMax, "blue-max")

	assert.Equal(t, byte(16), data[10], "red-shift")
	assert.Equal(t, byte(8), data[11], "green-shift")
	assert.Equal(t, byte(0), data[12], "blue-shift")

	// Padding bytes should be zero
	assert.Equal(t, byte(0), data[13])
	assert.Equal(t, byte(0), data[14])
	assert.Equal(t, byte(0), data[15])
}

func TestReadPixelFormat(t *testing.T) {
	pf := DefaultPixelFormat()
	data := pf.Marshal()
	reader := bytes.NewReader(data)

	result, err := ReadPixelFormat(reader)
	require.NoError(t, err)
	assert.Equal(t, pf, result)
}

func TestReadPixelFormatTooShort(t *testing.T) {
	reader := bytes.NewReader([]byte{1, 2, 3})
	_, err := ReadPixelFormat(reader)
	assert.Error(t, err)
}

func TestUnmarshalPixelFormatTooShort(t *testing.T) {
	_, err := UnmarshalPixelFormat([]byte{1, 2, 3})
	assert.Error(t, err)
}

func TestRectangleHeaderMarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name string
		rect Rectangle
	}{
		{"origin_raw", Rectangle{X: 0, Y: 0, Width: 1920, Height: 1080, EncodingType: EncodingRaw}},
		{"offset_zrle", Rectangle{X: 100, Y: 200, Width: 640, Height: 480, EncodingType: EncodingZRLE}},
		{"desktop_size", Rectangle{X: 0, Y: 0, Width: 1920, Height: 1080, EncodingType: EncodingDesktopSize}},
		{"cursor_pseudo", Rectangle{X: 10, Y: 20, Width: 32, Height: 32, EncodingType: EncodingCursor}},
		{"max_values", Rectangle{X: 65535, Y: 65535, Width: 65535, Height: 65535, EncodingType: EncodingRaw}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := tt.rect.MarshalHeader()
			assert.Len(t, data, 12, "rectangle header should be 12 bytes")

			restored, err := UnmarshalRectangleHeader(data)
			require.NoError(t, err)
			assert.Equal(t, tt.rect, restored)
		})
	}
}

func TestRectangleHeaderWireFormat(t *testing.T) {
	rect := Rectangle{X: 0x0102, Y: 0x0304, Width: 0x0506, Height: 0x0708, EncodingType: EncodingRaw}
	data := rect.MarshalHeader()

	assert.Equal(t, []byte{0x01, 0x02}, data[0:2], "x-position")
	assert.Equal(t, []byte{0x03, 0x04}, data[2:4], "y-position")
	assert.Equal(t, []byte{0x05, 0x06}, data[4:6], "width")
	assert.Equal(t, []byte{0x07, 0x08}, data[6:8], "height")
	assert.Equal(t, []byte{0x00, 0x00, 0x00, 0x00}, data[8:12], "encoding-type raw=0")
}

func TestUnmarshalRectangleHeaderTooShort(t *testing.T) {
	_, err := UnmarshalRectangleHeader([]byte{1, 2, 3})
	assert.Error(t, err)
}

func TestServerInitMarshal(t *testing.T) {
	si := ServerInit{
		Width:       1920,
		Height:      1080,
		PixelFormat: DefaultPixelFormat(),
		Name:        "JetKVM",
	}

	data := si.Marshal()

	// Width (2 bytes) + Height (2 bytes) + PixelFormat (16 bytes) + NameLength (4 bytes) + Name
	expectedLen := 2 + 2 + 16 + 4 + 6
	assert.Len(t, data, expectedLen)

	// Verify width
	assert.Equal(t, uint16(1920), binary.BigEndian.Uint16(data[0:2]))
	// Verify height
	assert.Equal(t, uint16(1080), binary.BigEndian.Uint16(data[2:4]))
	// Verify name length
	assert.Equal(t, uint32(6), binary.BigEndian.Uint32(data[20:24]))
	// Verify name
	assert.Equal(t, "JetKVM", string(data[24:]))
}

func TestServerInitEmptyName(t *testing.T) {
	si := ServerInit{
		Width:       640,
		Height:      480,
		PixelFormat: DefaultPixelFormat(),
		Name:        "",
	}

	data := si.Marshal()
	// Name length should be 0
	assert.Equal(t, uint32(0), binary.BigEndian.Uint32(data[20:24]))
	assert.Len(t, data, 24) // No name bytes
}

func TestEncodingConstants(t *testing.T) {
	// Verify encoding type constants match RFC 6143
	assert.Equal(t, int32(0), int32(EncodingRaw))
	assert.Equal(t, int32(1), int32(EncodingCopyRect))
	assert.Equal(t, int32(2), int32(EncodingRRE))
	assert.Equal(t, int32(5), int32(EncodingHextile))
	assert.Equal(t, int32(15), int32(EncodingTRLE))
	assert.Equal(t, int32(16), int32(EncodingZRLE))
	assert.Equal(t, int32(-239), int32(EncodingCursor))
	assert.Equal(t, int32(-223), int32(EncodingDesktopSize))
}

func TestSecurityTypeConstants(t *testing.T) {
	assert.Equal(t, uint8(0), uint8(SecTypeInvalid))
	assert.Equal(t, uint8(1), uint8(SecTypeNone))
	assert.Equal(t, uint8(2), uint8(SecTypeVNCAuth))
}

func TestProtocolVersion(t *testing.T) {
	assert.Equal(t, "RFB 003.008\n", ProtocolVersion)
	assert.Len(t, ProtocolVersion, 12)
}
