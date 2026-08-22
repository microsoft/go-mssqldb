package mssql

import (
	"io"
	"testing"
)

// Benchmarks for TDS buffer operations — the core I/O layer for all packet framing.

// discardTransport implements io.ReadWriteCloser, discarding writes and providing zeros on read.
type discardTransport struct{}

func (discardTransport) Read(p []byte) (int, error)  { return len(p), nil }
func (discardTransport) Write(p []byte) (int, error) { return len(p), nil }
func (discardTransport) Close() error                { return nil }

func BenchmarkTdsBuffer_Write_Small(b *testing.B) {
	buf := newTdsBuffer(4096, discardTransport{})
	payload := make([]byte, 64)
	for i := range payload {
		payload[i] = byte(i)
	}

	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.BeginPacket(packSQLBatch, false)
		buf.Write(payload)
		buf.FinishPacket()
	}
}

func BenchmarkTdsBuffer_Write_Medium(b *testing.B) {
	buf := newTdsBuffer(4096, discardTransport{})
	payload := make([]byte, 1024)
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.BeginPacket(packSQLBatch, false)
		buf.Write(payload)
		buf.FinishPacket()
	}
}

func BenchmarkTdsBuffer_Write_Large(b *testing.B) {
	// Payload larger than packet size forces multiple flushes
	buf := newTdsBuffer(4096, discardTransport{})
	payload := make([]byte, 8192)
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.BeginPacket(packSQLBatch, false)
		buf.Write(payload)
		buf.FinishPacket()
	}
}

func BenchmarkTdsBuffer_WriteByte(b *testing.B) {
	buf := newTdsBuffer(4096, discardTransport{})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.BeginPacket(packSQLBatch, false)
		for j := 0; j < 100; j++ {
			buf.WriteByte(byte(j))
		}
		buf.FinishPacket()
	}
}

func BenchmarkTdsBuffer_Read_Small(b *testing.B) {
	// Simulate reading a small packet from transport
	packetSize := uint16(512)
	buf := newTdsBuffer(packetSize, nil)

	// Pre-fill read buffer with a valid packet
	data := make([]byte, 64)
	for i := range data {
		data[i] = byte(i)
	}
	totalSize := 8 + len(data) // header + payload
	copy(buf.rbuf[8:], data)
	buf.rpos = 8
	buf.rsize = totalSize
	buf.final = true

	dest := make([]byte, 64)
	b.SetBytes(int64(len(dest)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.rpos = 8
		io.ReadFull(buf, dest)
	}
}

func BenchmarkTdsBuffer_ReadByte(b *testing.B) {
	buf := newTdsBuffer(4096, nil)
	// Fill buffer with data
	for i := 0; i < 1000; i++ {
		buf.rbuf[i] = byte(i % 256)
	}
	buf.rpos = 0
	buf.rsize = 1000
	buf.final = true

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.rpos = 0
		for j := 0; j < 100; j++ {
			buf.ReadByte()
		}
	}
}

func BenchmarkTdsBuffer_Uint16(b *testing.B) {
	buf := newTdsBuffer(4096, nil)
	// Fill with uint16 values
	for i := 0; i < 200; i += 2 {
		buf.rbuf[i] = byte(i)
		buf.rbuf[i+1] = byte(i >> 8)
	}
	buf.rpos = 0
	buf.rsize = 200
	buf.final = true

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.rpos = 0
		for j := 0; j < 50; j++ {
			buf.uint16()
		}
	}
}

func BenchmarkTdsBuffer_Uint32(b *testing.B) {
	buf := newTdsBuffer(4096, nil)
	for i := 0; i < 400; i += 4 {
		buf.rbuf[i] = byte(i)
		buf.rbuf[i+1] = byte(i >> 8)
		buf.rbuf[i+2] = byte(i >> 16)
		buf.rbuf[i+3] = byte(i >> 24)
	}
	buf.rpos = 0
	buf.rsize = 400
	buf.final = true

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.rpos = 0
		for j := 0; j < 50; j++ {
			buf.uint32()
		}
	}
}

func BenchmarkTdsBuffer_Uint64(b *testing.B) {
	buf := newTdsBuffer(4096, nil)
	for i := 0; i < 400; i++ {
		buf.rbuf[i] = byte(i % 256)
	}
	buf.rpos = 0
	buf.rsize = 400
	buf.final = true

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.rpos = 0
		for j := 0; j < 50; j++ {
			buf.uint64()
		}
	}
}

func BenchmarkTdsBuffer_BeginFinishPacket(b *testing.B) {
	buf := newTdsBuffer(4096, discardTransport{})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.BeginPacket(packSQLBatch, false)
		buf.FinishPacket()
	}
}
