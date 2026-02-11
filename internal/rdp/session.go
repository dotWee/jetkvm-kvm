package rdp

import (
	"fmt"
	"image"
	"io"
	"net"
	"sync"
	"time"
)

// SessionState represents the current state of an RDP session.
type SessionState int

const (
	StateNegotiation SessionState = iota
	StateMCSConnect
	StateMCSSetup
	StateCapabilityExchange
	StateActive
	StateClosed
)

// String returns a human-readable session state name.
func (s SessionState) String() string {
	switch s {
	case StateNegotiation:
		return "negotiation"
	case StateMCSConnect:
		return "mcs_connect"
	case StateMCSSetup:
		return "mcs_setup"
	case StateCapabilityExchange:
		return "capability_exchange"
	case StateActive:
		return "active"
	case StateClosed:
		return "closed"
	default:
		return "unknown"
	}
}

// FrameProvider supplies screen frames to the RDP server.
type FrameProvider interface {
	// GetFrame returns the current screen content as an RGBA image.
	// Returns nil if no frame is available.
	GetFrame() *image.RGBA

	// GetResolution returns the current screen width and height.
	GetResolution() (width, height uint16)
}

// Session represents an active RDP client connection.
type Session struct {
	conn    net.Conn
	state   SessionState
	mu      sync.Mutex
	shareID uint32
	userID  uint16

	// Negotiated screen dimensions.
	width  uint16
	height uint16

	// Frame tracking for incremental updates.
	lastFrame *image.RGBA
	tileSize  int
	frameRate int

	// Callbacks.
	inputHandler  InputHandler
	frameProvider FrameProvider

	// Lifecycle.
	done chan struct{}
	log  Logger
}

// Logger is a minimal logging interface used by the RDP session.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// defaultLogger logs to nowhere.
type defaultLogger struct{}

func (defaultLogger) Debug(_ string, _ ...any) {}
func (defaultLogger) Info(_ string, _ ...any)  {}
func (defaultLogger) Warn(_ string, _ ...any)  {}
func (defaultLogger) Error(_ string, _ ...any) {}

// SessionConfig holds configuration for a new session.
type SessionConfig struct {
	Width         uint16
	Height        uint16
	TileSize      int
	FrameRate     int // Target FPS (default: 30)
	InputHandler  InputHandler
	FrameProvider FrameProvider
	Log           Logger
}

// newSession creates a new RDP session for the given connection.
func newSession(conn net.Conn, cfg SessionConfig) *Session {
	log := cfg.Log
	if log == nil {
		log = defaultLogger{}
	}

	tileSize := cfg.TileSize
	if tileSize <= 0 {
		tileSize = 64
	}

	frameRate := cfg.FrameRate
	if frameRate <= 0 {
		frameRate = 30
	}

	return &Session{
		conn:          conn,
		state:         StateNegotiation,
		width:         cfg.Width,
		height:        cfg.Height,
		tileSize:      tileSize,
		frameRate:     frameRate,
		inputHandler:  cfg.InputHandler,
		frameProvider: cfg.FrameProvider,
		done:          make(chan struct{}),
		log:           log,
		shareID:       0x10000 + uint32(time.Now().UnixNano()&0xFFFF),
	}
}

// State returns the current session state.
func (s *Session) State() SessionState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// RemoteAddr returns the remote address of the client.
func (s *Session) RemoteAddr() string {
	return s.conn.RemoteAddr().String()
}

// Close terminates the session.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state == StateClosed {
		return nil
	}
	s.state = StateClosed
	close(s.done)
	return s.conn.Close()
}

// Run executes the RDP session protocol.
func (s *Session) Run() error {
	defer s.Close()

	s.log.Info("new RDP session from %s", s.RemoteAddr())

	// Phase 1: X.224 Connection Negotiation
	if err := s.handleNegotiation(); err != nil {
		return fmt.Errorf("negotiation: %w", err)
	}

	// Phase 2: MCS Connect (simplified)
	if err := s.handleMCSConnect(); err != nil {
		return fmt.Errorf("mcs connect: %w", err)
	}

	// Phase 3: MCS channel setup
	if err := s.handleMCSSetup(); err != nil {
		return fmt.Errorf("mcs setup: %w", err)
	}

	// Phase 4: Capability exchange
	if err := s.handleCapabilityExchange(); err != nil {
		return fmt.Errorf("capability exchange: %w", err)
	}

	// Phase 5: Active session
	return s.runActiveSession()
}

// handleNegotiation processes the X.224 Connection Request and sends a Confirm.
func (s *Session) handleNegotiation() error {
	_, payload, err := readTPKT(s.conn)
	if err != nil {
		return fmt.Errorf("read connection request: %w", err)
	}

	_, nego, err := parseX224ConnectionRequest(payload)
	if err != nil {
		return fmt.Errorf("parse connection request: %w", err)
	}

	// Select protocol: prefer plain RDP (no TLS/CredSSP for embedded use)
	selectedProto := uint32(protoRDP)
	if nego != nil {
		s.log.Debug("client requested protocols: 0x%08X", nego.Protocol)
	}

	confirm := buildX224ConnectionConfirm(selectedProto)
	if err := writeTPKT(s.conn, confirm); err != nil {
		return fmt.Errorf("write connection confirm: %w", err)
	}

	s.mu.Lock()
	s.state = StateMCSConnect
	s.mu.Unlock()

	s.log.Debug("negotiation complete, selected protocol: 0x%08X", selectedProto)
	return nil
}

// handleMCSConnect handles the MCS Connect Initial/Response exchange.
func (s *Session) handleMCSConnect() error {
	// Read MCS Connect Initial from client
	_, payload, err := readTPKT(s.conn)
	if err != nil {
		return fmt.Errorf("read MCS connect initial: %w", err)
	}

	if len(payload) < 3 {
		return ErrInvalidMCS
	}

	s.log.Debug("received MCS data (%d bytes)", len(payload))

	// Send MCS Connect Response
	response := s.buildMCSConnectResponse()
	x224Data := buildX224Data(response)
	if err := writeTPKT(s.conn, x224Data); err != nil {
		return fmt.Errorf("write MCS connect response: %w", err)
	}

	s.mu.Lock()
	s.state = StateMCSSetup
	s.mu.Unlock()

	s.log.Debug("MCS connect complete")
	return nil
}

// handleMCSSetup handles Erect Domain, Attach User, and Channel Join requests.
func (s *Session) handleMCSSetup() error {
	// Process up to 10 MCS setup messages
	for i := 0; i < 10; i++ {
		_ = s.conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		_, payload, err := readTPKT(s.conn)
		if err != nil {
			return fmt.Errorf("read MCS setup: %w", err)
		}

		if len(payload) < 3 {
			continue
		}

		// X.224 data header is first 3 bytes
		mcsData := payload[3:]
		if len(mcsData) == 0 {
			continue
		}

		msgType := mcsData[0]
		s.log.Debug("MCS message type: 0x%02X", msgType)

		switch msgType >> 2 {
		case mcsTypeErectDomain >> 2:
			// Erect Domain Request - no response needed
			continue

		case mcsTypeAttachUser >> 2:
			// Attach User Request - send Attach User Confirm
			s.userID = 1001 + uint16(i)
			confirm := s.buildAttachUserConfirm()
			x224Data := buildX224Data(confirm)
			if err := writeTPKT(s.conn, x224Data); err != nil {
				return fmt.Errorf("write attach user confirm: %w", err)
			}

		case mcsTypeChannelJoin >> 2:
			// Channel Join Request - send Channel Join Confirm
			channelID := uint16(0)
			if len(mcsData) >= 5 {
				channelID = getU16LE(mcsData, 3)
			}
			confirm := s.buildChannelJoinConfirm(channelID)
			x224Data := buildX224Data(confirm)
			if err := writeTPKT(s.conn, x224Data); err != nil {
				return fmt.Errorf("write channel join confirm: %w", err)
			}

		default:
			// Check if this might be a security exchange or client info
			if s.isSecurityOrInfoPDU(mcsData) {
				s.mu.Lock()
				s.state = StateCapabilityExchange
				s.mu.Unlock()
				return nil
			}
		}
	}

	s.mu.Lock()
	s.state = StateCapabilityExchange
	s.mu.Unlock()

	return nil
}

// handleCapabilityExchange sends Demand Active PDU and processes Confirm Active.
func (s *Session) handleCapabilityExchange() error {
	// Send Server License Error PDU (valid license, skip licensing)
	if err := s.sendLicenseError(); err != nil {
		return fmt.Errorf("send license error: %w", err)
	}

	// Send Demand Active PDU
	demandActive := s.buildDemandActivePDU()
	if err := s.sendShareControlPDU(demandActive); err != nil {
		return fmt.Errorf("send demand active: %w", err)
	}

	// Read Confirm Active from client (with timeout)
	_ = s.conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, _, err := readTPKT(s.conn)
	if err != nil {
		return fmt.Errorf("read confirm active: %w", err)
	}

	// Send Synchronize + Control Cooperate + Control Granted + Font Map
	if err := s.sendActivationSequence(); err != nil {
		return fmt.Errorf("send activation sequence: %w", err)
	}

	// Read client's activation responses
	for i := 0; i < 4; i++ {
		_ = s.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, _, err := readTPKT(s.conn)
		if err != nil {
			// Some clients don't send all 4, that's OK
			break
		}
	}

	s.mu.Lock()
	s.state = StateActive
	s.mu.Unlock()

	s.log.Info("session active (%dx%d)", s.width, s.height)
	return nil
}

// runActiveSession handles the main session loop: sending frames and receiving input.
func (s *Session) runActiveSession() error {
	_ = s.conn.SetReadDeadline(time.Time{}) // Remove deadline for active session

	// Start frame sender goroutine
	go s.frameSenderLoop()

	// Main loop: read client input
	for {
		select {
		case <-s.done:
			return nil
		default:
		}

		_ = s.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		_, payload, err := readTPKT(s.conn)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				// Timeout is OK, just continue
				continue
			}
			if err == io.EOF {
				s.log.Info("client disconnected")
				return nil
			}
			return fmt.Errorf("read input: %w", err)
		}

		s.processClientData(payload)
	}
}

// frameSenderLoop periodically sends screen updates to the client.
func (s *Session) frameSenderLoop() {
	interval := time.Second / time.Duration(s.frameRate)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			if s.frameProvider == nil {
				continue
			}

			frame := s.frameProvider.GetFrame()
			if frame == nil {
				continue
			}

			var updates []BitmapUpdate
			if s.lastFrame != nil {
				updates = DiffFrames(s.lastFrame, frame, s.tileSize)
			} else {
				updates = EncodeFrame(frame, s.tileSize)
			}

			if len(updates) > 0 {
				pdu := BuildBitmapUpdatePDU(updates, s.shareID)
				if pdu != nil {
					x224Data := buildX224Data(pdu)
					if err := writeTPKT(s.conn, x224Data); err != nil {
						s.log.Warn("failed to send bitmap update: %s", err)
						return
					}
				}
			}

			s.lastFrame = frame
		}
	}
}

// processClientData handles incoming data from the client during active session.
func (s *Session) processClientData(payload []byte) {
	if len(payload) < 3 {
		return
	}

	// Skip X.224 data header (3 bytes)
	data := payload[3:]
	if len(data) < 18 { // minimum share control + share data header
		return
	}

	// Skip share control header (6 bytes) and share data header (12 bytes)
	headerSize := 18
	if len(data) < headerSize {
		return
	}

	pduType2 := data[headerSize-6] // pduType2 is at offset 12 in share data

	if pduType2 == pduTypeDataInput {
		// Parse input events
		inputData := data[headerSize:]
		events := ParseInputEvents(inputData)
		if len(events) > 0 {
			if err := DispatchInputEvents(events, s.inputHandler); err != nil {
				s.log.Warn("input dispatch error: %s", err)
			}
		}
	}
}

// isSecurityOrInfoPDU checks if the MCS data contains a security exchange or client info PDU.
func (s *Session) isSecurityOrInfoPDU(data []byte) bool {
	if len(data) < 8 {
		return false
	}
	// Check for send data request pattern
	return (data[0]>>2) == (mcsTypeSendDataRequest >> 2)
}

// buildMCSConnectResponse builds a simplified MCS Connect Response.
func (s *Session) buildMCSConnectResponse() []byte {
	// Simplified BER-encoded MCS Connect Response
	buf := make([]byte, 0, 64)

	// T.125 Connect Response (BER tag 0x7F66)
	buf = append(buf, 0x7F, 0x66)

	// We'll build the content first, then set the length
	content := make([]byte, 0, 48)

	// Result: rt-successful (0)
	content = append(content, 0x0A, 0x01, 0x00) // ENUMERATED, length 1, value 0

	// CalledConnectId: 0
	content = append(content, 0x02, 0x01, 0x00) // INTEGER, length 1, value 0

	// Domain parameters
	content = append(content, 0x30, 0x1A) // SEQUENCE
	content = append(content, 0x02, 0x01, 0x01) // maxChannelIds
	content = append(content, 0x02, 0x01, 0x01) // maxUserIds
	content = append(content, 0x02, 0x01, 0x01) // maxTokenIds
	content = append(content, 0x02, 0x01, 0x01) // numPriorities
	content = append(content, 0x02, 0x01, 0x00) // minThroughput
	content = append(content, 0x02, 0x01, 0x01) // maxHeight
	content = append(content, 0x02, 0x02, 0xFF, 0xFF) // maxMCSPDUsize
	content = append(content, 0x02, 0x01, 0x02) // protocolVersion

	// User data (OCTET STRING with GCC Conference Create Response)
	gccData := s.buildGCCResponse()
	content = append(content, 0x04)
	content = appendBERLength(content, len(gccData))
	content = append(content, gccData...)

	// Set the length of the Connect Response
	buf = appendBERLength(buf, len(content))
	buf = append(buf, content...)

	return buf
}

// buildGCCResponse builds a minimal GCC Conference Create Response.
func (s *Session) buildGCCResponse() []byte {
	buf := make([]byte, 0, 64)

	// GCC Conference Create Response (PER encoded)
	// Key: object (OID for T.124)
	buf = append(buf, 0x00, 0x05, 0x00, 0x14) // basic header
	buf = append(buf, 0x7C, 0x00, 0x01)        // Connect-Response tag

	// Server Core Data (SC_CORE)
	scCore := make([]byte, 0, 12)
	scCore = appendU16LE(scCore, 0x0C01) // type: SC_CORE
	scCore = appendU16LE(scCore, 12)     // length
	scCore = appendU32LE(scCore, 0x00080004) // version: RDP 5.0+
	scCore = appendU32LE(scCore, 0)     // clientRequestedProtocols

	// Server Security Data (SC_SECURITY)
	scSec := make([]byte, 0, 12)
	scSec = appendU16LE(scSec, 0x0C02) // type: SC_SECURITY
	scSec = appendU16LE(scSec, 12)     // length
	scSec = appendU32LE(scSec, 0)      // encryptionMethod: NONE
	scSec = appendU32LE(scSec, 0)      // encryptionLevel: NONE

	// Server Network Data (SC_NET)
	scNet := make([]byte, 0, 8)
	scNet = appendU16LE(scNet, 0x0C03) // type: SC_NET
	scNet = appendU16LE(scNet, 8)      // length
	scNet = appendU16LE(scNet, 0x03EB) // MCSChannelId
	scNet = appendU16LE(scNet, 0)      // channelCount

	userData := make([]byte, 0, len(scCore)+len(scSec)+len(scNet))
	userData = append(userData, scCore...)
	userData = append(userData, scSec...)
	userData = append(userData, scNet...)

	buf = appendU16LE(buf, uint16(len(userData)))
	buf = append(buf, userData...)

	return buf
}

// appendBERLength appends a BER length encoding.
func appendBERLength(buf []byte, length int) []byte {
	if length < 0x80 {
		return append(buf, byte(length))
	}
	if length < 0x100 {
		return append(buf, 0x81, byte(length))
	}
	return append(buf, 0x82, byte(length>>8), byte(length))
}

// buildAttachUserConfirm builds an MCS Attach User Confirm.
func (s *Session) buildAttachUserConfirm() []byte {
	buf := make([]byte, 0, 4)
	buf = append(buf, mcsTypeAttachUserConf<<2) // tag
	buf = append(buf, 0)                        // result: rt-successful
	buf = append(buf, byte(s.userID>>8), byte(s.userID))
	return buf
}

// buildChannelJoinConfirm builds an MCS Channel Join Confirm.
func (s *Session) buildChannelJoinConfirm(channelID uint16) []byte {
	buf := make([]byte, 0, 8)
	buf = append(buf, mcsTypeChannelJoinConf<<2) // tag
	buf = append(buf, 0)                          // result: rt-successful
	buf = append(buf, byte(s.userID>>8), byte(s.userID))
	buf = append(buf, byte(channelID>>8), byte(channelID)) // requested
	buf = append(buf, byte(channelID>>8), byte(channelID)) // channelId
	return buf
}

// sendLicenseError sends a valid license error PDU to skip the licensing phase.
func (s *Session) sendLicenseError() error {
	// License Error PDU (Valid Client)
	licenseData := make([]byte, 0, 20)

	// Security header
	licenseData = appendU16LE(licenseData, secLicensePkt) // flags
	licenseData = appendU16LE(licenseData, 0)              // flagsHi

	// License PDU header
	licenseData = append(licenseData, 0xFF)        // bMsgType: ERROR_ALERT
	licenseData = append(licenseData, 0x03)        // flags: ST_NO_TRANSITION
	licenseData = appendU16LE(licenseData, 16)     // wMsgSize
	licenseData = appendU32LE(licenseData, 0x0007) // dwErrorCode: STATUS_VALID_CLIENT
	licenseData = appendU32LE(licenseData, 0x0002) // dwStateTransition: ST_NO_TRANSITION
	// Blob: type + length + empty
	licenseData = appendU16LE(licenseData, 0x00) // type
	licenseData = appendU16LE(licenseData, 0)    // length

	x224Data := buildX224Data(licenseData)
	return writeTPKT(s.conn, x224Data)
}

// buildDemandActivePDU builds the Server Demand Active PDU.
func (s *Session) buildDemandActivePDU() []byte {
	caps := s.buildCapabilitySets()

	buf := make([]byte, 0, 128+len(caps))

	// Share Control Header
	// Will set totalLength after building
	startLen := len(buf)
	buf = appendU16LE(buf, 0)                // totalLength (placeholder)
	buf = appendU16LE(buf, pduTypeDemandActive) // pduType
	buf = appendU16LE(buf, 0)                // pduSource

	// Demand Active fields
	buf = appendU32LE(buf, s.shareID)   // shareId
	buf = appendU16LE(buf, 4)           // lengthSourceDescriptor
	buf = append(buf, "RDP\x00"...)     // sourceDescriptor
	buf = appendU16LE(buf, uint16(len(caps)+4)) // lengthCombinedCapabilities
	buf = appendU16LE(buf, 5)           // numberCapabilities (general, bitmap, order, pointer, input)
	buf = appendU16LE(buf, 0)           // pad2Octets
	buf = append(buf, caps...)
	buf = appendU32LE(buf, 0) // sessionId

	// Fix up totalLength
	totalLen := len(buf) - startLen
	putU16LE(buf, startLen, uint16(totalLen))

	return buf
}

// buildCapabilitySets builds the server capability sets for the Demand Active PDU.
func (s *Session) buildCapabilitySets() []byte {
	buf := make([]byte, 0, 256)

	// General Capability Set (10 fields x 2 bytes = 20 bytes)
	generalCap := make([]byte, 0, 20)
	generalCap = appendU16LE(generalCap, capGeneral) // capabilitySetType
	generalCap = appendU16LE(generalCap, 20)         // lengthCapability
	generalCap = appendU16LE(generalCap, 0x0001)     // osMajorType: UNIX
	generalCap = appendU16LE(generalCap, 0x0003)     // osMinorType
	generalCap = appendU16LE(generalCap, 0x0200)     // protocolVersion
	generalCap = appendU16LE(generalCap, 0)           // pad2OctetsA
	generalCap = appendU16LE(generalCap, 0)           // generalCompressionTypes
	generalCap = appendU16LE(generalCap, generalExtraFlags) // extraFlags
	generalCap = appendU16LE(generalCap, 0)           // updateCapabilityFlag
	generalCap = appendU16LE(generalCap, 0)           // remoteUnshareFlag
	buf = append(buf, generalCap...)

	// Bitmap Capability Set (12 fields x 2 bytes = 24 bytes)
	bitmapCap := make([]byte, 0, 24)
	bitmapCap = appendU16LE(bitmapCap, capBitmap) // capabilitySetType
	bitmapCap = appendU16LE(bitmapCap, 24)        // lengthCapability
	bitmapCap = appendU16LE(bitmapCap, 24)        // preferredBitsPerPixel
	bitmapCap = appendU16LE(bitmapCap, 1)         // receive1BitPerPixel
	bitmapCap = appendU16LE(bitmapCap, 1)         // receive4BitsPerPixel
	bitmapCap = appendU16LE(bitmapCap, 1)         // receive8BitsPerPixel
	bitmapCap = appendU16LE(bitmapCap, s.width)   // desktopWidth
	bitmapCap = appendU16LE(bitmapCap, s.height)  // desktopHeight
	bitmapCap = appendU16LE(bitmapCap, 0)         // pad2Octets
	bitmapCap = appendU16LE(bitmapCap, 1)         // desktopResizeFlag
	bitmapCap = appendU16LE(bitmapCap, 1)         // bitmapCompressionFlag
	bitmapCap = appendU16LE(bitmapCap, 0)         // highColorFlags
	buf = append(buf, bitmapCap...)

	// Order Capability Set (minimal)
	orderCap := make([]byte, 0, 88)
	orderCap = appendU16LE(orderCap, capOrder) // capabilitySetType
	orderCap = appendU16LE(orderCap, 88)       // lengthCapability
	orderCap = append(orderCap, make([]byte, 84)...)
	buf = append(buf, orderCap...)

	// Pointer Capability Set
	pointerCap := make([]byte, 0, 10)
	pointerCap = appendU16LE(pointerCap, capPointer) // capabilitySetType
	pointerCap = appendU16LE(pointerCap, 10)         // lengthCapability
	pointerCap = appendU16LE(pointerCap, 1)          // colorPointerFlag
	pointerCap = appendU16LE(pointerCap, 20)         // colorPointerCacheSize
	pointerCap = appendU16LE(pointerCap, 20)         // pointerCacheSize
	buf = append(buf, pointerCap...)

	// Input Capability Set
	inputCap := make([]byte, 0, 88)
	inputCap = appendU16LE(inputCap, capInput)  // capabilitySetType
	inputCap = appendU16LE(inputCap, 88)        // lengthCapability
	inputCap = appendU16LE(inputCap, 0x0035)    // inputFlags (scancode + mouse + fastpath)
	inputCap = appendU16LE(inputCap, 0)         // pad2OctetsA
	inputCap = appendU32LE(inputCap, 0x00000409) // keyboardLayout (US English)
	inputCap = appendU32LE(inputCap, 0x00000004) // keyboardType (Enhanced 101)
	inputCap = appendU32LE(inputCap, 0)         // keyboardSubType
	inputCap = appendU32LE(inputCap, 12)        // keyboardFunctionKey
	inputCap = append(inputCap, make([]byte, 64)...) // imeFileName (64 bytes)
	buf = append(buf, inputCap...)

	return buf
}

// sendShareControlPDU wraps a PDU in X.224 and TPKT and sends it.
func (s *Session) sendShareControlPDU(pdu []byte) error {
	x224Data := buildX224Data(pdu)
	return writeTPKT(s.conn, x224Data)
}

// sendActivationSequence sends Synchronize + Control Cooperate + Control Granted + Font Map.
func (s *Session) sendActivationSequence() error {
	// Synchronize PDU
	syncData := make([]byte, 0, 4)
	syncData = appendU16LE(syncData, 1) // messageType: SYNCMSGTYPE_SYNC
	syncData = appendU16LE(syncData, uint16(s.userID))
	syncPDU := buildShareDataPDU(s.shareID, pduTypeDataSynchronize, syncData)
	if err := s.sendShareControlPDU(syncPDU); err != nil {
		return err
	}

	// Control Cooperate PDU
	ctrlCoopData := make([]byte, 0, 8)
	ctrlCoopData = appendU16LE(ctrlCoopData, 0x0004) // action: CTRLACTION_COOPERATE
	ctrlCoopData = appendU16LE(ctrlCoopData, 0)      // grantId
	ctrlCoopData = appendU32LE(ctrlCoopData, 0)      // controlId
	ctrlCoopPDU := buildShareDataPDU(s.shareID, pduTypeDataControl, ctrlCoopData)
	if err := s.sendShareControlPDU(ctrlCoopPDU); err != nil {
		return err
	}

	// Control Granted PDU
	ctrlGrantData := make([]byte, 0, 8)
	ctrlGrantData = appendU16LE(ctrlGrantData, 0x0002)             // action: CTRLACTION_GRANTED_CONTROL
	ctrlGrantData = appendU16LE(ctrlGrantData, uint16(s.userID))   // grantId
	ctrlGrantData = appendU32LE(ctrlGrantData, 0x03EA)             // controlId
	ctrlGrantPDU := buildShareDataPDU(s.shareID, pduTypeDataControl, ctrlGrantData)
	if err := s.sendShareControlPDU(ctrlGrantPDU); err != nil {
		return err
	}

	// Font Map PDU
	fontMapData := make([]byte, 0, 8)
	fontMapData = appendU16LE(fontMapData, 0)      // numberEntries
	fontMapData = appendU16LE(fontMapData, 0)      // totalNumEntries
	fontMapData = appendU16LE(fontMapData, 0x0003) // mapFlags
	fontMapData = appendU16LE(fontMapData, 0x0004) // entrySize
	fontMapPDU := buildShareDataPDU(s.shareID, pduTypeDataFontMap, fontMapData)
	return s.sendShareControlPDU(fontMapPDU)
}
