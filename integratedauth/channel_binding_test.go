package integratedauth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGenerateCBTFromTLSUnique(t *testing.T) {
	pairs := map[string]string{
		"0123456789abcdef": "1eb7620a5e38cb1f50478b1690621a03",
	}
	for input, expected := range pairs {
		channelBinding, err := GenerateCBTFromTLSUnique([]byte(input))
		assert.NoError(t, err, "Expected no error for input %s", input)
		actual := channelBinding.Md5Hash()
		assert.Equal(t, expected, hex.EncodeToString(actual), "Md5Hash mismatch for input %s", input)
	}
}

func TestAsSSPI_SEC_CHANNEL_BINDINGS(t *testing.T) {
	pairs := map[string]string{
		"0123456789abcdef": "0000000000000000200000000000000000000000200000001b00000020000000746c732d756e697175653a30313233343536373839616263646566",
	}
	for input, expected := range pairs {
		channelBinding, err := GenerateCBTFromTLSUnique([]byte(input))
		assert.NoError(t, err, "Expected no error for input %s", input)
		winsspiCB := channelBinding.AsSSPI_SEC_CHANNEL_BINDINGS().ToBytes()
		assert.Equal(t, expected, hex.EncodeToString(winsspiCB), "SSPI bytes mismatch for input %s", input)
	}
}

func TestGenerateCBTFromTLSUniqueEmpty(t *testing.T) {
	_, err := GenerateCBTFromTLSUnique([]byte{})
	assert.Error(t, err, "Expected error for empty TLS unique value")
}

func TestGenerateCBTFromTLSExporter(t *testing.T) {
	exporterKey := []byte("test-exporter-key-32-bytes-long!")

	cb, err := GenerateCBTFromTLSExporter(exporterKey)
	assert.NoError(t, err, "Expected no error")

	assert.Equal(t, ChannelBindingsType(ChannelBindingsTypeTLSExporter), cb.Type, "Type mismatch")

	expectedPrefix := TLS_EXPORTER_PREFIX
	assert.Equal(t, expectedPrefix, string(cb.ApplicationData[:len(expectedPrefix)]), "Application data should start with prefix")

	assert.Equal(t, string(exporterKey), string(cb.ApplicationData[len(expectedPrefix):]), "Application data should contain the exporter key after prefix")
}

func TestGenerateCBTFromTLSExporterEmpty(t *testing.T) {
	_, err := GenerateCBTFromTLSExporter([]byte{})
	assert.Error(t, err, "Expected error for empty exporter key")
}

func TestGenerateCBTFromTLSConnStateTLS12(t *testing.T) {
	state := tls.ConnectionState{
		Version:   tls.VersionTLS12,
		TLSUnique: []byte("tls12-unique-value"),
	}

	cb, err := GenerateCBTFromTLSConnState(state)
	assert.NoError(t, err, "Expected no error")

	assert.Equal(t, ChannelBindingsType(ChannelBindingsTypeTLSUnique), cb.Type, "Type mismatch")
}

func TestGenerateCBTFromTLSConnStateTLS13(t *testing.T) {
	state := tls.ConnectionState{
		Version: tls.VersionTLS13,
	}

	cb, err := GenerateCBTFromTLSConnState(state)
	assert.NoError(t, err, "Expected no error")

	// TLS 1.3 returns nil (not yet supported)
	assert.Nil(t, cb, "Expected nil for TLS 1.3")
}

func TestGenerateCBTFromTLSConnStateEmptyUnique(t *testing.T) {
	state := tls.ConnectionState{
		Version:   tls.VersionTLS12,
		TLSUnique: []byte{},
	}

	_, err := GenerateCBTFromTLSConnState(state)
	assert.Error(t, err, "Expected error for empty TLS unique")
}

func TestGenerateCBTFromServerCertNil(t *testing.T) {
	cb := GenerateCBTFromServerCert(nil)
	assert.Nil(t, cb, "Expected nil for nil certificate")
}

// Helper to create a test certificate with a specific signature algorithm
func createTestCertWithAlgorithm(sigAlg x509.SignatureAlgorithm) *x509.Certificate {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "test",
		},
		NotBefore:          time.Now(),
		NotAfter:           time.Now().Add(time.Hour),
		SignatureAlgorithm: sigAlg,
	}
	certDER, _ := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	cert, _ := x509.ParseCertificate(certDER)
	return cert
}

func TestGenerateCBTFromServerCertSHA256(t *testing.T) {
	cert := createTestCertWithAlgorithm(x509.ECDSAWithSHA256)

	cb := GenerateCBTFromServerCert(cert)
	assert.NotNil(t, cb, "Expected non-nil channel binding")

	assert.Equal(t, ChannelBindingsType(ChannelBindingsTypeTLSServerEndPoint), cb.Type, "Type mismatch")

	// SHA256 produces 32 bytes, plus the prefix length
	expectedLen := len(TLS_SERVER_END_POINT_PREFIX) + 32
	assert.Len(t, cb.ApplicationData, expectedLen, "Application data length mismatch")

	assert.Equal(t, TLS_SERVER_END_POINT_PREFIX, string(cb.ApplicationData[:len(TLS_SERVER_END_POINT_PREFIX)]), "Prefix mismatch")
}

func TestGenerateCBTFromServerCertSHA384(t *testing.T) {
	cert := createTestCertWithAlgorithm(x509.ECDSAWithSHA384)

	cb := GenerateCBTFromServerCert(cert)
	assert.NotNil(t, cb, "Expected non-nil channel binding")

	// SHA384 produces 48 bytes, plus the prefix length
	expectedLen := len(TLS_SERVER_END_POINT_PREFIX) + 48
	assert.Len(t, cb.ApplicationData, expectedLen, "Application data length mismatch")
}

func TestGenerateCBTFromServerCertSHA512(t *testing.T) {
	cert := createTestCertWithAlgorithm(x509.ECDSAWithSHA512)

	cb := GenerateCBTFromServerCert(cert)
	assert.NotNil(t, cb, "Expected non-nil channel binding")

	// SHA512 produces 64 bytes, plus the prefix length
	expectedLen := len(TLS_SERVER_END_POINT_PREFIX) + 64
	assert.Len(t, cb.ApplicationData, expectedLen, "Application data length mismatch")
}

func TestGenerateCBTFromServerCertDefaultHash(t *testing.T) {
	// Create a certificate with an algorithm that falls through to the default case
	// MD5WithRSA is an older algorithm that should trigger the default (SHA256) hash
	cert := &x509.Certificate{
		Raw:                []byte("test certificate raw bytes for hashing"),
		SignatureAlgorithm: x509.MD5WithRSA, // Falls through to default SHA256
	}

	cb := GenerateCBTFromServerCert(cert)
	assert.NotNil(t, cb, "Expected non-nil channel binding")

	// Default (SHA256) produces 32 bytes, plus the prefix length
	expectedLen := len(TLS_SERVER_END_POINT_PREFIX) + 32
	assert.Len(t, cb.ApplicationData, expectedLen, "Application data length mismatch (default SHA256)")
}

func TestEmptyChannelBindingsMd5Hash(t *testing.T) {
	hash := EmptyChannelBindings.Md5Hash()
	assert.Len(t, hash, 16, "Expected 16 byte hash")
	// Empty channel bindings should produce zeros
	for i, b := range hash {
		assert.Equal(t, byte(0), b, "Expected zero at position %d", i)
	}
}

func TestChannelBindingsToBytes(t *testing.T) {
	cb := &ChannelBindings{
		Type:              ChannelBindingsTypeTLSUnique,
		InitiatorAddrType: 1,
		InitiatorAddress:  []byte{0x01, 0x02, 0x03},
		AcceptorAddrType:  2,
		AcceptorAddress:   []byte{0x04, 0x05},
		ApplicationData:   []byte("test-data"),
	}

	bytes := cb.ToBytes()
	assert.NotEmpty(t, bytes, "Expected non-empty bytes")

	// Verify structure: 5 x uint32 headers + data lengths
	expectedLen := 4*5 + len(cb.InitiatorAddress) + len(cb.AcceptorAddress) + len(cb.ApplicationData)
	assert.Len(t, bytes, expectedLen, "Bytes length mismatch")
}

func TestChannelBindingsToBytesEmptyAddresses(t *testing.T) {
	cb := &ChannelBindings{
		Type:              ChannelBindingsTypeTLSUnique,
		InitiatorAddrType: 0,
		InitiatorAddress:  nil,
		AcceptorAddrType:  0,
		AcceptorAddress:   nil,
		ApplicationData:   []byte("app-data"),
	}

	bytes := cb.ToBytes()
	// 5 x uint32 headers + application data
	expectedLen := 4*5 + len(cb.ApplicationData)
	assert.Len(t, bytes, expectedLen, "Bytes length mismatch")
}

func TestAsSSPI_SEC_CHANNEL_BINDINGSWithAddresses(t *testing.T) {
	cb := &ChannelBindings{
		Type:              ChannelBindingsTypeTLSUnique,
		InitiatorAddrType: 1,
		InitiatorAddress:  []byte{0x01, 0x02, 0x03},
		AcceptorAddrType:  2,
		AcceptorAddress:   []byte{0x04, 0x05},
		ApplicationData:   []byte("test-data"),
	}

	secCB := cb.AsSSPI_SEC_CHANNEL_BINDINGS()

	assert.Equal(t, uint32(1), secCB.DwInitiatorAddrType, "Initiator addr type mismatch")
	assert.Equal(t, uint32(3), secCB.CbInitiatorLength, "Initiator length mismatch")
	assert.Equal(t, uint32(2), secCB.DwAcceptorAddrType, "Acceptor addr type mismatch")
	assert.Equal(t, uint32(2), secCB.CbAcceptorLength, "Acceptor length mismatch")
	assert.Equal(t, uint32(9), secCB.CbApplicationDataLength, "App data length mismatch")

	// Test ToBytes
	bytes := secCB.ToBytes()
	assert.Len(t, bytes, 32+len(secCB.Data), "Bytes length mismatch")
}

func TestSEC_CHANNEL_BINDINGSEmptyData(t *testing.T) {
	cb := &ChannelBindings{
		Type:              ChannelBindingsTypeTLSUnique,
		InitiatorAddrType: 0,
		InitiatorAddress:  nil,
		AcceptorAddrType:  0,
		AcceptorAddress:   nil,
		ApplicationData:   nil,
	}

	secCB := cb.AsSSPI_SEC_CHANNEL_BINDINGS()

	assert.Equal(t, uint32(0), secCB.CbInitiatorLength, "Initiator length should be 0")
	assert.Equal(t, uint32(0), secCB.CbAcceptorLength, "Acceptor length should be 0")
	assert.Equal(t, uint32(0), secCB.CbApplicationDataLength, "App data length should be 0")

	bytes := secCB.ToBytes()
	assert.Len(t, bytes, 32, "Bytes length should be 32")
}
