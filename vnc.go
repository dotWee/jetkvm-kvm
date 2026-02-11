package kvm

import (
	"fmt"
	"net"

	"github.com/jetkvm/kvm/internal/vnc"
)

var vncServer *vnc.Server

// vncInputHandler forwards VNC input events to the USB HID gadget.
type vncInputHandler struct{}

func (h *vncInputHandler) HandleKeyEvent(keysym uint32, pressed bool) {
	hidCode, ok := mapKeysymToHID(keysym)
	if !ok {
		return
	}
	if err := rpcKeypressReport(hidCode, pressed); err != nil {
		vncLogger.Warn().Err(err).Uint32("keysym", keysym).Msg("failed to send key event")
	}
}

func (h *vncInputHandler) HandlePointerEvent(buttonMask uint8, x, y uint16) {
	// Map VNC button mask to USB HID button format
	// VNC: bit 0=left, bit 1=middle, bit 2=right
	// USB HID absolute mouse: same mapping
	if err := rpcAbsMouseReport(int(x), int(y), buttonMask); err != nil {
		vncLogger.Warn().Err(err).Msg("failed to send pointer event")
	}
}

// VNCConfig holds the VNC server configuration returned by RPC.
type VNCConfig struct {
	Enabled  bool   `json:"enabled"`
	Port     int    `json:"port"`
	Password string `json:"password"`
}

func rpcGetVNCConfig() VNCConfig {
	return VNCConfig{
		Enabled:  config.VNCEnabled,
		Port:     config.VNCPort,
		Password: config.VNCPassword,
	}
}

func rpcSetVNCConfig(vncConfig VNCConfig) error {
	if vncConfig.Port < 1 || vncConfig.Port > 65535 {
		return fmt.Errorf("invalid VNC port: %d", vncConfig.Port)
	}

	if len(vncConfig.Password) > 8 {
		vncLogger.Warn().Msg("VNC password exceeds 8 characters; only the first 8 will be used per the RFB protocol")
	}

	config.VNCEnabled = vncConfig.Enabled
	config.VNCPort = vncConfig.Port
	config.VNCPassword = vncConfig.Password

	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	// Restart VNC server with new settings
	restartVNCServer()
	return nil
}

func startVNCServer() {
	if !config.VNCEnabled {
		vncLogger.Info().Msg("VNC server is disabled")
		return
	}

	port := config.VNCPort
	if port == 0 {
		port = 5900
	}

	fb := vnc.NewFramebuffer(1920, 1080)
	if lastVideoState.Width > 0 && lastVideoState.Height > 0 {
		fb = vnc.NewFramebuffer(lastVideoState.Width, lastVideoState.Height)
	}

	opts := []vnc.Option{
		vnc.WithLogger(&vncLogger),
	}
	if config.VNCPassword != "" {
		opts = append(opts, vnc.WithPassword(config.VNCPassword))
	}

	vncServer = vnc.NewServer(fb, &vncInputHandler{}, opts...)

	addr := fmt.Sprintf(":%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		vncLogger.Error().Err(err).Str("addr", addr).Msg("failed to start VNC server listener")
		return
	}

	vncLogger.Info().Str("addr", addr).Msg("starting VNC server")

	go func() {
		if err := vncServer.Serve(listener); err != nil {
			vncLogger.Info().Err(err).Msg("VNC server stopped")
		}
	}()
}

func stopVNCServer() {
	if vncServer != nil {
		vncLogger.Info().Msg("stopping VNC server")
		if err := vncServer.Close(); err != nil {
			vncLogger.Warn().Err(err).Msg("error closing VNC server")
		}
		vncServer = nil
	}
}

func restartVNCServer() {
	stopVNCServer()
	startVNCServer()
}

// mapKeysymToHID maps an X11 keysym to a USB HID keycode.
// Returns the HID keycode and true if the mapping exists.
func mapKeysymToHID(keysym uint32) (byte, bool) {
	// Common keysym to HID usage ID mappings
	code, ok := keysymToHIDMap[keysym]
	return code, ok
}

// keysymToHIDMap maps X11 keysyms to USB HID usage IDs.
// Reference: USB HID Usage Tables (https://usb.org/sites/default/files/hut1_4.pdf)
var keysymToHIDMap = map[uint32]byte{
	// Letters (a-z)
	0x0061: 0x04, // a
	0x0062: 0x05, // b
	0x0063: 0x06, // c
	0x0064: 0x07, // d
	0x0065: 0x08, // e
	0x0066: 0x09, // f
	0x0067: 0x0a, // g
	0x0068: 0x0b, // h
	0x0069: 0x0c, // i
	0x006a: 0x0d, // j
	0x006b: 0x0e, // k
	0x006c: 0x0f, // l
	0x006d: 0x10, // m
	0x006e: 0x11, // n
	0x006f: 0x12, // o
	0x0070: 0x13, // p
	0x0071: 0x14, // q
	0x0072: 0x15, // r
	0x0073: 0x16, // s
	0x0074: 0x17, // t
	0x0075: 0x18, // u
	0x0076: 0x19, // v
	0x0077: 0x1a, // w
	0x0078: 0x1b, // x
	0x0079: 0x1c, // y
	0x007a: 0x1d, // z

	// Uppercase (A-Z) map to same HID codes as lowercase
	0x0041: 0x04, // A
	0x0042: 0x05, // B
	0x0043: 0x06, // C
	0x0044: 0x07, // D
	0x0045: 0x08, // E
	0x0046: 0x09, // F
	0x0047: 0x0a, // G
	0x0048: 0x0b, // H
	0x0049: 0x0c, // I
	0x004a: 0x0d, // J
	0x004b: 0x0e, // K
	0x004c: 0x0f, // L
	0x004d: 0x10, // M
	0x004e: 0x11, // N
	0x004f: 0x12, // O
	0x0050: 0x13, // P
	0x0051: 0x14, // Q
	0x0052: 0x15, // R
	0x0053: 0x16, // S
	0x0054: 0x17, // T
	0x0055: 0x18, // U
	0x0056: 0x19, // V
	0x0057: 0x1a, // W
	0x0058: 0x1b, // X
	0x0059: 0x1c, // Y
	0x005a: 0x1d, // Z

	// Numbers (0-9)
	0x0030: 0x27, // 0
	0x0031: 0x1e, // 1
	0x0032: 0x1f, // 2
	0x0033: 0x20, // 3
	0x0034: 0x21, // 4
	0x0035: 0x22, // 5
	0x0036: 0x23, // 6
	0x0037: 0x24, // 7
	0x0038: 0x25, // 8
	0x0039: 0x26, // 9

	// Function keys
	0xff51: 0x50, // Left
	0xff52: 0x52, // Up
	0xff53: 0x4f, // Right
	0xff54: 0x51, // Down
	0xff0d: 0x28, // Return
	0xff1b: 0x29, // Escape
	0xff09: 0x2b, // Tab
	0xff08: 0x2a, // BackSpace
	0xffff: 0x4c, // Delete
	0xff63: 0x49, // Insert
	0xff50: 0x4a, // Home
	0xff57: 0x4d, // End
	0xff55: 0x4b, // Page_Up
	0xff56: 0x4e, // Page_Down

	// F1-F12
	0xffbe: 0x3a, // F1
	0xffbf: 0x3b, // F2
	0xffc0: 0x3c, // F3
	0xffc1: 0x3d, // F4
	0xffc2: 0x3e, // F5
	0xffc3: 0x3f, // F6
	0xffc4: 0x40, // F7
	0xffc5: 0x41, // F8
	0xffc6: 0x42, // F9
	0xffc7: 0x43, // F10
	0xffc8: 0x44, // F11
	0xffc9: 0x45, // F12

	// Modifiers (sent as regular keys; the VNC client handles modifier state)
	0xffe1: 0xe1, // Shift_L
	0xffe2: 0xe5, // Shift_R
	0xffe3: 0xe0, // Control_L
	0xffe4: 0xe4, // Control_R
	0xffe9: 0xe2, // Alt_L
	0xffea: 0xe6, // Alt_R
	0xffeb: 0xe3, // Super_L (Windows/Meta)
	0xffec: 0xe7, // Super_R

	// Symbols
	0x0020: 0x2c, // space
	0x002d: 0x2d, // minus
	0x003d: 0x2e, // equal
	0x005b: 0x2f, // bracketleft
	0x005d: 0x30, // bracketright
	0x005c: 0x31, // backslash
	0x003b: 0x33, // semicolon
	0x0027: 0x34, // apostrophe
	0x0060: 0x35, // grave
	0x002c: 0x36, // comma
	0x002e: 0x37, // period
	0x002f: 0x38, // slash

	// Caps/Num/Scroll Lock
	0xffe5: 0x39, // Caps_Lock
	0xff7f: 0x53, // Num_Lock
	0xff14: 0x47, // Scroll_Lock
	0xff61: 0x46, // Print_Screen / SysRq
	0xff13: 0x48, // Pause
}
