package kvm

import (
	"time"

	"github.com/jetkvm/kvm/internal/rdp"
	"github.com/jetkvm/kvm/internal/sync"
)

var (
	rdpManager     *rdp.Manager
	rdpManagerLock = &sync.Mutex{}

	controlSessionLock = &sync.Mutex{}
)

type rdpAuthConfigProvider struct{}

func (rdpAuthConfigProvider) LocalAuthMode() string {
	if config == nil {
		return ""
	}
	return config.LocalAuthMode
}

func (rdpAuthConfigProvider) HashedPassword() string {
	if config == nil {
		return ""
	}
	return config.HashedPassword
}

func initRDPManager() {
	rdpManagerLock.Lock()
	defer rdpManagerLock.Unlock()

	if rdpManager != nil {
		return
	}

	rdpManager = rdp.NewManager(rdp.ManagerOptions{
		Config: rdp.NormalizeConfig(rdp.Config{
			Enabled: config.RDPEnabled,
			Port:    config.RDPPort,
			MaxFPS:  config.RDPMaxFPS,
		}),
		BindAddress: getBindAddress,
		AuthConfig:  rdpAuthConfigProvider{},
		Logger:      rdpLogger,
		OnSessionActivated: func() {
			onRDPControlSessionActivated()
		},
		OnSessionDeactivated: func() {
			onRDPControlSessionDeactivated()
		},
	})
}

func ensureRDPManager() *rdp.Manager {
	if rdpManager == nil {
		initRDPManager()
	}
	return rdpManager
}

func currentRDPConfig() rdp.Config {
	return rdp.NormalizeConfig(rdp.Config{
		Enabled: config.RDPEnabled,
		Port:    config.RDPPort,
		MaxFPS:  config.RDPMaxFPS,
	})
}

func applyRDPConfig() error {
	return ensureRDPManager().ApplyConfig(currentRDPConfig())
}

func getRDPState() rdp.State {
	state := ensureRDPManager().State()
	state.Enabled = config.RDPEnabled
	state.Port = config.RDPPort
	state.MaxFPS = config.RDPMaxFPS
	return state
}

func rdpHasActiveSession() bool {
	if rdpManager == nil {
		return false
	}
	return rdpManager.ActiveSessions() > 0
}

func closeCurrentWebRTCSessionLocked() {
	if currentSession == nil {
		return
	}

	writeJSONRPCEvent("otherSessionConnected", nil, currentSession)
	peerConn := currentSession.peerConnection
	go func() {
		time.Sleep(1 * time.Second)
		_ = peerConn.Close()
	}()
}

func closeCurrentWebRTCSession() {
	controlSessionLock.Lock()
	defer controlSessionLock.Unlock()
	closeCurrentWebRTCSessionLocked()
}

func setCurrentSessionWithTakeover(session *Session) {
	controlSessionLock.Lock()
	closeCurrentWebRTCSessionLocked()

	// Cancel any ongoing keyboard macro when session changes.
	cancelKeyboardMacro()
	currentSession = session
	controlSessionLock.Unlock()

	if rdpManager != nil {
		rdpManager.CloseActiveSession()
	}
}

func onRDPControlSessionActivated() {
	closeCurrentWebRTCSession()

	if getActiveSessions() == 0 {
		onFirstSessionConnected()
	}
	onActiveSessionsChanged()
}

func onRDPControlSessionDeactivated() {
	if getActiveSessions() == 0 {
		onLastSessionDisconnected()
	}
	onActiveSessionsChanged()
}

func publishRDPFrame(frame []byte, duration time.Duration) {
	if rdpManager != nil {
		rdpManager.PublishFrame(frame, duration)
	}
}
