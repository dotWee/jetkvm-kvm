package rdp

import (
	"sync"
	"sync/atomic"
)

type runtimeState struct {
	enabled        atomic.Bool
	running        atomic.Bool
	port           atomic.Int32
	activeSessions atomic.Int32
	maxFPS         atomic.Int32

	lastErrorMu sync.RWMutex
	lastError   string
}

func newRuntimeState(cfg Config) *runtimeState {
	norm := NormalizeConfig(cfg)
	rs := &runtimeState{}
	rs.enabled.Store(norm.Enabled)
	rs.running.Store(false)
	rs.port.Store(int32(norm.Port))
	rs.activeSessions.Store(0)
	rs.maxFPS.Store(int32(norm.MaxFPS))
	return rs
}

func (s *runtimeState) setConfig(cfg Config) {
	norm := NormalizeConfig(cfg)
	s.enabled.Store(norm.Enabled)
	s.port.Store(int32(norm.Port))
	s.maxFPS.Store(int32(norm.MaxFPS))
}

func (s *runtimeState) setRunning(running bool) {
	s.running.Store(running)
}

func (s *runtimeState) setActiveSessions(count int) {
	s.activeSessions.Store(int32(count))
}

func (s *runtimeState) setLastError(err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	s.lastErrorMu.Lock()
	s.lastError = msg
	s.lastErrorMu.Unlock()
}

func (s *runtimeState) snapshot() State {
	s.lastErrorMu.RLock()
	lastErr := s.lastError
	s.lastErrorMu.RUnlock()

	return State{
		Enabled:        s.enabled.Load(),
		Running:        s.running.Load(),
		Port:           int(s.port.Load()),
		ActiveSessions: int(s.activeSessions.Load()),
		MaxFPS:         int(s.maxFPS.Load()),
		LastError:      lastErr,
	}
}
