package rdp

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics captures runtime RDP service telemetry.
type Metrics interface {
	IncConnections()
	IncAuthFailures()
	IncSessionTakeovers()
	SetActiveSessions(count int)
}

type noopMetrics struct{}

func (noopMetrics) IncConnections()       {}
func (noopMetrics) IncAuthFailures()      {}
func (noopMetrics) IncSessionTakeovers()  {}
func (noopMetrics) SetActiveSessions(int) {}

type promMetrics struct {
	connections      prometheus.Counter
	authFailures     prometheus.Counter
	sessionTakeovers prometheus.Counter
	activeSessions   prometheus.Gauge
}

var sharedPromMetrics = newPromMetrics()

func newPromMetrics() Metrics {
	return &promMetrics{
		connections: promauto.NewCounter(prometheus.CounterOpts{
			Name: "jetkvm_rdp_connections_total",
			Help: "Total accepted RDP TCP/TLS connections.",
		}),
		authFailures: promauto.NewCounter(prometheus.CounterOpts{
			Name: "jetkvm_rdp_auth_failures_total",
			Help: "Total failed local-auth checks for RDP sessions.",
		}),
		sessionTakeovers: promauto.NewCounter(prometheus.CounterOpts{
			Name: "jetkvm_rdp_session_takeovers_total",
			Help: "Total number of RDP session takeovers (new session evicted old one).",
		}),
		activeSessions: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "jetkvm_rdp_active_sessions",
			Help: "Current active RDP control sessions.",
		}),
	}
}

func (m *promMetrics) IncConnections() {
	m.connections.Inc()
}

func (m *promMetrics) IncAuthFailures() {
	m.authFailures.Inc()
}

func (m *promMetrics) IncSessionTakeovers() {
	m.sessionTakeovers.Inc()
}

func (m *promMetrics) SetActiveSessions(count int) {
	m.activeSessions.Set(float64(count))
}
