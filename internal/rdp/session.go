package rdp

import (
	"io"
	"net"
	"sync"
)

type session interface {
	Close() error
	Done() <-chan struct{}
}

// connectionSession wraps an accepted TCP/TLS connection.
type connectionSession struct {
	conn net.Conn

	closeOnce sync.Once
	done      chan struct{}
}

func newConnectionSession(conn net.Conn) *connectionSession {
	return &connectionSession{
		conn: conn,
		done: make(chan struct{}),
	}
}

func (s *connectionSession) Close() error {
	var err error
	s.closeOnce.Do(func() {
		err = s.conn.Close()
		close(s.done)
	})
	return err
}

func (s *connectionSession) Done() <-chan struct{} {
	return s.done
}

func (s *connectionSession) readLoop() {
	_, _ = io.Copy(io.Discard, s.conn)
	_ = s.Close()
}

// sessionBroker enforces single active RDP session at a time.
type sessionBroker struct {
	mu sync.Mutex

	active session

	metrics Metrics
	state   *runtimeState

	onActivated   func()
	onDeactivated func()
}

func newSessionBroker(state *runtimeState, metrics Metrics, onActivated func(), onDeactivated func()) *sessionBroker {
	if metrics == nil {
		metrics = noopMetrics{}
	}
	return &sessionBroker{
		metrics:       metrics,
		state:         state,
		onActivated:   onActivated,
		onDeactivated: onDeactivated,
	}
}

func (b *sessionBroker) activate(next session) {
	if next == nil {
		return
	}

	var (
		previous       session
		triggerOnFirst bool
	)

	b.mu.Lock()
	if b.active != nil && b.active != next {
		previous = b.active
		b.metrics.IncSessionTakeovers()
	}
	triggerOnFirst = b.active == nil
	b.active = next
	b.state.setActiveSessions(1)
	b.metrics.SetActiveSessions(1)
	b.mu.Unlock()

	if previous != nil {
		_ = previous.Close()
	}

	if triggerOnFirst && b.onActivated != nil {
		b.onActivated()
	}
}

func (b *sessionBroker) deactivate(s session) {
	if s == nil {
		return
	}

	triggerOnLast := false

	b.mu.Lock()
	if b.active == s {
		b.active = nil
		b.state.setActiveSessions(0)
		b.metrics.SetActiveSessions(0)
		triggerOnLast = true
	}
	b.mu.Unlock()

	if triggerOnLast && b.onDeactivated != nil {
		b.onDeactivated()
	}
}

func (b *sessionBroker) closeActive() {
	b.mu.Lock()
	active := b.active
	b.mu.Unlock()
	if active != nil {
		_ = active.Close()
		b.deactivate(active)
	}
}

func (b *sessionBroker) activeSessions() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.active == nil {
		return 0
	}
	return 1
}
