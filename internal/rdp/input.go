package rdp

// InputEvent represents a parsed RDP input event from a client.
type InputEvent struct {
	Type       uint16
	Timestamp  uint32
	Flags      uint16
	KeyCode    uint16 // for keyboard events (scancode)
	Unicode    uint16 // for unicode events
	MouseX     uint16 // for mouse events
	MouseY     uint16 // for mouse events
}

// KeyboardEvent returns true if this is a keyboard scancode event.
func (e *InputEvent) KeyboardEvent() bool {
	return e.Type == InputEventScancode
}

// UnicodeEvent returns true if this is a unicode keyboard event.
func (e *InputEvent) UnicodeEvent() bool {
	return e.Type == InputEventUnicode
}

// MouseEvent returns true if this is a mouse event.
func (e *InputEvent) MouseEvent() bool {
	return e.Type == InputEventMouse
}

// SyncEvent returns true if this is a synchronize event.
func (e *InputEvent) SyncEvent() bool {
	return e.Type == InputEventSync
}

// KeyPressed returns true if this keyboard event is a key press (not release).
func (e *InputEvent) KeyPressed() bool {
	return e.Flags&KeyFlagRelease == 0
}

// KeyExtended returns true if this keyboard event has the extended flag.
func (e *InputEvent) KeyExtended() bool {
	return e.Flags&KeyFlagExtended != 0
}

// MouseButtonLeft returns true if left mouse button is involved.
func (e *InputEvent) MouseButtonLeft() bool {
	return e.Flags&MouseFlagButton1 != 0
}

// MouseButtonRight returns true if right mouse button is involved.
func (e *InputEvent) MouseButtonRight() bool {
	return e.Flags&MouseFlagButton2 != 0
}

// MouseButtonMiddle returns true if middle mouse button is involved.
func (e *InputEvent) MouseButtonMiddle() bool {
	return e.Flags&MouseFlagButton3 != 0
}

// MouseButtonDown returns true if a mouse button press occurred.
func (e *InputEvent) MouseButtonDown() bool {
	return e.Flags&MouseFlagDown != 0
}

// MouseMove returns true if this is a mouse move event.
func (e *InputEvent) MouseMove() bool {
	return e.Flags&MouseFlagMove != 0
}

// MouseWheel returns the scroll wheel delta. Positive = up, negative = down.
// Returns 0 if this is not a wheel event.
func (e *InputEvent) MouseWheel() int {
	if e.Flags&MouseFlagWheelUp != 0 {
		return int(e.Flags & MouseFlagWheelMask)
	}
	if e.Flags&MouseFlagWheelDown != 0 {
		return -int(e.Flags & MouseFlagWheelMask)
	}
	return 0
}

// ParseInputEvents parses input events from an RDP input PDU payload.
// The payload starts after the share data header, at the numEvents field.
func ParseInputEvents(data []byte) []InputEvent {
	if len(data) < 4 {
		return nil
	}

	numEvents := getU16LE(data, 0)
	// padding: 2 bytes at offset 2
	offset := 4

	events := make([]InputEvent, 0, numEvents)
	for i := 0; i < int(numEvents); i++ {
		if offset+6 > len(data) {
			break
		}

		evt := InputEvent{
			Timestamp: getU32LE(data, offset),
			Type:      getU16LE(data, offset+4),
		}
		offset += 6

		switch evt.Type {
		case InputEventSync:
			if offset+6 <= len(data) {
				// pad2Octets(2) + toggleFlags(4)
				offset += 6
			}
		case InputEventScancode:
			if offset+6 <= len(data) {
				evt.Flags = getU16LE(data, offset)
				evt.KeyCode = getU16LE(data, offset+2)
				// pad2Octets(2) at offset+4
				offset += 6
			}
		case InputEventUnicode:
			if offset+6 <= len(data) {
				evt.Flags = getU16LE(data, offset)
				evt.Unicode = getU16LE(data, offset+2)
				// pad2Octets(2) at offset+4
				offset += 6
			}
		case InputEventMouse:
			if offset+6 <= len(data) {
				evt.Flags = getU16LE(data, offset)
				evt.MouseX = getU16LE(data, offset+2)
				evt.MouseY = getU16LE(data, offset+4)
				offset += 6
			}
		default:
			// Unknown event type, skip 6 bytes (standard event size)
			if offset+6 <= len(data) {
				offset += 6
			}
		}

		events = append(events, evt)
	}

	return events
}

// InputHandler is the interface that the RDP server uses to forward
// input events to the KVM system.
type InputHandler interface {
	// HandleKeyboard processes a keyboard scancode event.
	HandleKeyboard(scancode uint16, pressed bool, extended bool) error

	// HandleMouseMove processes a mouse move event with absolute coordinates.
	HandleMouseMove(x, y uint16) error

	// HandleMouseButton processes a mouse button event.
	// button: 1=left, 2=right, 3=middle
	HandleMouseButton(button int, pressed bool) error

	// HandleMouseWheel processes a mouse wheel event.
	// delta: positive=up, negative=down
	HandleMouseWheel(delta int) error
}

// DispatchInputEvents processes parsed input events through the given handler.
func DispatchInputEvents(events []InputEvent, handler InputHandler) error {
	if handler == nil {
		return nil
	}

	for _, evt := range events {
		var err error
		switch {
		case evt.KeyboardEvent():
			err = handler.HandleKeyboard(evt.KeyCode, evt.KeyPressed(), evt.KeyExtended())

		case evt.MouseEvent():
			if evt.MouseMove() {
				if mErr := handler.HandleMouseMove(evt.MouseX, evt.MouseY); mErr != nil {
					err = mErr
				}
			}
			if evt.MouseButtonLeft() {
				if mErr := handler.HandleMouseButton(1, evt.MouseButtonDown()); mErr != nil {
					err = mErr
				}
			}
			if evt.MouseButtonRight() {
				if mErr := handler.HandleMouseButton(2, evt.MouseButtonDown()); mErr != nil {
					err = mErr
				}
			}
			if evt.MouseButtonMiddle() {
				if mErr := handler.HandleMouseButton(3, evt.MouseButtonDown()); mErr != nil {
					err = mErr
				}
			}
			if wheel := evt.MouseWheel(); wheel != 0 {
				if mErr := handler.HandleMouseWheel(wheel); mErr != nil {
					err = mErr
				}
			}
		}

		if err != nil {
			return err
		}
	}

	return nil
}
