package mssql

import (
	"testing"
)

// Benchmarks for TDS string encoding/decoding and login packet construction.

func BenchmarkStr2ucs2_Short(b *testing.B) {
	s := "master"
	b.SetBytes(int64(len(s)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sideeffect = str2ucs2(s)
	}
}

func BenchmarkStr2ucs2_Medium(b *testing.B) {
	s := "server.database.windows.net"
	b.SetBytes(int64(len(s)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sideeffect = str2ucs2(s)
	}
}

func BenchmarkStr2ucs2_Long(b *testing.B) {
	s := "This is a longer string that might appear in connection strings or query text for benchmark purposes"
	b.SetBytes(int64(len(s)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sideeffect = str2ucs2(s)
	}
}

func BenchmarkUcs22str_ASCII(b *testing.B) {
	// Pure ASCII — exercises the fast path
	input := str2ucs2("SELECT * FROM dbo.Users WHERE id = 1")
	b.SetBytes(int64(len(input)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sideeffect, _ = ucs22str(input)
	}
}

func BenchmarkUcs22str_Unicode(b *testing.B) {
	// Contains non-ASCII characters — exercises the slow path
	input := str2ucs2("Ñoño données über Straße")
	b.SetBytes(int64(len(input)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sideeffect, _ = ucs22str(input)
	}
}

func BenchmarkUcs22str_LongASCII(b *testing.B) {
	input := str2ucs2("SELECT col1, col2, col3, col4, col5 FROM schema.very_long_table_name WHERE condition1 = 1 AND condition2 = 2 ORDER BY col1 DESC")
	b.SetBytes(int64(len(input)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sideeffect, _ = ucs22str(input)
	}
}

func BenchmarkManglePassword_Short(b *testing.B) {
	pw := "P@ssw0rd"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sideeffect = manglePassword(pw)
	}
}

func BenchmarkManglePassword_Long(b *testing.B) {
	pw := "ThisIsAVeryLongAndComplexP@ssw0rd!2024#Secure"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sideeffect = manglePassword(pw)
	}
}

func BenchmarkSendLogin(b *testing.B) {
	buf := newTdsBuffer(4096, discardTransport{})
	l := &login{
		TDSVersion:    verTDS74,
		PacketSize:    4096,
		ClientProgVer: 0x07000000,
		ClientPID:     1234,
		HostName:      "WORKSTATION",
		UserName:      "testuser",
		Password:      "P@ssw0rd!",
		AppName:       "go-mssqldb-benchmark",
		ServerName:    "localhost",
		CtlIntName:    "go-mssqldb",
		Language:      "",
		Database:      "master",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sendLogin(buf, l)
	}
}

func BenchmarkWritePrelogin(b *testing.B) {
	buf := newTdsBuffer(4096, discardTransport{})
	fields := map[uint8][]byte{
		preloginVERSION:    {0x10, 0x00, 0x00, 0x00, 0x00, 0x00},
		preloginENCRYPTION: {encryptOn},
		preloginINSTOPT:    {0x00},
		preloginTHREADID:   {0x00, 0x00, 0x00, 0x00},
		preloginMARS:       {0x00},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		writePrelogin(packPrelogin, buf, fields)
	}
}
