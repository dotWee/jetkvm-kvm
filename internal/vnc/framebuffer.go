package vnc

import (
	"image"
	"sync"
)

// Framebuffer represents a thread-safe pixel buffer for the VNC server.
// Pixels are stored in BGRA format (4 bytes per pixel) as expected by the RFB protocol.
type Framebuffer struct {
	mu     sync.RWMutex
	width  int
	height int
	pixels []byte // BGRA pixel data, length = width * height * 4
}

// NewFramebuffer creates a new framebuffer with the given dimensions.
func NewFramebuffer(width, height int) *Framebuffer {
	return &Framebuffer{
		width:  width,
		height: height,
		pixels: make([]byte, width*height*4),
	}
}

// Width returns the framebuffer width.
func (fb *Framebuffer) Width() int {
	fb.mu.RLock()
	defer fb.mu.RUnlock()
	return fb.width
}

// Height returns the framebuffer height.
func (fb *Framebuffer) Height() int {
	fb.mu.RLock()
	defer fb.mu.RUnlock()
	return fb.height
}

// Update replaces the framebuffer contents from an RGBA image.
func (fb *Framebuffer) Update(img *image.RGBA) {
	fb.mu.Lock()
	defer fb.mu.Unlock()

	bounds := img.Bounds()
	newWidth := bounds.Dx()
	newHeight := bounds.Dy()

	if newWidth != fb.width || newHeight != fb.height {
		fb.width = newWidth
		fb.height = newHeight
		fb.pixels = make([]byte, newWidth*newHeight*4)
	}

	// Convert RGBA to BGRA for RFB protocol
	src := img.Pix
	for i := 0; i < len(src); i += 4 {
		fb.pixels[i+0] = src[i+2] // B
		fb.pixels[i+1] = src[i+1] // G
		fb.pixels[i+2] = src[i+0] // R
		fb.pixels[i+3] = src[i+3] // A
	}
}

// GetRect returns the pixel data for the specified rectangle region.
// The returned data is in BGRA format.
// Coordinates are clamped to framebuffer bounds.
func (fb *Framebuffer) GetRect(x, y, width, height int) []byte {
	fb.mu.RLock()
	defer fb.mu.RUnlock()

	// Clamp to framebuffer bounds
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	if x+width > fb.width {
		width = fb.width - x
	}
	if y+height > fb.height {
		height = fb.height - y
	}
	if width <= 0 || height <= 0 {
		return nil
	}

	data := make([]byte, width*height*4)
	for row := 0; row < height; row++ {
		srcOffset := ((y + row) * fb.width + x) * 4
		dstOffset := row * width * 4
		copy(data[dstOffset:dstOffset+width*4], fb.pixels[srcOffset:srcOffset+width*4])
	}
	return data
}

// Resize changes the framebuffer dimensions, clearing the content.
func (fb *Framebuffer) Resize(width, height int) {
	fb.mu.Lock()
	defer fb.mu.Unlock()

	fb.width = width
	fb.height = height
	fb.pixels = make([]byte, width*height*4)
}
