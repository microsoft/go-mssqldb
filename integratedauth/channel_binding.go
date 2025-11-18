package integratedauth

import (
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/binary"
	"hash"
)

// gss_channel_bindings_struct: https://docs.oracle.com/cd/E19683-01/816-1331/overview-52/index.html
// gss_buffer_desc: https://docs.oracle.com/cd/E19683-01/816-1331/reference-21/index.html
type ChannelBindings struct {
	InitiatorAddrType uint32
	InitiatorAddress  []byte
	AcceptorAddrType  uint32
	AcceptorAddress   []byte
	ApplicationData   []byte
}

// SEC_CHANNEL_BINDINGS: https://learn.microsoft.com/en-us/windows/win32/api/sspi/ns-sspi-sec_channel_bindings
type SEC_CHANNEL_BINDINGS struct {
	DwInitiatorAddrType     uint32
	CbInitiatorLength       uint32
	DwInitiatorOffset       uint32
	DwAcceptorAddrType      uint32
	CbAcceptorLength        uint32
	DwAcceptorOffset        uint32
	CbApplicationDataLength uint32
	DwApplicationDataOffset uint32
	Data                    []byte
}

// ToBytes converts a ChannelBindings struct to a byte slice as it would be gss_channel_bindings_struct structure in GSSAPI.
// Returns:
// - a byte slice
func (cb *ChannelBindings) ToBytes() []byte {
	binarylength := 4 + 4 + 4 + 4 + 4 + uint32(len(cb.InitiatorAddress)+len(cb.AcceptorAddress)+len(cb.ApplicationData))
	i := 0
	bytes := make([]byte, binarylength)
	binary.LittleEndian.PutUint32(bytes[i:i+4], cb.InitiatorAddrType)
	i += 4
	binary.LittleEndian.PutUint32(bytes[i:i+4], uint32(len(cb.InitiatorAddress)))
	i += 4
	if len(cb.InitiatorAddress) > 0 {
		copy(bytes[i:i+len(cb.InitiatorAddress)], cb.InitiatorAddress)
		i += len(cb.InitiatorAddress)
	}
	binary.LittleEndian.PutUint32(bytes[i:i+4], cb.AcceptorAddrType)
	i += 4
	binary.LittleEndian.PutUint32(bytes[i:i+4], uint32(len(cb.AcceptorAddress)))
	i += 4
	if len(cb.AcceptorAddress) > 0 {
		copy(bytes[i:i+len(cb.AcceptorAddress)], cb.AcceptorAddress)
		i += len(cb.AcceptorAddress)
	}
	binary.LittleEndian.PutUint32(bytes[i:i+4], uint32(len(cb.ApplicationData)))
	i += 4
	if len(cb.ApplicationData) > 0 {
		copy(bytes[i:i+len(cb.ApplicationData)], cb.ApplicationData)
		i += len(cb.ApplicationData)
	}
	// Print bytes in hexdump -C style for debugging
	return bytes
}

// Md5Hash calculates the MD5 hash of the ChannelBindings struct
// Returns:
// - a byte slice
func (cb *ChannelBindings) Md5Hash() []byte {
	hash := md5.New()
	hash.Write(cb.ToBytes())
	return hash.Sum(nil)
}

// AsSSPI_SEC_CHANNEL_BINDINGS converts a ChannelBindings struct to a SEC_CHANNEL_BINDINGS struct
// Returns:
// - a SEC_CHANNEL_BINDINGS struct
func (cb *ChannelBindings) AsSSPI_SEC_CHANNEL_BINDINGS() *SEC_CHANNEL_BINDINGS {
	initiatorOffset := uint32(32)
	acceptorOffset := initiatorOffset + uint32(len(cb.InitiatorAddress))
	applicationDataOffset := acceptorOffset + uint32(len(cb.AcceptorAddress))
	c := &SEC_CHANNEL_BINDINGS{
		DwInitiatorAddrType:     cb.InitiatorAddrType,
		CbInitiatorLength:       uint32(len(cb.InitiatorAddress)),
		DwInitiatorOffset:       initiatorOffset,
		DwAcceptorAddrType:      cb.AcceptorAddrType,
		CbAcceptorLength:        uint32(len(cb.AcceptorAddress)),
		DwAcceptorOffset:        acceptorOffset,
		CbApplicationDataLength: uint32(len(cb.ApplicationData)),
		DwApplicationDataOffset: applicationDataOffset,
	}
	data := make([]byte, c.CbInitiatorLength+c.CbAcceptorLength+c.CbApplicationDataLength)
	var i uint32 = 0
	if c.CbInitiatorLength > 0 {
		copy(data[i:i+c.CbInitiatorLength], cb.InitiatorAddress)
		i += c.CbInitiatorLength
	}
	if c.CbAcceptorLength > 0 {
		copy(data[i:i+c.CbAcceptorLength], cb.AcceptorAddress)
		i += c.CbAcceptorLength
	}
	if c.CbApplicationDataLength > 0 {
		copy(data[i:i+c.CbApplicationDataLength], cb.ApplicationData)
		i += c.CbApplicationDataLength
	}
	c.Data = data
	return c
}

// ToBytes converts a SEC_CHANNEL_BINDINGS struct to a byte slice, that can be use in SSPI InitializeSecurityContext function.
// Returns:
// - a byte slice
func (cb *SEC_CHANNEL_BINDINGS) ToBytes() []byte {
	bytes := make([]byte, 32+len(cb.Data))
	binary.LittleEndian.PutUint32(bytes[0:4], cb.DwInitiatorAddrType)
	binary.LittleEndian.PutUint32(bytes[4:8], cb.CbInitiatorLength)
	binary.LittleEndian.PutUint32(bytes[8:12], cb.DwInitiatorOffset)
	binary.LittleEndian.PutUint32(bytes[12:16], cb.DwAcceptorAddrType)
	binary.LittleEndian.PutUint32(bytes[16:20], cb.CbAcceptorLength)
	binary.LittleEndian.PutUint32(bytes[20:24], cb.DwAcceptorOffset)
	binary.LittleEndian.PutUint32(bytes[24:28], cb.CbApplicationDataLength)
	binary.LittleEndian.PutUint32(bytes[28:32], cb.DwApplicationDataOffset)
	copy(bytes[32:32+len(cb.Data)], cb.Data)

	return bytes
}

// GenerateCBTFromTLSUnique generates a ChannelBindings struct from a TLS unique value
// Adds tls-unique: prefix to the TLS unique value.
// Parameters:
// - tlsUnique: the TLS unique value
// Returns:
// - a ChannelBindings struct
func GenerateCBTFromTLSUnique(tlsUnique []byte) *ChannelBindings {
	return &ChannelBindings{
		InitiatorAddrType: 0,
		InitiatorAddress:  nil,
		AcceptorAddrType:  0,
		AcceptorAddress:   nil,
		ApplicationData:   append([]byte("tls-unique:"), tlsUnique...),
	}
}

// GenerateCBTFromServerCert generates a ChannelBindings struct from a server certificate
// Calculates the hash of the server certificate as described in 4.2 section of RFC5056.
// Parameters:
// - cert: the server certificate
// Returns:
// - a ChannelBindings struct
func GenerateCBTFromServerCert(cert *x509.Certificate) *ChannelBindings {
	var certHash []byte
	var h hash.Hash
	switch cert.SignatureAlgorithm {
	case x509.SHA256WithRSA, x509.ECDSAWithSHA256, x509.SHA256WithRSAPSS:
		h = sha256.New()
	case x509.SHA384WithRSA, x509.ECDSAWithSHA384, x509.SHA384WithRSAPSS:
		h = sha512.New384()
	case x509.SHA512WithRSA, x509.ECDSAWithSHA512, x509.SHA512WithRSAPSS:
		h = sha512.New()
	default:
		h = sha256.New()
	}
	h.Write(cert.Raw)
	certHash = h.Sum(nil)
	return &ChannelBindings{
		InitiatorAddrType: 0,
		InitiatorAddress:  nil,
		AcceptorAddrType:  0,
		AcceptorAddress:   nil,
		ApplicationData:   append([]byte("tls-server-end-point:"), certHash...),
	}
}
