package rfb

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKeysymToHID_Letters(t *testing.T) {
	// Lowercase a-z
	for ch := byte('a'); ch <= byte('z'); ch++ {
		hid, isMod, found := KeysymToHID(uint32(ch))
		assert.True(t, found, "keysym 0x%02X ('%c') should be mapped", ch, ch)
		assert.False(t, isMod, "letter '%c' should not be a modifier", ch)
		assert.Equal(t, byte(0x04+(ch-'a')), hid, "letter '%c' HID code", ch)
	}

	// Uppercase A-Z
	for ch := byte('A'); ch <= byte('Z'); ch++ {
		hid, isMod, found := KeysymToHID(uint32(ch))
		assert.True(t, found, "keysym 0x%02X ('%c') should be mapped", ch, ch)
		assert.False(t, isMod, "letter '%c' should not be a modifier", ch)
		assert.Equal(t, byte(0x04+(ch-'A')), hid, "letter '%c' HID code", ch)
	}
}

func TestKeysymToHID_Digits(t *testing.T) {
	expected := map[uint32]byte{
		0x31: 0x1E, // 1
		0x32: 0x1F, // 2
		0x33: 0x20, // 3
		0x34: 0x21, // 4
		0x35: 0x22, // 5
		0x36: 0x23, // 6
		0x37: 0x24, // 7
		0x38: 0x25, // 8
		0x39: 0x26, // 9
		0x30: 0x27, // 0
	}

	for ks, expectedHID := range expected {
		hid, isMod, found := KeysymToHID(ks)
		assert.True(t, found, "digit keysym 0x%04X", ks)
		assert.False(t, isMod)
		assert.Equal(t, expectedHID, hid, "digit keysym 0x%04X", ks)
	}
}

func TestKeysymToHID_FunctionKeys(t *testing.T) {
	fKeys := []struct {
		keysym uint32
		hid    byte
		name   string
	}{
		{0xFFBE, 0x3A, "F1"},
		{0xFFBF, 0x3B, "F2"},
		{0xFFC0, 0x3C, "F3"},
		{0xFFC1, 0x3D, "F4"},
		{0xFFC2, 0x3E, "F5"},
		{0xFFC3, 0x3F, "F6"},
		{0xFFC4, 0x40, "F7"},
		{0xFFC5, 0x41, "F8"},
		{0xFFC6, 0x42, "F9"},
		{0xFFC7, 0x43, "F10"},
		{0xFFC8, 0x44, "F11"},
		{0xFFC9, 0x45, "F12"},
	}

	for _, tt := range fKeys {
		hid, isMod, found := KeysymToHID(tt.keysym)
		assert.True(t, found, "%s", tt.name)
		assert.False(t, isMod, "%s should not be modifier", tt.name)
		assert.Equal(t, tt.hid, hid, "%s HID code", tt.name)
	}
}

func TestKeysymToHID_SpecialKeys(t *testing.T) {
	specials := []struct {
		keysym uint32
		hid    byte
		name   string
	}{
		{0xFF0D, 0x28, "Return"},
		{0xFF1B, 0x29, "Escape"},
		{0xFF08, 0x2A, "BackSpace"},
		{0xFF09, 0x2B, "Tab"},
		{0x0020, 0x2C, "Space"},
	}

	for _, tt := range specials {
		hid, isMod, found := KeysymToHID(tt.keysym)
		assert.True(t, found, "%s", tt.name)
		assert.False(t, isMod, "%s", tt.name)
		assert.Equal(t, tt.hid, hid, "%s", tt.name)
	}
}

func TestKeysymToHID_NavigationKeys(t *testing.T) {
	navKeys := []struct {
		keysym uint32
		hid    byte
		name   string
	}{
		{0xFF63, 0x49, "Insert"},
		{0xFFFF, 0x4C, "Delete"},
		{0xFF50, 0x4A, "Home"},
		{0xFF57, 0x4D, "End"},
		{0xFF55, 0x4B, "Page_Up"},
		{0xFF56, 0x4E, "Page_Down"},
	}

	for _, tt := range navKeys {
		hid, _, found := KeysymToHID(tt.keysym)
		assert.True(t, found, "%s", tt.name)
		assert.Equal(t, tt.hid, hid, "%s", tt.name)
	}
}

func TestKeysymToHID_ArrowKeys(t *testing.T) {
	arrows := []struct {
		keysym uint32
		hid    byte
		name   string
	}{
		{0xFF53, 0x4F, "Right"},
		{0xFF51, 0x50, "Left"},
		{0xFF54, 0x51, "Down"},
		{0xFF52, 0x52, "Up"},
	}

	for _, tt := range arrows {
		hid, _, found := KeysymToHID(tt.keysym)
		assert.True(t, found, "%s", tt.name)
		assert.Equal(t, tt.hid, hid, "%s", tt.name)
	}
}

func TestKeysymToHID_Modifiers(t *testing.T) {
	modifiers := []struct {
		keysym uint32
		hid    byte
		name   string
	}{
		{XKShiftL, HIDLeftShift, "Shift_L"},
		{XKShiftR, HIDRightShift, "Shift_R"},
		{XKControlL, HIDLeftControl, "Control_L"},
		{XKControlR, HIDRightControl, "Control_R"},
		{XKAltL, HIDLeftAlt, "Alt_L"},
		{XKAltR, HIDRightAlt, "Alt_R"},
		{XKSuperL, HIDLeftGUI, "Super_L"},
		{XKSuperR, HIDRightGUI, "Super_R"},
		{XKMetaL, HIDLeftGUI, "Meta_L"},
		{XKMetaR, HIDRightGUI, "Meta_R"},
	}

	for _, tt := range modifiers {
		hid, isMod, found := KeysymToHID(tt.keysym)
		assert.True(t, found, "%s should be found", tt.name)
		assert.True(t, isMod, "%s should be a modifier", tt.name)
		assert.Equal(t, tt.hid, hid, "%s HID code", tt.name)
	}
}

func TestKeysymToHID_Unknown(t *testing.T) {
	// Unknown keysym
	_, _, found := KeysymToHID(0xDEAD)
	assert.False(t, found)

	_, _, found = KeysymToHID(0)
	assert.False(t, found)
}

func TestKeysymToHID_Punctuation(t *testing.T) {
	punctuation := []struct {
		keysym uint32
		hid    byte
		name   string
	}{
		{0x002D, 0x2D, "minus"},
		{0x003D, 0x2E, "equal"},
		{0x005B, 0x2F, "bracketleft"},
		{0x005D, 0x30, "bracketright"},
		{0x005C, 0x31, "backslash"},
		{0x003B, 0x33, "semicolon"},
		{0x0027, 0x34, "apostrophe"},
		{0x0060, 0x35, "grave"},
		{0x002C, 0x36, "comma"},
		{0x002E, 0x37, "period"},
		{0x002F, 0x38, "slash"},
	}

	for _, tt := range punctuation {
		hid, isMod, found := KeysymToHID(tt.keysym)
		assert.True(t, found, "%s keysym 0x%04X", tt.name, tt.keysym)
		assert.False(t, isMod, "%s should not be modifier", tt.name)
		assert.Equal(t, tt.hid, hid, "%s HID code", tt.name)
	}
}
