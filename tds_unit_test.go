package mssql

import (
	"context"
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/microsoft/go-mssqldb/msdsn"
	"github.com/stretchr/testify/assert"
)

func TestParseDAC(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		msg      []byte
		instance string
		wantPort string // empty means no entry is expected
	}{
		{
			name:     "valid DAC response",
			msg:      []byte{5, 6, 0, 1, 0x59, 0x05}, // Port 1369 (0x0559) little-endian
			instance: "testinstance",
			wantPort: "1369",
		},
		{
			// The worked example from MC-SQLR 4.3.
			name:     "spec example response",
			msg:      []byte{0x05, 0x06, 0x00, 0x01, 0x32, 0xDF},
			instance: "YUKONSTD",
			wantPort: "57138",
		},
		{
			name:     "empty message",
			msg:      []byte{},
			instance: "testinstance",
		},
		{
			name:     "wrong first byte",
			msg:      []byte{4, 6, 0, 1, 0x59, 0x05},
			instance: "testinstance",
		},
		{
			name:     "too short message",
			msg:      []byte{5, 6, 0, 1, 0x59},
			instance: "testinstance",
		},
		{
			name:     "too long message",
			msg:      []byte{5, 6, 0, 1, 0x59, 0x05, 0x00},
			instance: "testinstance",
		},
		{
			// RESP_SIZE MUST be 0x0006 (MC-SQLR 2.2.6).
			name:     "wrong resp size",
			msg:      []byte{5, 3, 0, 1, 0x59, 0x05},
			instance: "testinstance",
		},
		{
			// PROTOCOLVERSION MUST be 0x01 (MC-SQLR 2.2.6).
			name:     "wrong protocol version",
			msg:      []byte{5, 6, 0, 0, 0x59, 0x05},
			instance: "testinstance",
		},
		{
			// Port 0 is not a listening endpoint, and resolveServerPort would
			// silently turn it into the default TDS port.
			name:     "zero port",
			msg:      createValidDACResponse(0),
			instance: "testinstance",
		},
		{
			name:     "case insensitive instance",
			msg:      createValidDACResponse(1433),
			instance: "MyInstance",
			wantPort: "1433",
		},
		{
			name:     "max port",
			msg:      createValidDACResponse(65535),
			instance: "testinstance",
			wantPort: "65535",
		},
		{
			name:     "empty instance name",
			msg:      createValidDACResponse(1434),
			instance: "",
			wantPort: "1434",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := parseDAC(tt.msg, tt.instance)
			if tt.wantPort == "" {
				assert.Empty(t, results, "expected no browser data")
				return
			}
			key := strings.ToUpper(tt.instance)
			assert.Len(t, results, 1, "browser data length")
			assert.Equal(t, tt.wantPort, results[key]["tcp"], "tcp port")
			// ParseBrowserData matches on InstanceName, so it must be present.
			assert.Equal(t, key, results[key]["InstanceName"], "instance name")
		})
	}
}

// TestParseDACParseBrowserData verifies that the data parseDAC returns is
// actually consumable by the dialer that resolves the DAC port.
func TestParseDACParseBrowserData(t *testing.T) {
	t.Parallel()

	p := &msdsn.Config{Host: "localhost", Instance: "MyInstance"}
	err := tcpDialer{}.ParseBrowserData(parseDAC(createValidDACResponse(1434), p.Instance), p)
	assert.NoError(t, err, "ParseBrowserData")
	assert.Equal(t, uint64(1434), p.Port, "resolved DAC port")
}

// TestParseDACZeroPortNotDefaulted documents why a zero TCP_DAC_PORT is
// rejected outright: if it were accepted, ParseBrowserData would store it as-is
// and resolveServerPort would rewrite it to the default TDS port, quietly
// connecting to the regular endpoint instead of the DAC one.
func TestParseDACZeroPortNotDefaulted(t *testing.T) {
	t.Parallel()

	p := &msdsn.Config{Host: "localhost", Instance: "MyInstance"}
	err := tcpDialer{}.ParseBrowserData(parseDAC(createValidDACResponse(0), p.Instance), p)
	assert.Error(t, err, "a zero DAC port must not resolve")
	assert.Zero(t, p.Port, "port must be left unset")
	assert.Equal(t, uint64(defaultServerPort), resolveServerPort(p.Port),
		"an unset port silently becomes the default TDS port, which is what rejecting port 0 avoids")
}

// Helper to create a valid SVR_RESP (DAC) message per MC-SQLR 2.2.6:
// SVR_RESP (0x05), RESP_SIZE (whole-packet length, 0x0006), PROTOCOLVERSION
// (0x01), then the little-endian TCP_DAC_PORT at offset 4.
func createValidDACResponse(port uint16) []byte {
	msg := make([]byte, 6)
	msg[0] = 5
	binary.LittleEndian.PutUint16(msg[1:3], 6)
	msg[3] = 1
	binary.LittleEndian.PutUint16(msg[4:6], port)
	return msg
}

// TestGetInstancesDACResponseSize exercises the real UDP read path behind
// parseDAC. A datagram larger than the receive buffer is truncated rather than
// reported on Unix, so the buffer must be big enough for an oversized reply to
// stay observable; otherwise a non-conforming reply would be trimmed down to a
// well-formed looking 6 bytes and accepted. Windows instead fails the read with
// WSAEMSGSIZE, so the assertion here is the platform-independent one: an
// oversized reply must never resolve a DAC port.
func TestGetInstancesDACResponseSize(t *testing.T) {
	t.Parallel()

	valid := createValidDACResponse(1434)

	tests := []struct {
		name     string
		reply    []byte
		wantPort string // empty means the reply must be rejected
	}{
		{
			name:     "exactly sized reply",
			reply:    valid,
			wantPort: "1434",
		},
		{
			name:  "oversized reply with valid prefix",
			reply: append(append([]byte{}, valid...), 0xFF, 0xFF, 0xFF),
		},
		{
			name:  "truncated reply",
			reply: valid[:5],
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			d := newUDPResponder(t, tt.reply)
			results, err := getInstances(ctx, d, "127.0.0.1", msdsn.BrowserDAC, "MyInstance")

			if tt.wantPort == "" {
				// The read itself may fail instead, which rejects the reply too.
				assert.Empty(t, results, "expected the reply to be rejected")
				return
			}
			assert.NoError(t, err, "getInstances")
			assert.Equal(t, tt.wantPort, results["MYINSTANCE"]["tcp"], "tcp port")
		})
	}
}

// newUDPResponder starts a loopback UDP server that answers the first datagram
// it receives with reply, records that datagram, and acts as a Dialer targeting
// itself.
func newUDPResponder(t *testing.T, reply []byte) *udpResponder {
	t.Helper()

	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot bind a loopback UDP socket: %v", err)
	}
	t.Cleanup(func() { _ = pc.Close() })

	r := &udpResponder{addr: pc.LocalAddr().String(), requests: make(chan []byte, 1)}
	go func() {
		buf := make([]byte, 1024)
		n, peer, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		select {
		case r.requests <- append([]byte(nil), buf[:n]...):
		default:
		}
		_, _ = pc.WriteTo(reply, peer)
	}()

	return r
}

// udpResponder ignores the requested address so the test does not need to bind
// the real SQL Server Browser port.
type udpResponder struct {
	addr     string
	requests chan []byte
}

func (d *udpResponder) DialContext(ctx context.Context, network, _ string) (net.Conn, error) {
	var nd net.Dialer
	return nd.DialContext(ctx, network, d.addr)
}

// request returns the datagram the responder received.
func (d *udpResponder) request(t *testing.T) []byte {
	t.Helper()

	select {
	case req := <-d.requests:
		return req
	case <-time.After(10 * time.Second):
		t.Fatal("no request reached the responder")
		return nil
	}
}

// TestGetInstancesDACRequest pins the bytes actually put on the wire. MC-SQLR
// 2.2.4 defines CLNT_UCAST_DAC as 0x0F, PROTOCOLVERSION 0x01, then a
// null-terminated instance name, so the name starts at offset 2 and the packet
// ends with the terminator.
func TestGetInstancesDACRequest(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	d := newUDPResponder(t, createValidDACResponse(57138))
	_, err := getInstances(ctx, d, "127.0.0.1", msdsn.BrowserDAC, "YUKONSTD")
	assert.NoError(t, err, "getInstances")

	// The worked example from MC-SQLR 4.3: 0f 01 "YUKONSTD" 00.
	want := []byte{0x0F, 0x01, 'Y', 'U', 'K', 'O', 'N', 'S', 'T', 'D', 0x00}
	assert.Equal(t, want, d.request(t), "CLNT_UCAST_DAC request")
}

func TestParseInstances(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		msg  []byte
	}{
		{
			name: "empty message",
			msg:  []byte{},
		},
		{
			name: "wrong first byte",
			msg:  []byte{4, 0, 0, 0},
		},
		{
			name: "too short message",
			msg:  []byte{5, 0},
		},
		{
			name: "single instance response",
			// Format: 0x05 + 2 bytes length + semicolon-delimited key-value pairs
			msg: createBrowserResponse("MSSQLSERVER", "1433", "sql/query"),
		},
		{
			name: "no instances just header",
			msg:  []byte{5, 0, 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just verify we don't panic
			_ = parseInstances(tt.msg)
		})
	}
}

// createBrowserResponse creates a mock SQL Browser response message
func createBrowserResponse(instanceName, tcpPort, pipeName string) []byte {
	// Format: key1;value1;key2;value2;;key1;value1;...
	data := "InstanceName;" + instanceName + ";tcp;" + tcpPort + ";np;" + pipeName + ";;"
	msg := make([]byte, 3+len(data))
	msg[0] = 5
	msg[1] = byte(len(data) & 0xFF)
	msg[2] = byte(len(data) >> 8)
	copy(msg[3:], data)
	return msg
}

func TestStr2ucs2(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected []byte
	}{
		{
			name:     "empty string",
			input:    "",
			expected: []byte{},
		},
		{
			name:     "simple ASCII",
			input:    "A",
			expected: []byte{0x41, 0x00}, // 'A' in UTF-16LE
		},
		{
			name:     "hello",
			input:    "hello",
			expected: []byte{0x68, 0x00, 0x65, 0x00, 0x6c, 0x00, 0x6c, 0x00, 0x6f, 0x00},
		},
		{
			name:     "unicode character",
			input:    "日",
			expected: []byte{0xe5, 0x65}, // U+65E5 in little-endian
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := str2ucs2(tt.input)
			assert.Equal(t, tt.expected, result, "str2ucs2(%q)", tt.input)
		})
	}
}

func TestManglePassword(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "empty password",
			input: "",
		},
		{
			name:  "simple password",
			input: "password",
		},
		{
			name:  "complex password",
			input: "P@$$w0rd!123",
		},
		{
			name:  "unicode password",
			input: "пароль", // Russian word for "password"
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := manglePassword(tt.input)
			// Result should be 2x the rune count (UTF-16)
			expected := len(str2ucs2(tt.input))
			assert.Len(t, result, expected, "manglePassword(%q) length", tt.input)
		})
	}
}

func TestIsEncryptedFlag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    uint16
		expected bool
	}{
		{name: "not encrypted", value: 0, expected: false},
		{name: "encrypted flag set", value: colFlagEncrypted, expected: true},
		{name: "other flags only", value: 0x0001, expected: false},
		{name: "multiple flags with encrypted", value: colFlagEncrypted | 0x0001, expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isEncryptedFlag(tt.value)
			assert.Equal(t, tt.expected, result, "isEncryptedFlag(%d)", tt.value)
		})
	}
}

func TestColumnStructIsEncrypted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		flags    uint16
		expected bool
	}{
		{name: "not encrypted", flags: 0, expected: false},
		{name: "encrypted", flags: colFlagEncrypted, expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			col := columnStruct{Flags: tt.flags}
			result := col.isEncrypted()
			assert.Equal(t, tt.expected, result, "columnStruct.isEncrypted()")
		})
	}
}

func TestKeySliceSorting(t *testing.T) {
	t.Parallel()

	keys := keySlice{3, 1, 2}

	// Check Len
	assert.Equal(t, 3, keys.Len(), "keySlice.Len()")

	// Check Less
	assert.True(t, keys.Less(1, 0), "keySlice.Less(1, 0) should be true (1 < 3)")

	// Check Swap
	keys.Swap(0, 1)
	assert.Equal(t, uint8(1), keys[0], "keySlice[0] after swap")
	assert.Equal(t, uint8(3), keys[1], "keySlice[1] after swap")
}

func TestLoginHeaderCreation(t *testing.T) {
	t.Parallel()

	hdr := loginHeader{
		TDSVersion:   verTDS74,
		PacketSize:   4096,
		ClientPID:    12345,
		OptionFlags1: fUseDB,
	}

	assert.Equal(t, uint32(verTDS74), hdr.TDSVersion, "loginHeader.TDSVersion")
	assert.Equal(t, uint32(4096), hdr.PacketSize, "loginHeader.PacketSize")
}

func TestFeatureExtsAdd(t *testing.T) {
	t.Parallel()

	t.Run("add nil feature", func(t *testing.T) {
		var fe featureExts
		err := fe.Add(nil)
		assert.NoError(t, err, "Add(nil) should return nil")
	})

	t.Run("add first feature", func(t *testing.T) {
		var fe featureExts
		ce := &featureExtColumnEncryption{}
		err := fe.Add(ce)
		assert.NoError(t, err, "Add()")
		assert.NotNil(t, fe.features, "Add() should initialize the features map")
		assert.Len(t, fe.features, 1, "features length")
	})

	t.Run("add duplicate feature returns error", func(t *testing.T) {
		var fe featureExts
		ce := &featureExtColumnEncryption{}
		_ = fe.Add(ce)
		// Try to add the same feature ID again
		err := fe.Add(ce)
		assert.Error(t, err, "Add() should return error for duplicate feature ID")
	})

	t.Run("add multiple different features", func(t *testing.T) {
		var fe featureExts
		ce := &featureExtColumnEncryption{}
		fa := &featureExtFedAuth{}
		_ = fe.Add(ce)
		err := fe.Add(fa)
		assert.NoError(t, err, "Add() for second feature")
		assert.Len(t, fe.features, 2, "features length")
	})
}

func TestColEncryptionFeatureExtID(t *testing.T) {
	t.Parallel()
	ce := featureExtColumnEncryption{}
	assert.Equal(t, byte(4), ce.featureID(), "featureExtColumnEncryption.featureID()")
}

func TestFedAuthFeatureExtID(t *testing.T) {
	t.Parallel()

	fa := featureExtFedAuth{}
	assert.Equal(t, byte(2), fa.featureID(), "featureExtFedAuth.featureID()")
}

func TestColumnStructOriginalTypeInfo(t *testing.T) {
	t.Parallel()

	// Non-encrypted column
	col := columnStruct{
		Flags: 0,
		ti: typeInfo{
			TypeId: typeInt4,
		},
	}
	result := col.originalTypeInfo()
	assert.Equal(t, uint8(typeInt4), result.TypeId, "originalTypeInfo().TypeId")

	// Encrypted column
	cryptoTi := typeInfo{TypeId: typeBigVarChar}
	col2 := columnStruct{
		Flags: colFlagEncrypted,
		ti: typeInfo{
			TypeId: typeInt4,
		},
		cryptoMeta: &cryptoMetadata{
			typeInfo: cryptoTi,
		},
	}
	result2 := col2.originalTypeInfo()
	assert.Equal(t, uint8(typeBigVarChar), result2.TypeId, "originalTypeInfo().TypeId for encrypted")
}

func TestBrowserDataType(t *testing.T) {
	t.Parallel()

	data := msdsn.BrowserData{}
	data["INSTANCE1"] = map[string]string{
		"tcp": "1433",
		"np":  `\\.\pipe\sql\query`,
	}

	assert.Len(t, data, 1, "BrowserData length")
	assert.Equal(t, "1433", data["INSTANCE1"]["tcp"], "BrowserData[INSTANCE1][tcp]")
}
