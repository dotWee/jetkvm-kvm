package rfb

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"image"
	"io"
)

// Encoder encodes a rectangular region of pixels into RFB wire format.
type Encoder interface {
	// Type returns the encoding type number.
	Type() int32
	// Encode writes the encoded pixel data for the given rectangle.
	// The source image provides the pixel data, and pf describes the
	// target pixel format requested by the client.
	Encode(w io.Writer, img *image.RGBA, rect Rectangle, pf PixelFormat) error
}

// RawEncoder implements Raw encoding (type 0).
type RawEncoder struct{}

func (e *RawEncoder) Type() int32 { return EncodingRaw }

func (e *RawEncoder) Encode(w io.Writer, img *image.RGBA, rect Rectangle, pf PixelFormat) error {
	x0 := int(rect.X)
	y0 := int(rect.Y)
	x1 := x0 + int(rect.Width)
	y1 := y0 + int(rect.Height)

	// Clamp to image bounds
	if x1 > img.Bounds().Max.X {
		x1 = img.Bounds().Max.X
	}
	if y1 > img.Bounds().Max.Y {
		y1 = img.Bounds().Max.Y
	}

	bytesPerPixel := int(pf.BitsPerPixel) / 8
	rowBytes := int(rect.Width) * bytesPerPixel
	buf := make([]byte, rowBytes)

	for y := y0; y < y1; y++ {
		off := 0
		for x := x0; x < x1; x++ {
			pixel := convertPixel(img, x, y, pf)
			switch bytesPerPixel {
			case 4:
				if pf.BigEndianFlag != 0 {
					binary.BigEndian.PutUint32(buf[off:], pixel)
				} else {
					binary.LittleEndian.PutUint32(buf[off:], pixel)
				}
			case 2:
				if pf.BigEndianFlag != 0 {
					binary.BigEndian.PutUint16(buf[off:], uint16(pixel))
				} else {
					binary.LittleEndian.PutUint16(buf[off:], uint16(pixel))
				}
			case 1:
				buf[off] = uint8(pixel)
			}
			off += bytesPerPixel
		}
		if _, err := w.Write(buf[:off]); err != nil {
			return fmt.Errorf("failed to write raw pixel row: %w", err)
		}
	}
	return nil
}

// ZRLEEncoder implements ZRLE encoding (type 16) per RFC 6143 §7.7.6.
type ZRLEEncoder struct {
	zlibWriter *zlib.Writer
	buf        bytes.Buffer
}

func (e *ZRLEEncoder) Type() int32 { return EncodingZRLE }

func (e *ZRLEEncoder) Encode(w io.Writer, img *image.RGBA, rect Rectangle, pf PixelFormat) error {
	e.buf.Reset()

	// Initialize or reset zlib writer
	if e.zlibWriter == nil {
		e.zlibWriter = zlib.NewWriter(&e.buf)
	} else {
		e.zlibWriter.Reset(&e.buf)
	}

	tileW := 64
	tileH := 64

	for ty := int(rect.Y); ty < int(rect.Y)+int(rect.Height); ty += tileH {
		for tx := int(rect.X); tx < int(rect.X)+int(rect.Width); tx += tileW {
			tw := min(tileW, int(rect.X)+int(rect.Width)-tx)
			th := min(tileH, int(rect.Y)+int(rect.Height)-ty)

			if err := e.encodeTile(img, tx, ty, tw, th, pf); err != nil {
				return err
			}
		}
	}

	if err := e.zlibWriter.Flush(); err != nil {
		return fmt.Errorf("failed to flush zlib: %w", err)
	}

	// Write length prefix + compressed data
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(e.buf.Len()))
	if _, err := w.Write(lenBuf); err != nil {
		return fmt.Errorf("failed to write ZRLE length: %w", err)
	}
	if _, err := w.Write(e.buf.Bytes()); err != nil {
		return fmt.Errorf("failed to write ZRLE data: %w", err)
	}

	return nil
}

func (e *ZRLEEncoder) encodeTile(img *image.RGBA, tx, ty, tw, th int, pf PixelFormat) error {
	// Subencoding type: 0 = raw CPIXEL data
	if _, err := e.zlibWriter.Write([]byte{0}); err != nil {
		return err
	}

	cpixelLen := cpixelSize(pf)
	buf := make([]byte, cpixelLen)

	for y := ty; y < ty+th; y++ {
		for x := tx; x < tx+tw; x++ {
			px := x
			py := y
			if px >= img.Bounds().Max.X {
				px = img.Bounds().Max.X - 1
			}
			if py >= img.Bounds().Max.Y {
				py = img.Bounds().Max.Y - 1
			}

			pixel := convertPixel(img, px, py, pf)
			writeCPixel(buf, pixel, pf, cpixelLen)
			if _, err := e.zlibWriter.Write(buf[:cpixelLen]); err != nil {
				return err
			}
		}
	}

	return nil
}

// cpixelSize returns the CPIXEL size for the given pixel format.
// CPIXEL is 3 bytes when bpp=32 and depth<=24, otherwise bpp/8.
func cpixelSize(pf PixelFormat) int {
	if pf.BitsPerPixel == 32 && pf.Depth <= 24 && pf.TrueColorFlag != 0 {
		return 3
	}
	return int(pf.BitsPerPixel) / 8
}

// writeCPixel writes a pixel value as a CPIXEL.
func writeCPixel(buf []byte, pixel uint32, pf PixelFormat, size int) {
	if size == 3 {
		// CPIXEL: least significant 3 bytes in little-endian order
		buf[0] = byte(pixel)
		buf[1] = byte(pixel >> 8)
		buf[2] = byte(pixel >> 16)
	} else if size == 2 {
		if pf.BigEndianFlag != 0 {
			binary.BigEndian.PutUint16(buf, uint16(pixel))
		} else {
			binary.LittleEndian.PutUint16(buf, uint16(pixel))
		}
	} else if size == 4 {
		if pf.BigEndianFlag != 0 {
			binary.BigEndian.PutUint32(buf, pixel)
		} else {
			binary.LittleEndian.PutUint32(buf, pixel)
		}
	} else if size == 1 {
		buf[0] = byte(pixel)
	}
}

// convertPixel reads an RGBA pixel from the source image and converts it
// to the target pixel format, returning a packed uint32.
func convertPixel(img *image.RGBA, x, y int, pf PixelFormat) uint32 {
	idx := (y-img.Rect.Min.Y)*img.Stride + (x-img.Rect.Min.X)*4
	if idx+3 >= len(img.Pix) {
		return 0
	}
	r := uint32(img.Pix[idx])
	g := uint32(img.Pix[idx+1])
	b := uint32(img.Pix[idx+2])

	// Scale to target max values
	rScaled := (r * uint32(pf.RedMax)) / 255
	gScaled := (g * uint32(pf.GreenMax)) / 255
	bScaled := (b * uint32(pf.BlueMax)) / 255

	return (rScaled << uint32(pf.RedShift)) |
		(gScaled << uint32(pf.GreenShift)) |
		(bScaled << uint32(pf.BlueShift))
}

// SelectEncoder chooses the best encoder from the client's supported encodings.
func SelectEncoder(clientEncodings []int32) Encoder {
	for _, enc := range clientEncodings {
		switch enc {
		case EncodingZRLE:
			return &ZRLEEncoder{}
		case EncodingRaw:
			return &RawEncoder{}
		}
	}
	// Default to Raw encoding
	return &RawEncoder{}
}
