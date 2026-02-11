package rdp

import (
	"time"

	"github.com/rs/zerolog"

	"github.com/jetkvm/kvm/internal/sync"
)

type ListenerFactory func(
	cfg Config,
	broker *sessionBroker,
	state *runtimeState,
	metrics Metrics,
	logger *zerolog.Logger,
) listener

// ManagerOptions configures the RDP manager.
type ManagerOptions struct {
	Config       Config
	BindAddress  func(port int) string
	TLSStorePath string

	AuthConfig AuthConfigProvider

	OnSessionActivated   func()
	OnSessionDeactivated func()

	Metrics         Metrics
	Logger          *zerolog.Logger
	ListenerFactory ListenerFactory
}

// Manager owns lifecycle/state for the embedded RDP service.
type Manager struct {
	mu sync.Mutex

	cfg   Config
	state *runtimeState

	metrics         Metrics
	listener        listener
	listenerFactory ListenerFactory

	authenticator *Authenticator
	broker        *sessionBroker
	framePub      *FramePublisher

	logger *zerolog.Logger
}

func NewManager(opts ManagerOptions) *Manager {
	cfg := NormalizeConfig(opts.Config)

	metrics := opts.Metrics
	if metrics == nil {
		metrics = sharedPromMetrics
	}

	state := newRuntimeState(cfg)
	framePub := NewFramePublisher(cfg.MaxFPS)
	auth := NewAuthenticator(opts.AuthConfig, metrics)
	broker := newSessionBroker(state, metrics, opts.OnSessionActivated, opts.OnSessionDeactivated)

	logger := opts.Logger

	factory := opts.ListenerFactory
	if factory == nil {
		tlsProvider := NewTLSProvider(opts.TLSStorePath, logger)
		factory = func(
			cfg Config,
			broker *sessionBroker,
			state *runtimeState,
			metrics Metrics,
			logger *zerolog.Logger,
		) listener {
			return newTCPListener(opts.BindAddress, tlsProvider.TLSConfig(), broker, state, metrics, logger)
		}
	}

	m := &Manager{
		cfg:             cfg,
		state:           state,
		metrics:         metrics,
		listenerFactory: factory,
		authenticator:   auth,
		broker:          broker,
		framePub:        framePub,
		logger:          logger,
	}
	m.listener = m.listenerFactory(cfg, m.broker, m.state, m.metrics, m.logger)
	return m
}

func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.cfg.Enabled {
		m.state.setRunning(false)
		return nil
	}

	return m.startLocked()
}

func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopLocked()
}

func (m *Manager) ApplyConfig(cfg Config) error {
	norm := NormalizeConfig(cfg)

	m.mu.Lock()
	defer m.mu.Unlock()

	old := m.cfg
	m.cfg = norm
	m.state.setConfig(norm)
	m.framePub.SetMaxFPS(norm.MaxFPS)

	if !norm.Enabled {
		return m.stopLocked()
	}

	if !old.Enabled {
		return m.startLocked()
	}

	if old.Port != norm.Port {
		if err := m.stopLocked(); err != nil {
			return err
		}
		return m.startLocked()
	}

	if m.listener != nil && m.listener.Running() {
		return nil
	}

	return m.startLocked()
}

func (m *Manager) State() State {
	return m.state.snapshot()
}

func (m *Manager) PublishFrame(frame []byte, duration time.Duration) {
	if len(frame) == 0 {
		return
	}
	m.framePub.Publish(Frame{Data: frame, Duration: duration})
}

func (m *Manager) SubscribeFrames(buffer int) (uint64, <-chan Frame) {
	return m.framePub.Subscribe(buffer)
}

func (m *Manager) UnsubscribeFrames(id uint64) {
	m.framePub.Unsubscribe(id)
}

func (m *Manager) ValidatePassword(password string) bool {
	return m.authenticator.ValidatePassword(password)
}

func (m *Manager) CloseActiveSession() {
	m.broker.closeActive()
}

func (m *Manager) ActiveSessions() int {
	return m.broker.activeSessions()
}

func (m *Manager) startLocked() error {
	if m.listener == nil {
		m.listener = m.listenerFactory(m.cfg, m.broker, m.state, m.metrics, m.logger)
	}
	if m.listener.Running() {
		m.state.setRunning(true)
		return nil
	}
	if err := m.listener.Start(m.cfg); err != nil {
		m.state.setRunning(false)
		m.state.setLastError(err)
		if m.logger != nil {
			m.logger.Error().Err(err).Msg("failed to start rdp listener")
		}
		return err
	}
	m.state.setRunning(true)
	m.state.setLastError(nil)
	return nil
}

func (m *Manager) stopLocked() error {
	if m.listener == nil {
		m.state.setRunning(false)
		return nil
	}
	if err := m.listener.Stop(); err != nil {
		m.state.setLastError(err)
		if m.logger != nil {
			m.logger.Warn().Err(err).Msg("failed to stop rdp listener")
		}
		return err
	}
	m.state.setRunning(false)
	return nil
}
