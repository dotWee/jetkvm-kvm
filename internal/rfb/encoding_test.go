package rfb

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeTestImage(width, height int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{
				R: uint8((x * 37) % 256),
				G: uint8((y * 53) % 256),
				B: uint8(((x + y) * 17) % 256),
				A: 255,
			})
		}
	}
	return img
}

func TestRawEncoderType(t *testing.T) {
	e := &RawEncoder{}
	assert.Equal(t, int32(EncodingRaw), e.Type())
}

func TestRawEncoderSmallImage(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	// Set specific pixels
	img.Set(0, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	img.Set(1, 0, color.RGBA{R: 0, G: 255, B: 0, A: 255})
	img.Set(0, 1, color.RGBA{R: 0, G: 0, B: 255, A: 255})
	img.Set(1, 1, color.RGBA{R: 255, G: 255, B: 255, A: 255})

	pf := DefaultPixelFormat() // 32bpp, RGB shifts: 16,8,0
	rect := Rectangle{X: 0, Y: 0, Width: 2, Height: 2, EncodingType: EncodingRaw}

	var buf bytes.Buffer
	e := &RawEncoder{}
	err := e.Encode(&buf, img, rect, pf)
	require.NoError(t, err)

	// 2x2 pixels * 4 bytes = 16 bytes
	assert.Equal(t, 16, buf.Len())

	data := buf.Bytes()

	// Pixel (0,0): R=255, G=0, B=0 → little-endian 32-bit: 0x00FF0000
	// In LE bytes: 0x00, 0x00, 0xFF, 0x00
	assert.Equal(t, byte(0x00), data[0]) // B
	assert.Equal(t, byte(0x00), data[1]) // G
	assert.Equal(t, byte(0xFF), data[2]) // R
	assert.Equal(t, byte(0x00), data[3]) // padding/A

	// Pixel (1,0): G=255 → 0x0000FF00 LE: 0x00 0xFF 0x00 0x00
	assert.Equal(t, byte(0x00), data[4])
	assert.Equal(t, byte(0xFF), data[5])
	assert.Equal(t, byte(0x00), data[6])
	assert.Equal(t, byte(0x00), data[7])
}

func TestRawEncoderSubrectangle(t *testing.T) {
	img := makeTestImage(10, 10)

	pf := DefaultPixelFormat()
	rect := Rectangle{X: 2, Y: 3, Width: 4, Height: 5, EncodingType: EncodingRaw}

	var buf bytes.Buffer
	e := &RawEncoder{}
	err := e.Encode(&buf, img, rect, pf)
	require.NoError(t, err)

	// 4*5 pixels * 4 bytes = 80 bytes
	assert.Equal(t, 80, buf.Len())
}

func TestRawEncoder16bpp(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	img.Set(0, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	img.Set(1, 0, color.RGBA{R: 0, G: 0, B: 255, A: 255})

	pf := PixelFormat{
		BitsPerPixel: 16, Depth: 16, BigEndianFlag: 0, TrueColorFlag: 1,
		RedMax: 31, GreenMax: 63, BlueMax: 31,
		RedShift: 11, GreenShift: 5, BlueShift: 0,
	}
	rect := Rectangle{X: 0, Y: 0, Width: 2, Height: 1, EncodingType: EncodingRaw}

	var buf bytes.Buffer
	e := &RawEncoder{}
	err := e.Encode(&buf, img, rect, pf)
	require.NoError(t, err)

	// 2 pixels * 2 bytes = 4 bytes
	assert.Equal(t, 4, buf.Len())
}

func TestZRLEEncoderType(t *testing.T) {
	e := &ZRLEEncoder{}
	assert.Equal(t, int32(EncodingZRLE), e.Type())
}

func TestZRLEEncoderSmallImage(t *testing.T) {
	img := makeTestImage(4, 4)

	pf := DefaultPixelFormat()
	rect := Rectangle{X: 0, Y: 0, Width: 4, Height: 4, EncodingType: EncodingZRLE}

	var buf bytes.Buffer
	e := &ZRLEEncoder{}
	err := e.Encode(&buf, img, rect, pf)
	require.NoError(t, err)

	// Output should have 4-byte length prefix + compressed data
	assert.Greater(t, buf.Len(), 4)
}

func TestZRLEEncoderLargerImage(t *testing.T) {
	img := makeTestImage(128, 128)

	pf := DefaultPixelFormat()
	rect := Rectangle{X: 0, Y: 0, Width: 128, Height: 128, EncodingType: EncodingZRLE}

	var buf bytes.Buffer
	e := &ZRLEEncoder{}
	err := e.Encode(&buf, img, rect, pf)
	require.NoError(t, err)

	// Should produce compressed output
	assert.Greater(t, buf.Len(), 4)

	// Compressed data should be smaller than raw (128*128*3 = 49152 for CPIXEL)
	rawSize := 128 * 128 * 3 // CPIXEL for 32bpp/24depth = 3 bytes
	assert.Less(t, buf.Len()-4, rawSize, "ZRLE should compress")
}

func TestConvertPixel(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 200, G: 100, B: 50, A: 255})

	pf := DefaultPixelFormat() // red_shift=16, green_shift=8, blue_shift=0, max=255
	pixel := convertPixel(img, 0, 0, pf)

	r := (pixel >> 16) & 0xFF
	g := (pixel >> 8) & 0xFF
	b := pixel & 0xFF

	assert.Equal(t, uint32(200), r)
	assert.Equal(t, uint32(100), g)
	assert.Equal(t, uint32(50), b)
}

func TestConvertPixelScaling(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 255, G: 128, B: 0, A: 255})

	// RGB565 format
	pf := PixelFormat{
		BitsPerPixel: 16, Depth: 16, BigEndianFlag: 0, TrueColorFlag: 1,
		RedMax: 31, GreenMax: 63, BlueMax: 31,
		RedShift: 11, GreenShift: 5, BlueShift: 0,
	}
	pixel := convertPixel(img, 0, 0, pf)

	// R=255 → 31, G=128 → ~31, B=0 → 0
	r := (pixel >> 11) & 0x1F
	g := (pixel >> 5) & 0x3F
	b := pixel & 0x1F

	assert.Equal(t, uint32(31), r, "max red")
	assert.Greater(t, g, uint32(0), "non-zero green")
	assert.Equal(t, uint32(0), b, "zero blue")
}

func TestSelectEncoder(t *testing.T) {
	tests := []struct {
		name     string
		encs     []int32
		expected int32
	}{
		{"raw_only", []int32{EncodingRaw}, EncodingRaw},
		{"zrle_preferred", []int32{EncodingZRLE, EncodingRaw}, EncodingZRLE},
		{"zrle_only", []int32{EncodingZRLE}, EncodingZRLE},
		{"unknown_fallback", []int32{999, 998}, EncodingRaw},
		{"empty_fallback", []int32{}, EncodingRaw},
		{"mixed", []int32{EncodingCopyRect, EncodingRRE, EncodingZRLE, EncodingRaw}, EncodingZRLE},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enc := SelectEncoder(tt.encs)
			assert.Equal(t, tt.expected, enc.Type())
		})
	}
}

func TestCpixelSize(t *testing.T) {
	// 32bpp, depth 24, true color → 3 bytes (CPIXEL)
	pf1 := DefaultPixelFormat()
	assert.Equal(t, 3, cpixelSize(pf1))

	// 16bpp → 2 bytes
	pf2 := PixelFormat{BitsPerPixel: 16, Depth: 16, TrueColorFlag: 1}
	assert.Equal(t, 2, cpixelSize(pf2))

	// 8bpp → 1 byte
	pf3 := PixelFormat{BitsPerPixel: 8, Depth: 8, TrueColorFlag: 1}
	assert.Equal(t, 1, cpixelSize(pf3))

	// 32bpp, depth 32 → 4 bytes (not CPIXEL)
	pf4 := PixelFormat{BitsPerPixel: 32, Depth: 32, TrueColorFlag: 1}
	assert.Equal(t, 4, cpixelSize(pf4))
}
