package rdp

const (
	hidAbsoluteMax = 32767
)

// InputSink bridges translated RDP input into the existing HID RPC handlers.
type InputSink interface {
	Keypress(key byte, press bool) error
	AbsMouse(x int, y int, buttons uint8) error
	Wheel(wheelY int8) error
}

type keyCode struct {
	scancode uint16
	extended bool
}

var rdpScancodeToHID = map[keyCode]byte{
	{scancode: 0x1E}:                 0x04, // A
	{scancode: 0x30}:                 0x05, // B
	{scancode: 0x2E}:                 0x06, // C
	{scancode: 0x20}:                 0x07, // D
	{scancode: 0x12}:                 0x1A, // O
	{scancode: 0x11}:                 0x1B, // P
	{scancode: 0x1C}:                 0x28, // Enter
	{scancode: 0x01}:                 0x29, // Escape
	{scancode: 0x39}:                 0x2C, // Space
	{scancode: 0x3B}:                 0x3A, // F1
	{scancode: 0x48, extended: true}: 0x52, // Arrow up
	{scancode: 0x50, extended: true}: 0x51, // Arrow down
	{scancode: 0x4B, extended: true}: 0x50, // Arrow left
	{scancode: 0x4D, extended: true}: 0x4F, // Arrow right
}

// KeyEvent is a normalized keyboard event decoded from RDP input PDUs.
type KeyEvent struct {
	Scancode uint16
	Extended bool
	Press    bool
}

// MouseEvent is a normalized absolute pointer event decoded from RDP input PDUs.
type MouseEvent struct {
	X       int
	Y       int
	Width   int
	Height  int
	Buttons uint8
}

// WheelEvent is a normalized wheel event decoded from RDP input PDUs.
type WheelEvent struct {
	Delta int
}

// Mapper maps normalized RDP input events to JetKVM HID operations.
type Mapper struct {
	sink InputSink
}

func NewMapper(sink InputSink) *Mapper {
	return &Mapper{sink: sink}
}

func (m *Mapper) HandleKeyEvent(event KeyEvent) error {
	key, ok := MapKey(event.Scancode, event.Extended)
	if !ok {
		return nil
	}
	return m.sink.Keypress(key, event.Press)
}

func (m *Mapper) HandleMouseEvent(event MouseEvent) error {
	x := ScaleAbsoluteCoordinate(event.X, event.Width)
	y := ScaleAbsoluteCoordinate(event.Y, event.Height)
	return m.sink.AbsMouse(x, y, event.Buttons)
}

func (m *Mapper) HandleWheelEvent(event WheelEvent) error {
	return m.sink.Wheel(MapWheelDelta(event.Delta))
}

// MapKey maps an RDP scancode to a USB HID key code.
func MapKey(scancode uint16, extended bool) (byte, bool) {
	key, ok := rdpScancodeToHID[keyCode{scancode: scancode, extended: extended}]
	if ok {
		return key, true
	}

	// Try non-extended fallback for scancodes that do not need it.
	key, ok = rdpScancodeToHID[keyCode{scancode: scancode}]
	if !ok {
		return 0, false
	}
	return key, true
}

// ScaleAbsoluteCoordinate scales an input coordinate to the HID absolute range [0..32767].
func ScaleAbsoluteCoordinate(value int, max int) int {
	if max <= 1 {
		return 0
	}
	if value < 0 {
		value = 0
	}
	if value >= max {
		value = max - 1
	}

	numerator := int64(value) * hidAbsoluteMax
	denominator := int64(max - 1)
	return int((numerator + denominator/2) / denominator)
}

// MapWheelDelta normalizes RDP wheel deltas (commonly multiples of 120) to int8 HID wheel values.
func MapWheelDelta(delta int) int8 {
	if delta == 0 {
		return 0
	}

	steps := delta / 120
	if steps == 0 {
		if delta > 0 {
			steps = 1
		} else {
			steps = -1
		}
	}

	if steps > 127 {
		steps = 127
	}
	if steps < -127 {
		steps = -127
	}
	return int8(steps)
}
