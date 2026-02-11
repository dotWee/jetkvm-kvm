package rfb

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadSetPixelFormat(t *testing.T) {
	pf := PixelFormat{
		BitsPerPixel: 16, Depth: 16, BigEndianFlag: 0, TrueColorFlag: 1,
		RedMax: 31, GreenMax: 63, BlueMax: 31,
		RedShift: 11, GreenShift: 5, BlueShift: 0,
	}

	// 3 bytes padding + 16 bytes pixel format
	buf := make([]byte, 3+16)
	copy(buf[3:], pf.Marshal())

	msg, err := ReadSetPixelFormat(bytes.NewReader(buf))
	require.NoError(t, err)
	assert.Equal(t, pf, msg.Format)
}

func TestReadSetPixelFormatTooShort(t *testing.T) {
	_, err := ReadSetPixelFormat(bytes.NewReader([]byte{0, 0}))
	assert.Error(t, err)
}

func TestReadSetEncodings(t *testing.T) {
	// 1 byte padding + 2 bytes count + 4 bytes per encoding
	encodings := []int32{EncodingRaw, EncodingZRLE, EncodingDesktopSize}
	buf := make([]byte, 3+4*len(encodings))
	buf[0] = 0 // padding
	binary.BigEndian.PutUint16(buf[1:3], uint16(len(encodings)))
	for i, enc := range encodings {
		binary.BigEndian.PutUint32(buf[3+i*4:], uint32(enc))
	}

	msg, err := ReadSetEncodings(bytes.NewReader(buf))
	require.NoError(t, err)
	assert.Equal(t, encodings, msg.Encodings)
}

func TestReadSetEncodingsEmpty(t *testing.T) {
	buf := make([]byte, 3)
	buf[0] = 0
	binary.BigEndian.PutUint16(buf[1:3], 0)

	msg, err := ReadSetEncodings(bytes.NewReader(buf))
	require.NoError(t, err)
	assert.Empty(t, msg.Encodings)
}

func TestReadSetEncodingsTooMany(t *testing.T) {
	buf := make([]byte, 3)
	buf[0] = 0
	binary.BigEndian.PutUint16(buf[1:3], 2000) // > 1024 limit

	_, err := ReadSetEncodings(bytes.NewReader(buf))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too many encodings")
}

func TestReadFramebufferUpdateRequest(t *testing.T) {
	// 9 bytes: incremental(1) + x(2) + y(2) + width(2) + height(2)
	buf := make([]byte, 9)
	buf[0] = 1 // incremental
	binary.BigEndian.PutUint16(buf[1:3], 100)
	binary.BigEndian.PutUint16(buf[3:5], 200)
	binary.BigEndian.PutUint16(buf[5:7], 640)
	binary.BigEndian.PutUint16(buf[7:9], 480)

	msg, err := ReadFramebufferUpdateRequest(bytes.NewReader(buf))
	require.NoError(t, err)
	assert.True(t, msg.Incremental)
	assert.Equal(t, uint16(100), msg.X)
	assert.Equal(t, uint16(200), msg.Y)
	assert.Equal(t, uint16(640), msg.Width)
	assert.Equal(t, uint16(480), msg.Height)
}

func TestReadFramebufferUpdateRequestNonIncremental(t *testing.T) {
	buf := make([]byte, 9)
	buf[0] = 0 // non-incremental
	binary.BigEndian.PutUint16(buf[1:3], 0)
	binary.BigEndian.PutUint16(buf[3:5], 0)
	binary.BigEndian.PutUint16(buf[5:7], 1920)
	binary.BigEndian.PutUint16(buf[7:9], 1080)

	msg, err := ReadFramebufferUpdateRequest(bytes.NewReader(buf))
	require.NoError(t, err)
	assert.False(t, msg.Incremental)
	assert.Equal(t, uint16(0), msg.X)
	assert.Equal(t, uint16(0), msg.Y)
	assert.Equal(t, uint16(1920), msg.Width)
	assert.Equal(t, uint16(1080), msg.Height)
}

func TestReadKeyEvent(t *testing.T) {
	// 7 bytes: down(1) + padding(2) + keysym(4)
	buf := make([]byte, 7)
	buf[0] = 1                                   // down
	binary.BigEndian.PutUint32(buf[3:7], 0xFF0D) // Return key

	msg, err := ReadKeyEvent(bytes.NewReader(buf))
	require.NoError(t, err)
	assert.True(t, msg.DownFlag)
	assert.Equal(t, uint32(0xFF0D), msg.Key)
}

func TestReadKeyEventRelease(t *testing.T) {
	buf := make([]byte, 7)
	buf[0] = 0                                   // up
	binary.BigEndian.PutUint32(buf[3:7], 0x0061) // 'a'

	msg, err := ReadKeyEvent(bytes.NewReader(buf))
	require.NoError(t, err)
	assert.False(t, msg.DownFlag)
	assert.Equal(t, uint32(0x0061), msg.Key)
}

func TestReadPointerEvent(t *testing.T) {
	// 5 bytes: button-mask(1) + x(2) + y(2)
	buf := make([]byte, 5)
	buf[0] = 0x01 // left button
	binary.BigEndian.PutUint16(buf[1:3], 500)
	binary.BigEndian.PutUint16(buf[3:5], 300)

	msg, err := ReadPointerEvent(bytes.NewReader(buf))
	require.NoError(t, err)
	assert.Equal(t, uint8(0x01), msg.ButtonMask)
	assert.Equal(t, uint16(500), msg.X)
	assert.Equal(t, uint16(300), msg.Y)
}

func TestReadPointerEventNoButtons(t *testing.T) {
	buf := make([]byte, 5)
	buf[0] = 0x00
	binary.BigEndian.PutUint16(buf[1:3], 0)
	binary.BigEndian.PutUint16(buf[3:5], 0)

	msg, err := ReadPointerEvent(bytes.NewReader(buf))
	require.NoError(t, err)
	assert.Equal(t, uint8(0), msg.ButtonMask)
}

func TestReadClientCutText(t *testing.T) {
	text := "Hello, clipboard!"
	// 3 bytes padding + 4 bytes length + text
	buf := make([]byte, 7+len(text))
	binary.BigEndian.PutUint32(buf[3:7], uint32(len(text)))
	copy(buf[7:], text)

	msg, err := ReadClientCutText(bytes.NewReader(buf))
	require.NoError(t, err)
	assert.Equal(t, text, msg.Text)
}

func TestReadClientCutTextEmpty(t *testing.T) {
	buf := make([]byte, 7)
	binary.BigEndian.PutUint32(buf[3:7], 0)

	msg, err := ReadClientCutText(bytes.NewReader(buf))
	require.NoError(t, err)
	assert.Equal(t, "", msg.Text)
}

func TestReadClientCutTextTooLarge(t *testing.T) {
	buf := make([]byte, 7)
	binary.BigEndian.PutUint32(buf[3:7], 20*1024*1024) // 20MB > 10MB limit

	_, err := ReadClientCutText(bytes.NewReader(buf))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too large")
}

func TestWriteFramebufferUpdate(t *testing.T) {
	var buf bytes.Buffer
	err := WriteFramebufferUpdate(&buf, 3)
	require.NoError(t, err)

	data := buf.Bytes()
	assert.Len(t, data, 4)
	assert.Equal(t, byte(MsgFramebufferUpdate), data[0])
	assert.Equal(t, byte(0), data[1]) // padding
	assert.Equal(t, uint16(3), binary.BigEndian.Uint16(data[2:4]))
}

func TestWriteBell(t *testing.T) {
	var buf bytes.Buffer
	err := WriteBell(&buf)
	require.NoError(t, err)
	assert.Equal(t, []byte{MsgBell}, buf.Bytes())
}

func TestWriteServerCutText(t *testing.T) {
	var buf bytes.Buffer
	text := "copied text"
	err := WriteServerCutText(&buf, text)
	require.NoError(t, err)

	data := buf.Bytes()
	assert.Equal(t, byte(MsgServerCutText), data[0])
	// padding bytes 1-3
	assert.Equal(t, uint32(len(text)), binary.BigEndian.Uint32(data[4:8]))
	assert.Equal(t, text, string(data[8:]))
}

func TestWriteServerCutTextEmpty(t *testing.T) {
	var buf bytes.Buffer
	err := WriteServerCutText(&buf, "")
	require.NoError(t, err)

	data := buf.Bytes()
	assert.Equal(t, uint32(0), binary.BigEndian.Uint32(data[4:8]))
	assert.Len(t, data, 8)
}
