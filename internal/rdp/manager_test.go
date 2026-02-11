package rdp

import (
	"testing"

	"github.com/rs/zerolog"
)

type fakeListener struct {
	startCount int
	stopCount  int
	running    bool
	lastCfg    Config
	startErr   error
}

func (f *fakeListener) Start(cfg Config) error {
	if f.startErr != nil {
		return f.startErr
	}
	f.startCount++
	f.running = true
	f.lastCfg = cfg
	return nil
}

func (f *fakeListener) Stop() error {
	if f.running {
		f.stopCount++
	}
	f.running = false
	return nil
}

func (f *fakeListener) Running() bool {
	return f.running
}

func TestManagerLifecycleIdempotent(t *testing.T) {
	l := &fakeListener{}
	m := NewManager(ManagerOptions{
		Config: Config{Enabled: true, Port: DefaultPort, MaxFPS: DefaultMaxFPS},
		ListenerFactory: func(Config, *sessionBroker, *runtimeState, Metrics, *zerolog.Logger) listener {
			return l
		},
	})

	if err := m.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if err := m.Start(); err != nil {
		t.Fatalf("second start failed: %v", err)
	}
	if l.startCount != 1 {
		t.Fatalf("expected start count 1, got %d", l.startCount)
	}

	if err := m.Stop(); err != nil {
		t.Fatalf("stop failed: %v", err)
	}
	if err := m.Stop(); err != nil {
		t.Fatalf("second stop failed: %v", err)
	}
	if l.stopCount != 1 {
		t.Fatalf("expected stop count 1, got %d", l.stopCount)
	}

	if err := m.Start(); err != nil {
		t.Fatalf("restart failed: %v", err)
	}
	if l.startCount != 2 {
		t.Fatalf("expected start count 2 after restart, got %d", l.startCount)
	}
}

func TestManagerApplyConfigRestartOnPortChange(t *testing.T) {
	l := &fakeListener{}
	m := NewManager(ManagerOptions{
		Config: Config{Enabled: true, Port: DefaultPort, MaxFPS: DefaultMaxFPS},
		ListenerFactory: func(Config, *sessionBroker, *runtimeState, Metrics, *zerolog.Logger) listener {
			return l
		},
	})

	if err := m.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	if err := m.ApplyConfig(Config{Enabled: true, Port: 3390, MaxFPS: 10}); err != nil {
		t.Fatalf("apply config failed: %v", err)
	}

	if l.stopCount != 1 {
		t.Fatalf("expected one stop on port change, got %d", l.stopCount)
	}
	if l.startCount != 2 {
		t.Fatalf("expected listener restart on port change, got startCount=%d", l.startCount)
	}
	if l.lastCfg.Port != 3390 {
		t.Fatalf("expected listener to restart with port 3390, got %d", l.lastCfg.Port)
	}
}
