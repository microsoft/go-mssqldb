package namedpipe

import (
	"context"
	"fmt"
	"net"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/microsoft/go-mssqldb/msdsn"
	"gopkg.in/natefinch/npipe.v2"
)

type namedPipeData struct {
	PipeName string
}

func (n namedPipeDialer) ParseServer(server string, p *msdsn.Config) error {
	// assume a server name starting with \\ is the full named pipe path
	if strings.HasPrefix(server, `\\`) {
		p.ProtocolParameters[n.Protocol()] = &namedPipeData{PipeName: server}
	} else if p.Port > 0 {
		return fmt.Errorf("Named pipes disallowed due to port being specified")
	} else if p.Host == "" { // if the string specifies np:host\instance, tcpParser won't have filled in p.Host
		parts := strings.SplitN(server, `\`, 2)
		p.Host = parts[0]
		if p.Host == "." || strings.ToUpper(p.Host) == "(LOCAL)" {
			p.Host = "localhost"
		}
		if len(parts) > 1 {
			p.Instance = parts[1]
		}
	}
	return nil
}

func (n namedPipeDialer) Protocol() string {
	return "np"
}

func (n namedPipeDialer) ParseBrowserData(data msdsn.BrowserData, p *msdsn.Config) error {
	// If instance is specified, but no port, check SQL Server Browser
	// for the instance and discover its port.
	p.Instance = strings.ToUpper(p.Instance)
	instance := p.Instance
	if instance == "" {
		instance = "MSSQLSERVER"
	}
	pipename, ok := data[instance]["np"]
	if !ok {
		f := "no named pipe instance matching '%v' returned from host '%v'"
		return fmt.Errorf(f, p.Instance, p.Host)
	}
	p.ProtocolParameters[n.Protocol()] = namedPipeData{PipeName: pipename}
	return nil
}

func (n namedPipeDialer) DialConnection(ctx context.Context, p *msdsn.Config) (conn net.Conn, err error) {
	data := p.ProtocolParameters[n.Protocol()]
	switch d := data.(type) {
	case namedPipeData:
		dl, ok := ctx.Deadline()
		if ok {
			duration := time.Until(dl)
			conn, err = npipe.DialTimeout(d.PipeName, duration)
		} else {
			conn, err = npipe.Dial(d.PipeName)
		}
		if err == nil && p.ServerSPN == "" {
			host := p.Host
			instance := ""
			if p.Instance != "" {
				instance = fmt.Sprintf(":%s", p.Instance)
			}
			ip := net.ParseIP(host)
			if ip != nil && ip.IsLoopback() {
				host, _ = os.Hostname()
			}
			p.ServerSPN = fmt.Sprintf("MSSQLSvc/%s%s", host, instance)
		}
		return
	}
	return nil, fmt.Errorf("Unexpected protocol data specified for connection: %v", reflect.TypeOf(data))
}

func (n namedPipeDialer) CallBrowser(p *msdsn.Config) bool {
	_, ok := p.ProtocolParameters[n.Protocol()]
	return !ok
}
