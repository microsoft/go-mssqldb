package integratedauth

import (
	"crypto/md5"
	"encoding/binary"
)

func GenerateCBTFromTLSUnique(tlsUnique []byte) []byte {
	// Initialize the channel binding structure with empty addresses
	// These fields are defined in the RFC but not used for TLS bindings
	initiatorAddress := make([]byte, 8)
	acceptorAddress := make([]byte, 8)

	// Create the application data with the "tls-unique:" prefix
	applicationDataRaw := append([]byte("tls-unique:"), tlsUnique...)

	// Add the length prefix to the application data (little-endian 32-bit integer)
	lenApplicationData := make([]byte, 4)
	binary.LittleEndian.PutUint32(lenApplicationData, uint32(len(applicationDataRaw)))
	applicationData := append(lenApplicationData, applicationDataRaw...)

	// Assemble the complete channel binding structure
	channelBindingStruct := append(append(initiatorAddress, acceptorAddress...), applicationData...)

	// Return the MD5 hash of the structure
	hash := md5.Sum(channelBindingStruct)
	return hash[:]
}
