package namedpipe

import (
	"github.com/microsoft/go-mssqldb/msdsn"
)

type namedPipeData struct {
	PipeName string
}

type namedPipeDialer struct{}

var dialer namedPipeDialer = namedPipeDialer{}

func init() {
	msdsn.ProtocolParsers = append(msdsn.ProtocolParsers, dialer)
	msdsn.ProtocolDialers["np"] = dialer
}
