package rdp

import (
	"crypto/tls"
	"fmt"
	"net"
	"sync"

	"github.com/rs/zerolog"
)

type listener interface {
	Start(cfg Config) error
	Stop() error
	Running() bool
}

type tcpListener struct {
	bindAddress func(port int) string
	tlsConfig   *tls.Config
	broker      *sessionBroker
	state       *runtimeState
	metrics     Metrics
	log         *zerolog.Logger

	mu      sync.Mutex
	ln      net.Listener
	running bool
	wg      sync.WaitGroup
}

func newTCPListener(
	bindAddress func(port int) string,
	tlsConfig *tls.Config,
	broker *sessionBroker,
	state *runtimeState,
	metrics Metrics,
	log *zerolog.Logger,
) *tcpListener {
	if metrics == nil {
		metrics = noopMetrics{}
	}
	return &tcpListener{
		bindAddress: bindAddress,
		tlsConfig:   tlsConfig,
		broker:      broker,
		state:       state,
		metrics:     metrics,
		log:         log,
	}
}

func (l *tcpListener) Start(cfg Config) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.running {
		return nil
	}

	addr := ""
	if l.bindAddress != nil {
		addr = l.bindAddress(cfg.Port)
	}
	if addr == "" {
		return fmt.Errorf("failed to resolve bind address for port %d", cfg.Port)
	}

	ln, err := tls.Listen("tcp", addr, l.tlsConfig)
	if err != nil {
		return fmt.Errorf("failed to bind rdp listener on %s: %w", addr, err)
	}

	l.ln = ln
	l.running = true
	l.state.setRunning(true)
	l.state.setLastError(nil)
	if l.log != nil {
		l.log.Info().Str("bindAddress", addr).Msg("rdp listener started")
	}

	l.wg.Add(1)
	go l.acceptLoop()
	return nil
}

func (l *tcpListener) acceptLoop() {
	defer l.wg.Done()

	for {
		conn, err := l.ln.Accept()
		if err != nil {
			if !l.Running() {
				return
			}
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				l.state.setLastError(err)
				continue
			}

			l.state.setLastError(err)
			if l.log != nil {
				l.log.Warn().Err(err).Msg("rdp listener accept loop exited")
			}
			l.mu.Lock()
			l.running = false
			l.mu.Unlock()
			l.state.setRunning(false)
			return
		}

		l.metrics.IncConnections()
		session := newConnectionSession(conn)
		l.broker.activate(session)

		l.wg.Add(1)
		go func(s *connectionSession) {
			defer l.wg.Done()
			defer l.broker.deactivate(s)
			s.readLoop()
		}(session)
	}
}

func (l *tcpListener) Stop() error {
	l.mu.Lock()
	if !l.running {
		l.mu.Unlock()
		return nil
	}
	ln := l.ln
	l.running = false
	l.ln = nil
	l.mu.Unlock()

	if l.log != nil {
		l.log.Info().Msg("stopping rdp listener")
	}

	var closeErr error
	if ln != nil {
		closeErr = ln.Close()
	}
	l.broker.closeActive()
	l.wg.Wait()

	l.state.setRunning(false)
	if l.log != nil {
		l.log.Info().Msg("rdp listener stopped")
	}
	return closeErr
}

func (l *tcpListener) Running() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.running
}
