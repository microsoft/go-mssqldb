package sharedmemory

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/microsoft/go-mssqldb/msdsn"
	"gopkg.in/natefinch/npipe.v2"
)

func (n sharedMemoryDialer) ParseServer(server string, p *msdsn.Config) error {
	if p.Port > 0 {
		return fmt.Errorf("Shared memory disallowed due to port being specified")
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
	hostName, err := os.Hostname()
	if err != nil {
		// Don't know when HostName would return an error, but if it does only support shared memory for localhost or .
		hostName = "localhost"
	}
	ip := net.ParseIP(p.Host)

	if (ip != nil && !ip.IsLoopback()) || (ip == nil && (!strings.EqualFold(p.Host, hostName) && !strings.EqualFold("localhost", p.Host))) {
		return fmt.Errorf("Cannot open a Shared Memory connection to a remote SQL server")
	}
	return nil
}

func (n sharedMemoryDialer) Protocol() string {
	return "lpc"
}

func (n sharedMemoryDialer) ParseBrowserData(data msdsn.BrowserData, p *msdsn.Config) error {
	return nil
}

func (n sharedMemoryDialer) DialConnection(ctx context.Context, p *msdsn.Config) (conn net.Conn, err error) {
	pipename := `\\.\pipe\SQLLocal\`
	if p.Instance != "" {
		pipename = pipename + p.Instance
	} else {
		pipename = pipename + "MSSQLSERVER"
	}
	dl, ok := ctx.Deadline()
	if ok {
		duration := time.Until(dl)
		conn, err = npipe.DialTimeout(pipename, duration)
	} else {
		conn, err = npipe.Dial(pipename)
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

func (n sharedMemoryDialer) CallBrowser(p *msdsn.Config) bool {
	return false
}
