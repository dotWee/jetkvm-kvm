package rfb

import (
	"crypto/des" //nolint:gosec // DES is required by the VNC authentication protocol (RFC 6143 §7.2.2)
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
)

// SecurityHandler handles a security handshake for one security type.
type SecurityHandler interface {
	// Type returns the RFB security type number.
	Type() uint8
	// Handle performs the security handshake on the connection.
	// Returns nil on success, or an error on failure.
	Handle(rw io.ReadWriter) error
}

// SecurityNone implements the "None" security type (type 1) per RFC 6143 §7.2.1.
type SecurityNone struct{}

func (s *SecurityNone) Type() uint8 { return SecTypeNone }

func (s *SecurityNone) Handle(rw io.ReadWriter) error {
	// SecurityResult: OK
	return writeSecurityResult(rw, SecurityResultOK, "")
}

// PasswordVerifier is a function that checks a plaintext password candidate.
// Returns true if the password is correct.
type PasswordVerifier func(password []byte) bool

// SecurityVNCAuth implements VNC Authentication (type 2) per RFC 6143 §7.2.2.
// The server sends a 16-byte random challenge. The client encrypts it with DES
// using the password as key and sends back the 16-byte response.
type SecurityVNCAuth struct {
	// Verify is called with the password candidate extracted from the
	// DES challenge-response. Because VNC auth uses DES (which requires
	// knowing the plaintext password), and we store bcrypt hashes, we
	// actually need to store a separate VNC password.
	// The password bytes are the raw 8-byte (max) password from the client.
	Password []byte
}

func (s *SecurityVNCAuth) Type() uint8 { return SecTypeVNCAuth }

func (s *SecurityVNCAuth) Handle(rw io.ReadWriter) error {
	// Generate 16-byte random challenge
	challenge := make([]byte, VNCAuthChallengeSize)
	if _, err := rand.Read(challenge); err != nil {
		return fmt.Errorf("failed to generate challenge: %w", err)
	}

	// Send challenge to client
	if _, err := rw.Write(challenge); err != nil {
		return fmt.Errorf("failed to send challenge: %w", err)
	}

	// Read 16-byte response from client
	response := make([]byte, VNCAuthChallengeSize)
	if _, err := io.ReadFull(rw, response); err != nil {
		return fmt.Errorf("failed to read auth response: %w", err)
	}

	// Compute expected response using the stored password
	expected, err := encryptChallenge(challenge, s.Password)
	if err != nil {
		return writeSecurityResult(rw, SecurityResultFailed, "internal authentication error")
	}

	// Compare
	match := true
	for i := range VNCAuthChallengeSize {
		if response[i] != expected[i] {
			match = false
		}
	}

	if !match {
		return writeSecurityResult(rw, SecurityResultFailed, "authentication failed")
	}

	return writeSecurityResult(rw, SecurityResultOK, "")
}

// EncryptChallenge encrypts a VNC auth challenge using the given password.
// This is exported for testing purposes.
func EncryptChallenge(challenge, password []byte) ([]byte, error) {
	return encryptChallenge(challenge, password)
}

// encryptChallenge encrypts a VNC auth challenge with DES.
// The password is truncated or zero-padded to 8 bytes, with bits reversed
// within each byte before use as a DES key.
func encryptChallenge(challenge, password []byte) ([]byte, error) {
	// Prepare 8-byte key from password
	key := make([]byte, 8)
	copy(key, password) // truncate or zero-pad to 8 bytes

	// Reverse bits in each byte (VNC protocol requirement)
	for i := range key {
		key[i] = reverseBits(key[i])
	}

	// Create DES cipher
	block, err := des.NewCipher(key) //nolint:gosec // Required by VNC protocol
	if err != nil {
		return nil, fmt.Errorf("failed to create DES cipher: %w", err)
	}

	// Encrypt each 8-byte block of the challenge
	result := make([]byte, VNCAuthChallengeSize)
	block.Encrypt(result[0:8], challenge[0:8])
	block.Encrypt(result[8:16], challenge[8:16])

	return result, nil
}

// reverseBits reverses the bit order within a byte.
// VNC protocol requires this for the DES key.
func reverseBits(b byte) byte {
	var result byte
	for i := 0; i < 8; i++ {
		result = (result << 1) | (b & 1)
		b >>= 1
	}
	return result
}

// writeSecurityResult sends a SecurityResult message to the client.
func writeSecurityResult(w io.Writer, result uint32, reason string) error {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, result)
	if _, err := w.Write(buf); err != nil {
		return fmt.Errorf("failed to write security result: %w", err)
	}

	// If failed, send reason string (RFB 3.8+)
	if result != SecurityResultOK {
		reasonBytes := []byte(reason)
		reasonLen := make([]byte, 4)
		binary.BigEndian.PutUint32(reasonLen, uint32(len(reasonBytes)))
		if _, err := w.Write(reasonLen); err != nil {
			return fmt.Errorf("failed to write reason length: %w", err)
		}
		if len(reasonBytes) > 0 {
			if _, err := w.Write(reasonBytes); err != nil {
				return fmt.Errorf("failed to write reason: %w", err)
			}
		}
		return fmt.Errorf("security failed: %s", reason)
	}

	return nil
}

// NegotiateSecurity performs the server-side security negotiation.
// It sends the list of supported security types, reads the client's choice,
// and runs the selected handler.
func NegotiateSecurity(rw io.ReadWriter, handlers []SecurityHandler) error {
	if len(handlers) == 0 {
		// Send 0 types + reason
		buf := []byte{0}
		if _, err := rw.Write(buf); err != nil {
			return err
		}
		reason := "no security types available"
		reasonBuf := make([]byte, 4+len(reason))
		binary.BigEndian.PutUint32(reasonBuf[0:4], uint32(len(reason)))
		copy(reasonBuf[4:], reason)
		_, _ = rw.Write(reasonBuf)
		return fmt.Errorf("no security handlers configured")
	}

	// Send number of security types + type list
	buf := make([]byte, 1+len(handlers))
	buf[0] = uint8(len(handlers))
	for i, h := range handlers {
		buf[1+i] = h.Type()
	}
	if _, err := rw.Write(buf); err != nil {
		return fmt.Errorf("failed to send security types: %w", err)
	}

	// Read client's choice
	chosen := make([]byte, 1)
	if _, err := io.ReadFull(rw, chosen); err != nil {
		return fmt.Errorf("failed to read security type choice: %w", err)
	}

	// Find and run the chosen handler
	for _, h := range handlers {
		if h.Type() == chosen[0] {
			return h.Handle(rw)
		}
	}

	return fmt.Errorf("client selected unsupported security type: %d", chosen[0])
}
