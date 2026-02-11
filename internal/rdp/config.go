package rdp

const (
	DefaultPort   = 3389
	DefaultMaxFPS = 15
	MinMaxFPS     = 1
	MaxMaxFPS     = 30
	MinPort       = 1
	MaxPort       = 65535
)

// Config controls the embedded RDP service runtime behavior.
type Config struct {
	Enabled bool
	Port    int
	MaxFPS  int
}

// NormalizeConfig applies defaults and clamps values to supported ranges.
func NormalizeConfig(c Config) Config {
	if c.Port < MinPort || c.Port > MaxPort {
		c.Port = DefaultPort
	}

	if c.MaxFPS == 0 {
		c.MaxFPS = DefaultMaxFPS
	}
	if c.MaxFPS < MinMaxFPS {
		c.MaxFPS = MinMaxFPS
	}
	if c.MaxFPS > MaxMaxFPS {
		c.MaxFPS = MaxMaxFPS
	}

	return c
}

// State is the externally visible RDP runtime state.
type State struct {
	Enabled        bool   `json:"enabled"`
	Running        bool   `json:"running"`
	Port           int    `json:"port"`
	ActiveSessions int    `json:"activeSessions"`
	MaxFPS         int    `json:"maxFps"`
	LastError      string `json:"lastError"`
}
