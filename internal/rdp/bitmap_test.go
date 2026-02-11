package rdp

import (
	"image"
	"image/color"
	"testing"
)

func TestEncodeFrameNil(t *testing.T) {
	updates := EncodeFrame(nil, 64)
	if updates != nil {
		t.Errorf("expected nil for nil image, got %d updates", len(updates))
	}
}

func TestEncodeFrameSmallImage(t *testing.T) {
	img := NewTestImage(4, 4, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	updates := EncodeFrame(img, 64)

	if len(updates) != 1 {
		t.Fatalf("expected 1 update, got %d", len(updates))
	}

	u := updates[0]
	if u.Width != 4 || u.Height != 4 {
		t.Errorf("tile size = %dx%d, want 4x4", u.Width, u.Height)
	}
	if u.BitsPerPixel != 24 {
		t.Errorf("bitsPerPixel = %d, want 24", u.BitsPerPixel)
	}
	if len(u.Data) == 0 {
		t.Error("expected non-empty bitmap data")
	}
}

func TestEncodeFrameTiling(t *testing.T) {
	// 128x128 image with tile size 64 should produce 4 tiles
	img := NewTestImage(128, 128, color.RGBA{G: 255, A: 255})
	updates := EncodeFrame(img, 64)

	if len(updates) != 4 {
		t.Fatalf("expected 4 tiles, got %d", len(updates))
	}

	// Verify tile positions
	positions := make(map[[2]uint16]bool)
	for _, u := range updates {
		positions[[2]uint16{u.X, u.Y}] = true
		if u.Width != 64 || u.Height != 64 {
			t.Errorf("tile at (%d,%d) size = %dx%d, want 64x64", u.X, u.Y, u.Width, u.Height)
		}
	}

	expected := [][2]uint16{{0, 0}, {64, 0}, {0, 64}, {64, 64}}
	for _, pos := range expected {
		if !positions[pos] {
			t.Errorf("missing tile at position (%d, %d)", pos[0], pos[1])
		}
	}
}

func TestEncodeFramePartialTiles(t *testing.T) {
	// 100x100 with tile size 64: 2x2 grid but last tiles are partial
	img := NewTestImage(100, 100, color.RGBA{B: 255, A: 255})
	updates := EncodeFrame(img, 64)

	if len(updates) != 4 {
		t.Fatalf("expected 4 tiles, got %d", len(updates))
	}

	// Check the partial tile sizes
	for _, u := range updates {
		if u.X == 64 && u.Width != 36 {
			t.Errorf("right tile width = %d, want 36", u.Width)
		}
		if u.Y == 64 && u.Height != 36 {
			t.Errorf("bottom tile height = %d, want 36", u.Height)
		}
	}
}

func TestEncodeTileBGRColorOrder(t *testing.T) {
	// Single pixel red image
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.SetRGBA(0, 0, color.RGBA{R: 0xAA, G: 0xBB, B: 0xCC, A: 0xFF})

	data := encodeTileBGR(img, 0, 0, 1, 1)

	// BGR order: Blue first, then Green, then Red
	if len(data) < 3 {
		t.Fatalf("data too short: %d", len(data))
	}
	if data[0] != 0xCC { // Blue
		t.Errorf("B = 0x%02X, want 0xCC", data[0])
	}
	if data[1] != 0xBB { // Green
		t.Errorf("G = 0x%02X, want 0xBB", data[1])
	}
	if data[2] != 0xAA { // Red
		t.Errorf("R = 0x%02X, want 0xAA", data[2])
	}
}

func TestEncodeTileBGRBottomUp(t *testing.T) {
	// 1x2 image: top pixel red, bottom pixel blue
	img := image.NewRGBA(image.Rect(0, 0, 1, 2))
	img.SetRGBA(0, 0, color.RGBA{R: 255, A: 255}) // top = red
	img.SetRGBA(0, 1, color.RGBA{B: 255, A: 255}) // bottom = blue

	data := encodeTileBGR(img, 0, 0, 1, 2)

	// Row stride: 1 pixel * 3 bytes = 3, padded to 4 bytes
	rowStride := 4

	// Bottom-up: first row in output = bottom pixel (blue)
	if data[0] != 0xFF { // Blue channel of bottom pixel
		t.Errorf("row0 B = 0x%02X, want 0xFF", data[0])
	}
	// Second row in output = top pixel (red)
	if data[rowStride+2] != 0xFF { // Red channel of top pixel
		t.Errorf("row1 R = 0x%02X, want 0xFF", data[rowStride+2])
	}
}

func TestDiffFramesNilPrev(t *testing.T) {
	curr := NewTestImage(64, 64, color.RGBA{R: 255, A: 255})
	updates := DiffFrames(nil, curr, 64)

	// Should return full frame
	if len(updates) != 1 {
		t.Fatalf("expected 1 update for nil prev, got %d", len(updates))
	}
}

func TestDiffFramesNilCurr(t *testing.T) {
	prev := NewTestImage(64, 64, color.RGBA{R: 255, A: 255})
	updates := DiffFrames(prev, nil, 64)
	if updates != nil {
		t.Errorf("expected nil for nil curr, got %d updates", len(updates))
	}
}

func TestDiffFramesIdentical(t *testing.T) {
	img := NewTestImage(64, 64, color.RGBA{R: 255, A: 255})
	updates := DiffFrames(img, img, 64)

	if len(updates) != 0 {
		t.Errorf("expected 0 updates for identical frames, got %d", len(updates))
	}
}

func TestDiffFramesChanged(t *testing.T) {
	prev := NewTestImage(128, 128, color.RGBA{R: 255, A: 255})
	curr := NewTestImage(128, 128, color.RGBA{R: 255, A: 255})

	// Change one pixel in the top-left tile
	curr.SetRGBA(10, 10, color.RGBA{G: 255, A: 255})

	updates := DiffFrames(prev, curr, 64)

	if len(updates) != 1 {
		t.Fatalf("expected 1 changed tile, got %d", len(updates))
	}
	if updates[0].X != 0 || updates[0].Y != 0 {
		t.Errorf("changed tile at (%d,%d), want (0,0)", updates[0].X, updates[0].Y)
	}
}

func TestBuildBitmapUpdatePDUEmpty(t *testing.T) {
	data := BuildBitmapUpdatePDU(nil, 1)
	if data != nil {
		t.Error("expected nil for empty updates")
	}
}

func TestBuildBitmapUpdatePDU(t *testing.T) {
	updates := []BitmapUpdate{
		{
			X: 0, Y: 0, Width: 2, Height: 2,
			BitsPerPixel: 24,
			Data:         make([]byte, 16),
		},
	}

	data := BuildBitmapUpdatePDU(updates, 0x12345678)
	if len(data) == 0 {
		t.Fatal("expected non-empty PDU data")
	}

	// Verify share data header
	totalLen := getU16LE(data, 0)
	if int(totalLen) != len(data) {
		t.Errorf("totalLength = %d, actual = %d", totalLen, len(data))
	}
	pduType := getU16LE(data, 2)
	if pduType != pduTypeData {
		t.Errorf("pduType = 0x%04X, want 0x%04X", pduType, pduTypeData)
	}
	shareID := getU32LE(data, 6)
	if shareID != 0x12345678 {
		t.Errorf("shareID = 0x%08X, want 0x12345678", shareID)
	}
}

func TestNewTestImage(t *testing.T) {
	c := color.RGBA{R: 128, G: 64, B: 32, A: 255}
	img := NewTestImage(10, 20, c)

	bounds := img.Bounds()
	if bounds.Dx() != 10 || bounds.Dy() != 20 {
		t.Errorf("size = %dx%d, want 10x20", bounds.Dx(), bounds.Dy())
	}

	// Check a sample pixel
	got := img.RGBAAt(5, 10)
	if got != c {
		t.Errorf("pixel = %v, want %v", got, c)
	}
}

func TestEncodeFrameDefaultTileSize(t *testing.T) {
	img := NewTestImage(32, 32, color.RGBA{R: 100, A: 255})

	// tileSize 0 should default to 64
	updates := EncodeFrame(img, 0)
	if len(updates) != 1 {
		t.Errorf("expected 1 tile with default tile size, got %d", len(updates))
	}
}

func TestTileChanged(t *testing.T) {
	a := NewTestImage(64, 64, color.RGBA{R: 255, A: 255})
	b := NewTestImage(64, 64, color.RGBA{R: 255, A: 255})

	if tileChanged(a, b, 0, 0, 64, 64) {
		t.Error("identical images should not report change")
	}

	b.SetRGBA(32, 32, color.RGBA{G: 255, A: 255})
	if !tileChanged(a, b, 0, 0, 64, 64) {
		t.Error("different images should report change")
	}
}
