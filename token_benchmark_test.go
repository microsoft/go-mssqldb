package mssql

import (
	"encoding/binary"
	"testing"
)

// Benchmarks for token parsing functions — these decode every response token from the wire.

func makeTdsBufferFromBytes(data []byte) *tdsBuffer {
	buf := newTdsBuffer(uint16(len(data)+8), nil)
	copy(buf.rbuf[0:], data)
	buf.rpos = 0
	buf.rsize = len(data)
	buf.final = true
	return buf
}

func BenchmarkParseDone(b *testing.B) {
	// doneStruct: Status(2) + CurCmd(2) + RowCount(8) = 12 bytes
	data := make([]byte, 12)
	binary.LittleEndian.PutUint16(data[0:], doneCount) // Status: has row count
	binary.LittleEndian.PutUint16(data[2:], cmdSelect) // CurCmd
	binary.LittleEndian.PutUint64(data[4:], 42)        // RowCount

	buf := makeTdsBufferFromBytes(data)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.rpos = 0
		parseDone(buf)
	}
}

func BenchmarkParseDoneInProc(b *testing.B) {
	data := make([]byte, 12)
	binary.LittleEndian.PutUint16(data[0:], doneMore|doneCount)
	binary.LittleEndian.PutUint16(data[2:], cmdSelect)
	binary.LittleEndian.PutUint64(data[4:], 100)

	buf := makeTdsBufferFromBytes(data)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.rpos = 0
		parseDoneInProc(buf)
	}
}

func BenchmarkParseReturnStatus(b *testing.B) {
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data[0:], 0) // return status 0

	buf := makeTdsBufferFromBytes(data)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.rpos = 0
		parseReturnStatus(buf)
	}
}

func BenchmarkParseOrder(b *testing.B) {
	// 4 columns in ORDER BY
	numCols := 4
	data := make([]byte, 2+numCols*2)
	binary.LittleEndian.PutUint16(data[0:], uint16(numCols*2))
	for i := 0; i < numCols; i++ {
		binary.LittleEndian.PutUint16(data[2+i*2:], uint16(i+1))
	}

	buf := makeTdsBufferFromBytes(data)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.rpos = 0
		parseOrder(buf)
	}
}

func BenchmarkParseError72(b *testing.B) {
	// Build a realistic error token payload:
	// Length(2) + Number(4) + State(1) + Class(1) + Message(UsVarChar) + ServerName(BVarChar) + ProcName(BVarChar) + LineNo(4)
	msg := str2ucs2("Login failed for user 'sa'.")
	server := str2ucs2("MYSERVER")
	proc := str2ucs2("")

	// UsVarChar: uint16 length (in chars) + UTF-16 data
	// BVarChar: byte length (in chars) + UTF-16 data
	msgChars := len(msg) / 2
	serverChars := len(server) / 2
	procChars := len(proc) / 2

	payloadSize := 4 + 1 + 1 + 2 + len(msg) + 1 + len(server) + 1 + len(proc) + 4
	data := make([]byte, 2+payloadSize)
	binary.LittleEndian.PutUint16(data[0:], uint16(payloadSize))
	off := 2
	binary.LittleEndian.PutUint32(data[off:], 18456) // Error number
	off += 4
	data[off] = 1 // State
	off++
	data[off] = 14 // Class (severity)
	off++
	// Message (UsVarChar)
	binary.LittleEndian.PutUint16(data[off:], uint16(msgChars))
	off += 2
	copy(data[off:], msg)
	off += len(msg)
	// ServerName (BVarChar)
	data[off] = byte(serverChars)
	off++
	copy(data[off:], server)
	off += len(server)
	// ProcName (BVarChar)
	data[off] = byte(procChars)
	off++
	copy(data[off:], proc)
	off += len(proc)
	// LineNo
	binary.LittleEndian.PutUint32(data[off:], 1)

	buf := makeTdsBufferFromBytes(data)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.rpos = 0
		parseError72(buf)
	}
}

func BenchmarkParseInfo(b *testing.B) {
	// Same structure as parseError72 but with informational message
	msg := str2ucs2("Changed database context to 'master'.")
	server := str2ucs2("MYSERVER")
	proc := str2ucs2("")

	msgChars := len(msg) / 2
	serverChars := len(server) / 2
	procChars := len(proc) / 2

	payloadSize := 4 + 1 + 1 + 2 + len(msg) + 1 + len(server) + 1 + len(proc) + 4
	data := make([]byte, 2+payloadSize)
	binary.LittleEndian.PutUint16(data[0:], uint16(payloadSize))
	off := 2
	binary.LittleEndian.PutUint32(data[off:], 5701) // Info number
	off += 4
	data[off] = 2 // State
	off++
	data[off] = 0 // Class
	off++
	binary.LittleEndian.PutUint16(data[off:], uint16(msgChars))
	off += 2
	copy(data[off:], msg)
	off += len(msg)
	data[off] = byte(serverChars)
	off++
	copy(data[off:], server)
	off += len(server)
	data[off] = byte(procChars)
	off++
	copy(data[off:], proc)
	off += len(proc)
	binary.LittleEndian.PutUint32(data[off:], 0)

	buf := makeTdsBufferFromBytes(data)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.rpos = 0
		parseInfo(buf)
	}
}

func BenchmarkParseLoginAck(b *testing.B) {
	// loginAckStruct: size(2) + Interface(1) + TDSVersion(4) + ProgNameLen(1) + ProgName(UTF-16) + ProgVer(4)
	progName := str2ucs2("Microsoft SQL Server")
	progNameChars := len(progName) / 2
	payloadSize := 1 + 4 + 1 + len(progName) + 4
	data := make([]byte, 2+payloadSize)
	binary.LittleEndian.PutUint16(data[0:], uint16(payloadSize))
	off := 2
	data[off] = 1 // Interface: SQL_DFLT
	off++
	binary.BigEndian.PutUint32(data[off:], 0x74000004) // TDS 7.4
	off += 4
	data[off] = byte(progNameChars)
	off++
	copy(data[off:], progName)
	off += len(progName)
	binary.BigEndian.PutUint32(data[off:], 0x10000000) // ProgVer 16.0.0.0

	buf := makeTdsBufferFromBytes(data)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.rpos = 0
		parseLoginAck(buf)
	}
}

func BenchmarkParseFeatureExtAck_Empty(b *testing.B) {
	// Just a terminator byte
	data := []byte{featExtTERMINATOR}

	buf := makeTdsBufferFromBytes(data)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.rpos = 0
		parseFeatureExtAck(buf)
	}
}

func BenchmarkParseTabName(b *testing.B) {
	// Simulate a TABNAME token with table name data
	tableName := str2ucs2("dbo.Users")
	data := make([]byte, 2+len(tableName))
	binary.LittleEndian.PutUint16(data[0:], uint16(len(tableName)))
	copy(data[2:], tableName)

	buf := makeTdsBufferFromBytes(data)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.rpos = 0
		parseTabName(buf)
	}
}

func BenchmarkParseColInfo(b *testing.B) {
	// Simulate COLINFO with some column info bytes
	colInfoData := make([]byte, 20) // arbitrary column info
	data := make([]byte, 2+len(colInfoData))
	binary.LittleEndian.PutUint16(data[0:], uint16(len(colInfoData)))
	copy(data[2:], colInfoData)

	buf := makeTdsBufferFromBytes(data)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.rpos = 0
		parseColInfo(buf)
	}
}
