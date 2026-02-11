package vnc

import (
	"crypto/des"    //nolint:gosec // DES is required by the VNC authentication protocol (RFB spec)
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"io"
)

// vncAuthChallenge generates a 16-byte random challenge for VNC authentication.
func vncAuthChallenge() ([]byte, error) {
	challenge := make([]byte, 16)
	if _, err := rand.Read(challenge); err != nil {
		return nil, err
	}
	return challenge, nil
}

// vncAuthVerify verifies the VNC authentication response against the challenge and password.
// It returns true if the response matches the expected DES-encrypted challenge.
// Uses constant-time comparison to prevent timing attacks.
func vncAuthVerify(challenge, response []byte, password string) bool {
	expected := vncAuthEncrypt(challenge, password)
	if len(response) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare(response, expected) == 1
}

// vncAuthEncrypt performs VNC DES encryption of the challenge with the password.
// The password is truncated to 8 chars or zero-padded, and each byte is bit-reversed
// as per the VNC/RFB specification.
func vncAuthEncrypt(challenge []byte, password string) []byte {
	// VNC password is exactly 8 bytes, truncated or zero-padded
	key := make([]byte, 8)
	if len(password) > 8 {
		copy(key, password[:8])
	} else {
		copy(key, password)
	}

	// Reverse bits in each byte (VNC DES key quirk)
	for i := range key {
		key[i] = reverseBits(key[i])
	}

	block, err := des.NewCipher(key) //nolint:gosec // DES is required by the VNC authentication protocol
	if err != nil {
		return nil
	}

	result := make([]byte, 16)
	block.Encrypt(result[0:8], challenge[0:8])
	block.Encrypt(result[8:16], challenge[8:16])
	return result
}

// reverseBits reverses the bit order of a byte (required by VNC DES key handling).
func reverseBits(b byte) byte {
	var result byte
	for i := 0; i < 8; i++ {
		result = (result << 1) | (b & 1)
		b >>= 1
	}
	return result
}

// performAuth handles the server-side authentication handshake.
// Returns nil on success.
func performAuth(rw io.ReadWriter, password string) error {
	if password == "" {
		// No authentication required
		secTypes := []byte{1, secTypeNone}
		if _, err := rw.Write(secTypes); err != nil {
			return err
		}

		// Read client's chosen security type
		chosenType := make([]byte, 1)
		if _, err := io.ReadFull(rw, chosenType); err != nil {
			return err
		}
		if chosenType[0] != secTypeNone {
			return fmt.Errorf("client chose unsupported security type: %d", chosenType[0])
		}

		// Send SecurityResult (0 = OK)
		result := []byte{0, 0, 0, 0}
		_, err := rw.Write(result)
		return err
	}

	// VNC authentication
	secTypes := []byte{1, secTypeVNCAuth}
	if _, err := rw.Write(secTypes); err != nil {
		return err
	}

	// Read client's chosen security type
	chosenType := make([]byte, 1)
	if _, err := io.ReadFull(rw, chosenType); err != nil {
		return err
	}
	if chosenType[0] != secTypeVNCAuth {
		return fmt.Errorf("client chose unsupported security type: %d", chosenType[0])
	}

	// Send challenge
	challenge, err := vncAuthChallenge()
	if err != nil {
		return err
	}
	if _, err := rw.Write(challenge); err != nil {
		return err
	}

	// Read response
	response := make([]byte, 16)
	if _, err := io.ReadFull(rw, response); err != nil {
		return err
	}

	if !vncAuthVerify(challenge, response, password) {
		// Send failure
		result := []byte{0, 0, 0, 1}
		_, _ = rw.Write(result)
		return ErrAuthFailed
	}

	// Send success
	result := []byte{0, 0, 0, 0}
	_, err = rw.Write(result)
	return err
}
