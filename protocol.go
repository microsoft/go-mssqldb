package mssql

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/microsoft/go-mssqldb/msdsn"
)

// ProtocolDialer makes the network connection for a protocol
type ProtocolDialer interface {
	// Translates data from SQL Browser to parameters in the config
	ParseBrowserData(data BrowserData, p *msdsn.Config) error
	// Returns a Dialer to make the connection
	DialConnection(ctx context.Context, c *Connector, p msdsn.Config) (conn net.Conn, err error)
	// Returns true if information is needed from the SQL Browser service to make a connection
	CallBrowser(p *msdsn.Config) bool
}

// ProtocolResolvers should be updated by any package that provides a ProtocolResolver and an associated ProtocolDialer
var ProtocolDialers map[string]ProtocolDialer = map[string]ProtocolDialer{
	"tcp": tcpDialer{},
}

type tcpDialer struct{}

func (t tcpDialer) ParseBrowserData(data BrowserData, p *msdsn.Config) error {
	// If instance is specified, but no port, check SQL Server Browser
	// for the instance and discover its port.
	p.Instance = strings.ToUpper(p.Instance)
	strport, ok := data[p.Instance]["tcp"]
	if !ok {
		f := "no instance matching '%v' returned from host '%v'"
		return fmt.Errorf(f, p.Instance, p.Host)
	}
	port, err := strconv.ParseUint(strport, 0, 16)
	if err != nil {
		f := "invalid tcp port returned from Sql Server Browser '%v': %v"
		return fmt.Errorf(f, strport, err.Error())
	}
	p.Port = port
	return nil
}

// SQL Server AlwaysOn Availability Group Listeners are bound by DNS to a
// list of IP addresses.  So if there is more than one, try them all and
// use the first one that allows a connection.
func (t tcpDialer) DialConnection(ctx context.Context, c *Connector, p msdsn.Config) (conn net.Conn, err error) {
	var ips []net.IP
	ip := net.ParseIP(p.Host)
	if ip == nil {
		ips, err = net.LookupIP(p.Host)
		if err != nil {
			return
		}
	} else {
		ips = []net.IP{ip}
	}
	if len(ips) == 1 {
		d := c.getDialer(&p)
		addr := net.JoinHostPort(ips[0].String(), strconv.Itoa(int(resolveServerPort(p.Port))))
		conn, err = d.DialContext(ctx, "tcp", addr)

	} else {
		//Try Dials in parallel to avoid waiting for timeouts.
		connChan := make(chan net.Conn, len(ips))
		errChan := make(chan error, len(ips))
		portStr := strconv.Itoa(int(resolveServerPort(p.Port)))
		for _, ip := range ips {
			go func(ip net.IP) {
				d := c.getDialer(&p)
				addr := net.JoinHostPort(ip.String(), portStr)
				conn, err := d.DialContext(ctx, "tcp", addr)
				if err == nil {
					connChan <- conn
				} else {
					errChan <- err
				}
			}(ip)
		}
		// Wait for either the *first* successful connection, or all the errors
	wait_loop:
		for i := range ips {
			select {
			case conn = <-connChan:
				// Got a connection to use, close any others
				go func(n int) {
					for i := 0; i < n; i++ {
						select {
						case conn := <-connChan:
							conn.Close()
						case <-errChan:
						}
					}
				}(len(ips) - i - 1)
				// Remove any earlier errors we may have collected
				err = nil
				break wait_loop
			case err = <-errChan:
			}
		}
	}
	// Can't do the usual err != nil check, as it is possible to have gotten an error before a successful connection
	if conn == nil {
		f := "unable to open tcp connection with host '%v:%v': %v"
		return nil, fmt.Errorf(f, p.Host, resolveServerPort(p.Port), err.Error())
	}
	return conn, err
}

func (t tcpDialer) CallBrowser(p *msdsn.Config) bool {
	return len(p.Instance) > 0 && p.Port == 0
}

func resolveServerPort(port uint64) uint64 {
	if port == 0 {
		return defaultServerPort
	}

	return port
}
