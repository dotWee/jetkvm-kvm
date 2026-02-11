package rdp

import (
	"sync"
	"testing"

	"github.com/rs/zerolog"
)

type fakeSession struct {
	mu         sync.Mutex
	closeCount int
	done       chan struct{}
	once       sync.Once
}

func newFakeSession() *fakeSession {
	return &fakeSession{done: make(chan struct{})}
}

func (s *fakeSession) Close() error {
	s.mu.Lock()
	s.closeCount++
	s.mu.Unlock()
	s.once.Do(func() { close(s.done) })
	return nil
}

func (s *fakeSession) Done() <-chan struct{} {
	return s.done
}

func (s *fakeSession) ClosedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeCount
}

func TestSessionBrokerSecondSessionEvictsFirst(t *testing.T) {
	state := newRuntimeState(Config{Enabled: true, Port: DefaultPort, MaxFPS: DefaultMaxFPS})
	metrics := &metricsSpy{}
	broker := newSessionBroker(state, metrics, nil, nil)

	first := newFakeSession()
	second := newFakeSession()

	broker.activate(first)
	broker.activate(second)

	if first.ClosedCount() != 1 {
		t.Fatalf("expected first session to be closed once, got %d", first.ClosedCount())
	}
	if second.ClosedCount() != 0 {
		t.Fatalf("expected second session to stay active, got closed=%d", second.ClosedCount())
	}
	if metrics.sessionTakeovers != 1 {
		t.Fatalf("expected one session takeover metric, got %d", metrics.sessionTakeovers)
	}
	if broker.activeSessions() != 1 {
		t.Fatalf("expected one active session, got %d", broker.activeSessions())
	}
}

func TestManagerWebRTCTakeoverClosesRDPSession(t *testing.T) {
	activated := 0
	deactivated := 0
	m := NewManager(ManagerOptions{
		Config: Config{Enabled: true, Port: DefaultPort, MaxFPS: DefaultMaxFPS},
		OnSessionActivated: func() {
			activated++
		},
		OnSessionDeactivated: func() {
			deactivated++
		},
		ListenerFactory: func(Config, *sessionBroker, *runtimeState, Metrics, *zerolog.Logger) listener {
			return &fakeListener{}
		},
	})

	s := newFakeSession()
	m.broker.activate(s)
	if m.ActiveSessions() != 1 {
		t.Fatalf("expected one active RDP session before takeover")
	}

	m.CloseActiveSession()

	if s.ClosedCount() == 0 {
		t.Fatalf("expected active RDP session to be closed on WebRTC takeover")
	}
	if m.ActiveSessions() != 0 {
		t.Fatalf("expected no active RDP session after takeover")
	}
	if activated != 1 {
		t.Fatalf("expected activation callback once, got %d", activated)
	}
	if deactivated != 1 {
		t.Fatalf("expected deactivation callback once, got %d", deactivated)
	}
}
