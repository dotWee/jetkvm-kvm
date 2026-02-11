package rdp

import "testing"

func TestMapKeyRepresentativeKeys(t *testing.T) {
	tests := []struct {
		name     string
		scancode uint16
		extended bool
		want     byte
	}{
		{name: "A", scancode: 0x1E, want: 0x04},
		{name: "Enter", scancode: 0x1C, want: 0x28},
		{name: "Escape", scancode: 0x01, want: 0x29},
		{name: "ArrowUp", scancode: 0x48, extended: true, want: 0x52},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := MapKey(tt.scancode, tt.extended)
			if !ok {
				t.Fatalf("expected mapping for scancode %#x", tt.scancode)
			}
			if got != tt.want {
				t.Fatalf("MapKey() = %#x, want %#x", got, tt.want)
			}
		})
	}
}

func TestScaleAbsoluteCoordinate(t *testing.T) {
	if got := ScaleAbsoluteCoordinate(0, 1920); got != 0 {
		t.Fatalf("expected left edge to map to 0, got %d", got)
	}

	if got := ScaleAbsoluteCoordinate(1919, 1920); got != 32767 {
		t.Fatalf("expected right edge to map to 32767, got %d", got)
	}

	center := ScaleAbsoluteCoordinate(960, 1920)
	if center < 16300 || center > 16400 {
		t.Fatalf("expected center around half-range, got %d", center)
	}
}

func TestMapWheelDelta(t *testing.T) {
	if got := MapWheelDelta(120); got <= 0 {
		t.Fatalf("expected positive wheel mapping for +120, got %d", got)
	}
	if got := MapWheelDelta(-120); got >= 0 {
		t.Fatalf("expected negative wheel mapping for -120, got %d", got)
	}
	if got := MapWheelDelta(480); got != 4 {
		t.Fatalf("expected magnitude 4 for delta 480, got %d", got)
	}
}
