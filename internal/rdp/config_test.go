package rdp

import "testing"

func TestNormalizeConfig(t *testing.T) {
	tests := []struct {
		name string
		in   Config
		want Config
	}{
		{
			name: "defaults migration",
			in:   Config{},
			want: Config{Enabled: false, Port: DefaultPort, MaxFPS: DefaultMaxFPS},
		},
		{
			name: "max fps clamped low",
			in:   Config{Enabled: true, Port: DefaultPort, MaxFPS: -10},
			want: Config{Enabled: true, Port: DefaultPort, MaxFPS: MinMaxFPS},
		},
		{
			name: "max fps clamped high",
			in:   Config{Enabled: true, Port: DefaultPort, MaxFPS: 120},
			want: Config{Enabled: true, Port: DefaultPort, MaxFPS: MaxMaxFPS},
		},
		{
			name: "port clamped low to default",
			in:   Config{Enabled: true, Port: 0, MaxFPS: 10},
			want: Config{Enabled: true, Port: DefaultPort, MaxFPS: 10},
		},
		{
			name: "port clamped high to default",
			in:   Config{Enabled: true, Port: 70000, MaxFPS: 10},
			want: Config{Enabled: true, Port: DefaultPort, MaxFPS: 10},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeConfig(tt.in)
			if got != tt.want {
				t.Fatalf("NormalizeConfig() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
