package sharedmemory

import (
	"runtime"

	"github.com/moizm89/go-mssqldb/msdsn"
)

type sharedMemoryDialer struct{}

var dialer sharedMemoryDialer = sharedMemoryDialer{}

func init() {
	if runtime.GOOS == "windows" {
		msdsn.ProtocolParsers = append(msdsn.ProtocolParsers, dialer)
		msdsn.ProtocolDialers["lpc"] = dialer
	}
}
