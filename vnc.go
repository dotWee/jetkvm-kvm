package kvm

import (
	"fmt"
	"image"
	"image/color"

	"github.com/jetkvm/kvm/internal/rfb"
)

var (
	vncServer *rfb.Server
)

const (
	// vncDefaultWidth and vncDefaultHeight are the initial framebuffer size
	// advertised to VNC clients. They are updated when a real frame arrives.
	vncDefaultWidth  = 1920
	vncDefaultHeight = 1080

	// hidMaxCoord is the maximum USB HID absolute coordinate value.
	hidMaxCoord = 32767
)

// vncFrameProvider implements rfb.FrameProvider by maintaining a
// framebuffer that can be updated from decoded video frames.
type vncFrameProvider struct {
	// currentFrame holds the latest decoded frame. The VNC server may
	// read it from any goroutine so we use atomic-style replacement.
	currentFrame *rfb.FrameData

	width, height uint16
}

func newVNCFrameProvider() *vncFrameProvider {
	return &vncFrameProvider{
		width:  vncDefaultWidth,
		height: vncDefaultHeight,
	}
}

func (p *vncFrameProvider) GetFrame() *rfb.FrameData {
	return p.currentFrame
}

func (p *vncFrameProvider) GetSize() (uint16, uint16) {
	if p.currentFrame != nil {
		return uint16(p.currentFrame.Width), uint16(p.currentFrame.Height)
	}
	return p.width, p.height
}

// UpdateFrame sets a new frame from decoded video data.
func (p *vncFrameProvider) UpdateFrame(img *image.RGBA) {
	bounds := img.Bounds()
	p.currentFrame = &rfb.FrameData{
		Pix:    img.Pix,
		Stride: img.Stride,
		Width:  bounds.Dx(),
		Height: bounds.Dy(),
	}
	p.width = uint16(bounds.Dx())
	p.height = uint16(bounds.Dy())
}

// vncInputHandler implements rfb.InputHandler by translating VNC
// input events to USB HID reports.
type vncInputHandler struct {
	// modifiers tracks currently-held modifier keys as a bitmask.
	modifiers byte
}

func newVNCInputHandler() *vncInputHandler {
	return &vncInputHandler{}
}

// KeyEvent handles a VNC key press/release by mapping the X11 keysym
// to a USB HID Usage ID and sending the appropriate HID report.
func (h *vncInputHandler) KeyEvent(keysym uint32, down bool) {
	hidKey, isModifier, found := rfb.KeysymToHID(keysym)
	if !found {
		vncLogger.Debug().Uint32("keysym", keysym).Msg("unmapped keysym")
		return
	}

	if isModifier {
		if down {
			h.modifiers |= hidKey
		} else {
			h.modifiers &^= hidKey
		}
		// Send a keyboard report with current modifiers and no keys
		if err := rpcKeyboardReport(h.modifiers, []byte{0, 0, 0, 0, 0, 0}); err != nil {
			vncLogger.Warn().Err(err).Msg("failed to send modifier report")
		}
		return
	}

	if err := rpcKeypressReport(hidKey, down); err != nil {
		vncLogger.Warn().Err(err).Msg("failed to send key report")
	}
}

// PointerEvent handles a VNC pointer event by scaling coordinates from
// VNC pixel space to USB HID absolute coordinate space (0-32767) and
// sending a mouse report.
func (h *vncInputHandler) PointerEvent(buttonMask uint8, x, y uint16) {
	// Get current framebuffer dimensions
	w, ht := vncFrameProv.GetSize()
	if w == 0 || ht == 0 {
		return
	}

	// Handle scroll wheel (VNC buttons 4 and 5)
	if buttonMask&0x08 != 0 { // button 4 = scroll up
		if err := rpcWheelReport(1); err != nil {
			vncLogger.Warn().Err(err).Msg("failed to send wheel report")
		}
	}
	if buttonMask&0x10 != 0 { // button 5 = scroll down
		if err := rpcWheelReport(-1); err != nil {
			vncLogger.Warn().Err(err).Msg("failed to send wheel report")
		}
	}

	// Scale VNC coordinates to HID range
	hidX := int(x) * hidMaxCoord / int(w)
	hidY := int(y) * hidMaxCoord / int(ht)

	// Clamp to valid range
	if hidX > hidMaxCoord {
		hidX = hidMaxCoord
	}
	if hidY > hidMaxCoord {
		hidY = hidMaxCoord
	}

	// Map VNC buttons (1=left,2=middle,3=right) to USB HID buttons
	// VNC: bit0=left, bit1=middle, bit2=right
	// HID: bit0=left, bit1=right, bit2=middle
	hidButtons := uint8(0)
	if buttonMask&0x01 != 0 {
		hidButtons |= 0x01 // left
	}
	if buttonMask&0x02 != 0 {
		hidButtons |= 0x04 // middle → bit2
	}
	if buttonMask&0x04 != 0 {
		hidButtons |= 0x02 // right → bit1
	}

	if err := rpcAbsMouseReport(hidX, hidY, hidButtons); err != nil {
		vncLogger.Warn().Err(err).Msg("failed to send mouse report")
	}
}

var vncFrameProv *vncFrameProvider

// initVNC initializes and starts the VNC server if enabled in config.
func initVNC() {
	if !config.VNCEnabled {
		vncLogger.Info().Msg("VNC server disabled")
		return
	}

	vncFrameProv = newVNCFrameProvider()

	// Generate a placeholder frame (gray screen) until real video arrives
	placeholderImg := image.NewRGBA(image.Rect(0, 0, vncDefaultWidth, vncDefaultHeight))
	gray := color.RGBA{R: 64, G: 64, B: 64, A: 255}
	for y := range vncDefaultHeight {
		for x := range vncDefaultWidth {
			placeholderImg.Set(x, y, gray)
		}
	}
	vncFrameProv.UpdateFrame(placeholderImg)

	// Configure security handlers
	var securityHandlers []rfb.SecurityHandler
	if config.VNCPassword != "" {
		password := config.VNCPassword
		securityHandlers = append(securityHandlers, &rfb.SecurityVNCAuth{
			Password: []byte(password),
		})
	}
	securityHandlers = append(securityHandlers, &rfb.SecurityNone{})

	addr := fmt.Sprintf(":%d", config.VNCPort)
	vncServer = rfb.NewServer(rfb.ServerConfig{
		Addr:             addr,
		Name:             "JetKVM",
		FrameProvider:    vncFrameProv,
		Input:            newVNCInputHandler(),
		SecurityHandlers: securityHandlers,
		Logger:           *vncLogger,
		OnClientConnected: func() {
			if incrActiveSessions() == 1 {
				vncLogger.Info().Msg("first session connected via VNC, starting video")
				onFirstSessionConnected()
			}
			onActiveSessionsChanged()
		},
		OnClientDisconnected: func() {
			if decrActiveSessions() == 0 {
				vncLogger.Info().Msg("last session disconnected via VNC, stopping video")
				onLastSessionDisconnected()
			}
			onActiveSessionsChanged()
		},
	})

	go func() {
		vncLogger.Info().Str("addr", addr).Msg("starting VNC server")
		if err := vncServer.Listen(appCtx); err != nil {
			vncLogger.Error().Err(err).Msg("VNC server error")
		}
	}()
}

// stopVNC stops the VNC server if running.
func stopVNC() {
	if vncServer != nil {
		vncServer.Close()
		vncServer = nil
		vncLogger.Info().Msg("VNC server stopped")
	}
}

// restartVNC restarts the VNC server with current config.
func restartVNC() {
	stopVNC()
	initVNC()
}

// --- JSON-RPC handlers for VNC ---

type VNCState struct {
	Enabled          bool `json:"enabled"`
	Port             int  `json:"port"`
	HasPassword      bool `json:"hasPassword"`
	ConnectedClients int  `json:"connectedClients"`
}

func rpcGetVNCState() (VNCState, error) {
	connectedClients := 0
	if vncServer != nil {
		connectedClients = vncServer.ConnectedClients()
	}
	return VNCState{
		Enabled:          config.VNCEnabled,
		Port:             config.VNCPort,
		HasPassword:      config.VNCPassword != "",
		ConnectedClients: connectedClients,
	}, nil
}

func rpcSetVNCEnabled(enabled bool) error {
	config.VNCEnabled = enabled
	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	if enabled {
		if vncServer == nil {
			initVNC()
		}
	} else {
		stopVNC()
	}
	return nil
}

func rpcSetVNCPort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid port number: %d", port)
	}
	config.VNCPort = port
	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	if config.VNCEnabled {
		restartVNC()
	}
	return nil
}

func rpcSetVNCPassword(password string) error {
	config.VNCPassword = password
	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	if config.VNCEnabled {
		restartVNC()
	}
	return nil
}
