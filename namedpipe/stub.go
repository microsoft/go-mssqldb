//go:build !windows
package namedpipe

func (n namedPipeDialer) ParseServer(server string, p *msdsn.Config) error {
	return fmt.Errorf("Named pipe connections are not supported on this operating system")
}

func (n namedPipeDialer) Protocol() string {
	return "np"
}

func (n namedPipeDialer) ParseBrowserData(data msdsn.BrowserData, p *msdsn.Config) error {
	return fmt.Errorf("Named pipe connections are not supported on this operating system")
}

func (n namedPipeDialer) DialConnection(ctx context.Context, p msdsn.Config) (conn net.Conn, err error) {
	
	return nil, return fmt.Errorf("Named pipe connections are not supported on this operating system")
}

func (n namedPipeDialer) CallBrowser(p *msdsn.Config) bool {
	return false
}
