package mssql

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/microsoft/go-mssqldb/msdsn"
)

func selfSignedTLSCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// TestGetTLSConnEnforcesConnTimeout verifies that the strict-mode TLS
// connection keeps the timeoutConn read/write deadlines, so a server that
// completes the TLS handshake but never answers prelogin cannot hang the client.
func TestGetTLSConnEnforcesConnTimeout(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	serverCfg := &tls.Config{
		Certificates: []tls.Certificate{selfSignedTLSCert(t)},
		NextProtos:   []string{"tds/8.0"},
	}
	go func() {
		srv := tls.Server(serverConn, serverCfg)
		if err := srv.Handshake(); err != nil {
			return
		}
		// Never respond; just consume whatever the client sends.
		buf := make([]byte, 1024)
		for {
			if _, err := srv.Read(buf); err != nil {
				return
			}
		}
	}()

	toconn := newTimeoutConn(clientConn, 200*time.Millisecond)
	p := msdsn.Config{
		Host:      "localhost",
		TLSConfig: &tls.Config{InsecureSkipVerify: true},
	}
	tlsConn, err := getTLSConn(toconn, p, "tds/8.0")
	if err != nil {
		t.Fatalf("getTLSConn failed: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := tlsConn.Read(make([]byte, 1))
		done <- err
	}()

	select {
	case err := <-done:
		var netErr net.Error
		if !errors.As(err, &netErr) || !netErr.Timeout() {
			t.Fatalf("expected timeout error, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("read on strict TLS connection did not honor ConnTimeout")
	}
}
