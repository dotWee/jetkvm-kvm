package rdp

import (
	"errors"
	"testing"
)

func TestInputEventTypes(t *testing.T) {
	tests := []struct {
		name     string
		evt      InputEvent
		keyboard bool
		unicode  bool
		mouse    bool
		sync     bool
	}{
		{"keyboard", InputEvent{Type: InputEventScancode}, true, false, false, false},
		{"unicode", InputEvent{Type: InputEventUnicode}, false, true, false, false},
		{"mouse", InputEvent{Type: InputEventMouse}, false, false, true, false},
		{"sync", InputEvent{Type: InputEventSync}, false, false, false, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.evt.KeyboardEvent() != tc.keyboard {
				t.Errorf("KeyboardEvent() = %v, want %v", tc.evt.KeyboardEvent(), tc.keyboard)
			}
			if tc.evt.UnicodeEvent() != tc.unicode {
				t.Errorf("UnicodeEvent() = %v, want %v", tc.evt.UnicodeEvent(), tc.unicode)
			}
			if tc.evt.MouseEvent() != tc.mouse {
				t.Errorf("MouseEvent() = %v, want %v", tc.evt.MouseEvent(), tc.mouse)
			}
			if tc.evt.SyncEvent() != tc.sync {
				t.Errorf("SyncEvent() = %v, want %v", tc.evt.SyncEvent(), tc.sync)
			}
		})
	}
}

func TestKeyPressed(t *testing.T) {
	pressed := InputEvent{Type: InputEventScancode, Flags: KeyFlagDown}
	if !pressed.KeyPressed() {
		t.Error("expected key pressed")
	}

	released := InputEvent{Type: InputEventScancode, Flags: KeyFlagRelease}
	if released.KeyPressed() {
		t.Error("expected key released")
	}
}

func TestKeyExtended(t *testing.T) {
	ext := InputEvent{Type: InputEventScancode, Flags: KeyFlagExtended}
	if !ext.KeyExtended() {
		t.Error("expected extended flag")
	}

	normal := InputEvent{Type: InputEventScancode, Flags: 0}
	if normal.KeyExtended() {
		t.Error("expected no extended flag")
	}
}

func TestMouseButtons(t *testing.T) {
	evt := InputEvent{
		Type:  InputEventMouse,
		Flags: MouseFlagButton1 | MouseFlagDown,
	}

	if !evt.MouseButtonLeft() {
		t.Error("expected left button")
	}
	if evt.MouseButtonRight() {
		t.Error("unexpected right button")
	}
	if evt.MouseButtonMiddle() {
		t.Error("unexpected middle button")
	}
	if !evt.MouseButtonDown() {
		t.Error("expected button down")
	}
}

func TestMouseMove(t *testing.T) {
	evt := InputEvent{
		Type:   InputEventMouse,
		Flags:  MouseFlagMove,
		MouseX: 100,
		MouseY: 200,
	}

	if !evt.MouseMove() {
		t.Error("expected move event")
	}
	if evt.MouseX != 100 || evt.MouseY != 200 {
		t.Errorf("position = (%d, %d), want (100, 200)", evt.MouseX, evt.MouseY)
	}
}

func TestMouseWheel(t *testing.T) {
	upEvt := InputEvent{
		Type:  InputEventMouse,
		Flags: MouseFlagWheelUp | 3, // 3 lines up
	}
	if upEvt.MouseWheel() != 3 {
		t.Errorf("wheel up = %d, want 3", upEvt.MouseWheel())
	}

	downEvt := InputEvent{
		Type:  InputEventMouse,
		Flags: MouseFlagWheelDown | 3, // 3 lines down
	}
	if downEvt.MouseWheel() != -3 {
		t.Errorf("wheel down = %d, want -3", downEvt.MouseWheel())
	}

	noWheel := InputEvent{Type: InputEventMouse, Flags: MouseFlagMove}
	if noWheel.MouseWheel() != 0 {
		t.Errorf("no wheel = %d, want 0", noWheel.MouseWheel())
	}
}

func TestParseInputEventsEmpty(t *testing.T) {
	events := ParseInputEvents(nil)
	if events != nil {
		t.Errorf("expected nil for nil data, got %d events", len(events))
	}

	events = ParseInputEvents([]byte{0, 0})
	if events != nil {
		t.Errorf("expected nil for short data, got %d events", len(events))
	}
}

func TestParseInputEventsKeyboard(t *testing.T) {
	data := make([]byte, 16)
	putU16LE(data, 0, 1) // numEvents = 1
	// padding at offset 2
	putU32LE(data, 4, 100)               // timestamp
	putU16LE(data, 8, InputEventScancode) // type
	putU16LE(data, 10, 0)                 // flags (key down)
	putU16LE(data, 12, 0x1E)              // keyCode (A key)
	// padding at 14

	events := ParseInputEvents(data)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	evt := events[0]
	if !evt.KeyboardEvent() {
		t.Error("expected keyboard event")
	}
	if evt.KeyCode != 0x1E {
		t.Errorf("keyCode = 0x%04X, want 0x001E", evt.KeyCode)
	}
	if !evt.KeyPressed() {
		t.Error("expected key pressed")
	}
}

func TestParseInputEventsMouse(t *testing.T) {
	data := make([]byte, 16)
	putU16LE(data, 0, 1) // numEvents = 1
	putU32LE(data, 4, 200)             // timestamp
	putU16LE(data, 8, InputEventMouse) // type
	putU16LE(data, 10, MouseFlagMove|MouseFlagButton1|MouseFlagDown) // flags
	putU16LE(data, 12, 320) // mouseX
	putU16LE(data, 14, 240) // mouseY

	events := ParseInputEvents(data)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	evt := events[0]
	if !evt.MouseEvent() {
		t.Error("expected mouse event")
	}
	if evt.MouseX != 320 || evt.MouseY != 240 {
		t.Errorf("position = (%d, %d), want (320, 240)", evt.MouseX, evt.MouseY)
	}
	if !evt.MouseMove() {
		t.Error("expected move flag")
	}
	if !evt.MouseButtonLeft() {
		t.Error("expected left button")
	}
	if !evt.MouseButtonDown() {
		t.Error("expected button down")
	}
}

// mockInputHandler records input events for testing.
type mockInputHandler struct {
	keyboards    []mockKeyboard
	moves        []mockMove
	buttons      []mockButton
	wheels       []int
	keyboardErr  error
	moveErr      error
	buttonErr    error
	wheelErr     error
}

type mockKeyboard struct {
	scancode uint16
	pressed  bool
	extended bool
}

type mockMove struct {
	x, y uint16
}

type mockButton struct {
	button  int
	pressed bool
}

func (m *mockInputHandler) HandleKeyboard(scancode uint16, pressed bool, extended bool) error {
	m.keyboards = append(m.keyboards, mockKeyboard{scancode, pressed, extended})
	return m.keyboardErr
}

func (m *mockInputHandler) HandleMouseMove(x, y uint16) error {
	m.moves = append(m.moves, mockMove{x, y})
	return m.moveErr
}

func (m *mockInputHandler) HandleMouseButton(button int, pressed bool) error {
	m.buttons = append(m.buttons, mockButton{button, pressed})
	return m.buttonErr
}

func (m *mockInputHandler) HandleMouseWheel(delta int) error {
	m.wheels = append(m.wheels, delta)
	return m.wheelErr
}

func TestDispatchInputEventsNilHandler(t *testing.T) {
	events := []InputEvent{{Type: InputEventScancode}}
	if err := DispatchInputEvents(events, nil); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDispatchInputEventsKeyboard(t *testing.T) {
	handler := &mockInputHandler{}
	events := []InputEvent{
		{Type: InputEventScancode, KeyCode: 0x1E, Flags: KeyFlagDown},
		{Type: InputEventScancode, KeyCode: 0x1E, Flags: KeyFlagRelease},
		{Type: InputEventScancode, KeyCode: 0x39, Flags: KeyFlagExtended},
	}

	if err := DispatchInputEvents(events, handler); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(handler.keyboards) != 3 {
		t.Fatalf("expected 3 keyboard events, got %d", len(handler.keyboards))
	}

	if !handler.keyboards[0].pressed {
		t.Error("first event should be pressed")
	}
	if handler.keyboards[1].pressed {
		t.Error("second event should be released")
	}
	if !handler.keyboards[2].extended {
		t.Error("third event should be extended")
	}
}

func TestDispatchInputEventsMouse(t *testing.T) {
	handler := &mockInputHandler{}
	events := []InputEvent{
		{Type: InputEventMouse, Flags: MouseFlagMove, MouseX: 100, MouseY: 200},
		{Type: InputEventMouse, Flags: MouseFlagButton1 | MouseFlagDown},
		{Type: InputEventMouse, Flags: MouseFlagButton2 | MouseFlagDown},
		{Type: InputEventMouse, Flags: MouseFlagWheelUp | 5},
	}

	if err := DispatchInputEvents(events, handler); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(handler.moves) != 1 {
		t.Fatalf("expected 1 move, got %d", len(handler.moves))
	}
	if handler.moves[0].x != 100 || handler.moves[0].y != 200 {
		t.Errorf("move = (%d, %d), want (100, 200)", handler.moves[0].x, handler.moves[0].y)
	}

	if len(handler.buttons) != 2 {
		t.Fatalf("expected 2 button events, got %d", len(handler.buttons))
	}

	if len(handler.wheels) != 1 {
		t.Fatalf("expected 1 wheel event, got %d", len(handler.wheels))
	}
	if handler.wheels[0] != 5 {
		t.Errorf("wheel = %d, want 5", handler.wheels[0])
	}
}

func TestDispatchInputEventsError(t *testing.T) {
	testErr := errors.New("test error")
	handler := &mockInputHandler{keyboardErr: testErr}
	events := []InputEvent{
		{Type: InputEventScancode, KeyCode: 0x1E},
	}

	err := DispatchInputEvents(events, handler)
	if !errors.Is(err, testErr) {
		t.Errorf("error = %v, want %v", err, testErr)
	}
}

func TestParseInputEventsMultiple(t *testing.T) {
	// Two events: keyboard + mouse
	data := make([]byte, 28)
	putU16LE(data, 0, 2) // numEvents = 2

	// Event 1: keyboard
	putU32LE(data, 4, 100)               // timestamp
	putU16LE(data, 8, InputEventScancode) // type
	putU16LE(data, 10, 0)                 // flags
	putU16LE(data, 12, 0x1E)              // keyCode
	putU16LE(data, 14, 0)                 // padding

	// Event 2: mouse
	putU32LE(data, 16, 200)             // timestamp
	putU16LE(data, 20, InputEventMouse) // type
	putU16LE(data, 22, MouseFlagMove)   // flags
	putU16LE(data, 24, 100)             // mouseX
	putU16LE(data, 26, 200)             // mouseY

	events := ParseInputEvents(data)
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	if !events[0].KeyboardEvent() {
		t.Error("first event should be keyboard")
	}
	if !events[1].MouseEvent() {
		t.Error("second event should be mouse")
	}
}
