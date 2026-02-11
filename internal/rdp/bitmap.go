package rdp

import (
	"image"
	"image/color"
)

// BitmapUpdate represents a rectangular bitmap update to send to the client.
type BitmapUpdate struct {
	X, Y          uint16
	Width, Height uint16
	BitsPerPixel  uint16
	Data          []byte
}

// EncodeFrame converts an RGBA image to a list of bitmap updates suitable
// for sending over RDP. The image is split into tiles of the given size to
// keep individual update packets manageable.
func EncodeFrame(img *image.RGBA, tileSize int) []BitmapUpdate {
	if img == nil {
		return nil
	}
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()

	if tileSize <= 0 {
		tileSize = 64
	}

	var updates []BitmapUpdate

	for y := 0; y < h; y += tileSize {
		for x := 0; x < w; x += tileSize {
			tw := tileSize
			th := tileSize
			if x+tw > w {
				tw = w - x
			}
			if y+th > h {
				th = h - y
			}

			data := encodeTileBGR(img, x, y, tw, th)

			updates = append(updates, BitmapUpdate{
				X:            uint16(x),
				Y:            uint16(y),
				Width:        uint16(tw),
				Height:       uint16(th),
				BitsPerPixel: 24, // 24-bit BGR
				Data:         data,
			})
		}
	}

	return updates
}

// encodeTileBGR extracts a tile from the image and encodes it as BGR bytes
// in bottom-up row order (as expected by RDP bitmap updates).
func encodeTileBGR(img *image.RGBA, ox, oy, tw, th int) []byte {
	// RDP bitmaps use bottom-up row order
	rowBytes := tw * 3
	// Rows must be aligned to 4-byte boundary
	rowPad := (4 - (rowBytes % 4)) % 4
	paddedRow := rowBytes + rowPad

	data := make([]byte, paddedRow*th)

	for row := 0; row < th; row++ {
		// bottom-up: row 0 in output = last row of tile
		srcY := oy + th - 1 - row
		dstOffset := row * paddedRow

		for col := 0; col < tw; col++ {
			srcX := ox + col
			r, g, b, _ := img.At(srcX, srcY).RGBA()
			// BGR order, 8 bits per channel
			data[dstOffset+col*3+0] = uint8(b >> 8)
			data[dstOffset+col*3+1] = uint8(g >> 8)
			data[dstOffset+col*3+2] = uint8(r >> 8)
		}
	}

	return data
}

// BuildBitmapUpdatePDU builds an RDP bitmap update PDU from a set of bitmap updates.
// This creates the share data header + bitmap update data structure.
func BuildBitmapUpdatePDU(updates []BitmapUpdate, shareID uint32) []byte {
	if len(updates) == 0 {
		return nil
	}

	// Calculate total data size
	var bitmapDataSize int
	for _, u := range updates {
		// Each bitmap data entry: 18 bytes header + pixel data
		bitmapDataSize += 18 + len(u.Data)
	}

	// Build the bitmap update data
	// updateType (2) + numberRectangles (2) + bitmap data entries
	updateData := make([]byte, 0, 4+bitmapDataSize)
	updateData = appendU16LE(updateData, 0x0001) // UPDATETYPE_BITMAP
	updateData = appendU16LE(updateData, uint16(len(updates)))

	for _, u := range updates {
		updateData = appendU16LE(updateData, u.X)               // destLeft
		updateData = appendU16LE(updateData, u.Y)               // destTop
		updateData = appendU16LE(updateData, u.X+u.Width-1)     // destRight
		updateData = appendU16LE(updateData, u.Y+u.Height-1)    // destBottom
		updateData = appendU16LE(updateData, u.Width)            // width
		updateData = appendU16LE(updateData, u.Height)           // height
		updateData = appendU16LE(updateData, u.BitsPerPixel)     // bitsPerPixel
		updateData = appendU16LE(updateData, bitmapCompNone)     // flags (uncompressed)
		updateData = appendU16LE(updateData, uint16(len(u.Data))) // bitmapLength
		updateData = append(updateData, u.Data...)
	}

	return buildShareDataPDU(shareID, pduTypeDataBitmapUp, updateData)
}

// buildShareDataPDU wraps data in a share control + share data header.
func buildShareDataPDU(shareID uint32, pduType2 uint8, data []byte) []byte {
	// Share Control Header: totalLength(2) + pduType(2) + pduSource(2) = 6
	// Share Data Header: shareId(4) + pad(1) + streamId(1) + uncompressedLength(2)
	//                   + pduType2(1) + compressedType(1) + compressedLength(2) = 12
	totalLen := 6 + 12 + len(data)

	buf := make([]byte, 0, totalLen)
	buf = appendU16LE(buf, uint16(totalLen)) // totalLength
	buf = appendU16LE(buf, pduTypeData)      // pduType
	buf = appendU16LE(buf, 0)                // pduSource (server channel)

	buf = appendU32LE(buf, shareID) // shareId
	buf = append(buf, 0)           // pad1
	buf = append(buf, 0x01)        // streamId (STREAM_LOW)
	buf = appendU16LE(buf, uint16(len(data)+4)) // uncompressedLength
	buf = append(buf, pduType2) // pduType2
	buf = append(buf, 0)       // compressedType
	buf = appendU16LE(buf, 0)  // compressedLength

	buf = append(buf, data...)
	return buf
}

// DiffFrames compares two RGBA images and returns only the tiles that changed.
// This enables incremental updates to reduce bandwidth.
func DiffFrames(prev, curr *image.RGBA, tileSize int) []BitmapUpdate {
	if curr == nil {
		return nil
	}
	if prev == nil {
		return EncodeFrame(curr, tileSize)
	}

	bounds := curr.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()

	if tileSize <= 0 {
		tileSize = 64
	}

	var updates []BitmapUpdate

	for y := 0; y < h; y += tileSize {
		for x := 0; x < w; x += tileSize {
			tw := tileSize
			th := tileSize
			if x+tw > w {
				tw = w - x
			}
			if y+th > h {
				th = h - y
			}

			if tileChanged(prev, curr, x, y, tw, th) {
				data := encodeTileBGR(curr, x, y, tw, th)
				updates = append(updates, BitmapUpdate{
					X:            uint16(x),
					Y:            uint16(y),
					Width:        uint16(tw),
					Height:       uint16(th),
					BitsPerPixel: 24,
					Data:         data,
				})
			}
		}
	}

	return updates
}

// tileChanged checks whether a tile region differs between two images.
func tileChanged(a, b *image.RGBA, ox, oy, tw, th int) bool {
	for y := oy; y < oy+th; y++ {
		for x := ox; x < ox+tw; x++ {
			if a.At(x, y) != b.At(x, y) {
				return true
			}
		}
	}
	return false
}

// NewTestImage creates a solid-color RGBA image for testing.
func NewTestImage(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}
