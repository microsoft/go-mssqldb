package msdsn

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math/big"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newSelfSignedCert writes a freshly generated certificate to a temporary PEM
// file and returns the path along with its DER bytes, so a test can check that
// a round-tripped config still trusts the certificate it started with.
func newSelfSignedCert(t *testing.T) (path string, der []byte) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err, "generating key")

	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "host.example.com"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err = x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	require.NoError(t, err, "creating certificate")

	file, err := os.CreateTemp("", "*.pem")
	require.NoError(t, err, "creating temporary certificate file")
	t.Cleanup(func() { _ = os.Remove(file.Name()) })
	_, err = file.Write(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	require.NoError(t, err, "writing temporary certificate file")
	require.NoError(t, file.Close(), "closing temporary certificate file")

	return file.Name(), der
}

// newSelfSignedServer is newSelfSignedCert with the private key kept, for a
// handshake against the certificate rather than a call to a callback.
func newSelfSignedServer(t *testing.T) (server tls.Certificate, path string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err, "generating key")

	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "host.example.com"},
		DNSNames:              []string{"host.example.com"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	require.NoError(t, err, "creating certificate")

	file, err := os.CreateTemp("", "*.pem")
	require.NoError(t, err, "creating temporary certificate file")
	t.Cleanup(func() { _ = os.Remove(file.Name()) })
	_, err = file.Write(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	require.NoError(t, err, "writing temporary certificate file")
	require.NoError(t, file.Close(), "closing temporary certificate file")

	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, file.Name()
}

// handshakeWith runs a TLS handshake against a server presenting the given
// certificate and reports what the client config made of it.
func handshakeWith(t *testing.T, server tls.Certificate, client *tls.Config) error {
	t.Helper()
	return handshakeBetween(t, &tls.Config{Certificates: []tls.Certificate{server}}, client)
}

// handshakeBetween is handshakeWith for a server with settings of its own.
func handshakeBetween(t *testing.T, server, client *tls.Config) error {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "listening")
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		serverConn := tls.Server(conn, server)
		_ = serverConn.Handshake()
		_ = serverConn.Close()
	}()

	raw, err := net.Dial("tcp", listener.Addr().String())
	require.NoError(t, err, "dialing")
	defer raw.Close()
	return tls.Client(raw, client).Handshake()
}

func roundTrip(t *testing.T, dsn string) (before, after Config) {
	t.Helper()
	before, err := Parse(dsn)
	require.NoError(t, err, "parsing %q", dsn)
	after, err = Parse(before.URL().String())
	require.NoError(t, err, "reparsing %q", before.URL().String())
	return before, after
}

// TestConfigURLRoundTripPreservesSettings walks the settings inventoried in
// issue #455. Each case asserts the effective configuration after a
// Parse -> URL -> Parse round trip rather than the spelling of the URL.
func TestConfigURLRoundTripPreservesSettings(t *testing.T) {
	tests := []struct {
		name  string
		dsn   string
		check func(t *testing.T, after Config)
	}{
		{
			name: "explicit encrypt=false keeps certificate verification",
			dsn:  "server=host.example.com;encrypt=false",
			check: func(t *testing.T, after Config) {
				assert.Equal(t, Encryption(EncryptionOff), after.Encryption, "Encryption")
				assert.False(t, after.TrustServerCertificate, "TrustServerCertificate")
				require.NotNil(t, after.TLSConfig, "TLSConfig")
				assert.False(t, after.TLSConfig.InsecureSkipVerify, "InsecureSkipVerify")
			},
		},
		{
			name: "explicit encrypt=optional keeps certificate verification",
			dsn:  "server=host.example.com;encrypt=optional",
			check: func(t *testing.T, after Config) {
				assert.Equal(t, Encryption(EncryptionOff), after.Encryption, "Encryption")
				assert.False(t, after.TrustServerCertificate, "TrustServerCertificate")
				require.NotNil(t, after.TLSConfig, "TLSConfig")
				assert.False(t, after.TLSConfig.InsecureSkipVerify, "InsecureSkipVerify")
			},
		},
		{
			name: "explicit encrypt=false with trustservercertificate=true",
			dsn:  "server=host.example.com;encrypt=false;trustservercertificate=true",
			check: func(t *testing.T, after Config) {
				assert.True(t, after.TrustServerCertificate, "TrustServerCertificate")
				require.NotNil(t, after.TLSConfig, "TLSConfig")
				assert.True(t, after.TLSConfig.InsecureSkipVerify, "InsecureSkipVerify")
			},
		},
		{
			name: "encrypt=disable stays disabled",
			dsn:  "server=host.example.com;encrypt=disable",
			check: func(t *testing.T, after Config) {
				assert.Equal(t, Encryption(EncryptionDisabled), after.Encryption, "Encryption")
			},
		},
		{
			name: "tlsmin",
			dsn:  "server=host.example.com;encrypt=true;tlsmin=1.3",
			check: func(t *testing.T, after Config) {
				require.NotNil(t, after.TLSConfig, "TLSConfig")
				assert.Equal(t, uint16(tls.VersionTLS13), after.TLSConfig.MinVersion, "MinVersion")
			},
		},
		{
			name: "tlsmin survives encrypt=disable, which builds no tls.Config",
			dsn:  "server=host.example.com;encrypt=disable;tlsmin=1.2",
			check: func(t *testing.T, after Config) {
				assert.Equal(t, "1.2", after.Parameters[TLSMin], "tlsmin parameter")
			},
		},
		{
			name: "hostnameincertificate survives encrypt=disable too",
			dsn:  "server=host.example.com;encrypt=disable;hostnameincertificate=other.example.com",
			check: func(t *testing.T, after Config) {
				assert.Equal(t, "other.example.com", after.Parameters[HostNameInCertificate], "hostnameincertificate parameter")
			},
		},
		{
			name: "hostnameincertificate",
			dsn:  "server=host.example.com;encrypt=true;hostnameincertificate=other.example.com",
			check: func(t *testing.T, after Config) {
				require.NotNil(t, after.TLSConfig, "TLSConfig")
				assert.Equal(t, "other.example.com", after.TLSConfig.ServerName, "ServerName")
				assert.True(t, after.HostInCertificateProvided, "HostInCertificateProvided")
			},
		},
		{
			name: "epa enabled",
			dsn:  "server=host.example.com;epa enabled=true",
			check: func(t *testing.T, after Config) {
				assert.True(t, after.EpaEnabled, "EpaEnabled")
			},
		},
		{
			name: "fedauth and its azuread settings",
			dsn:  "server=host.example.com;fedauth=ActiveDirectoryServicePrincipal;applicationclientid=client-id;user id=u;password=p",
			check: func(t *testing.T, after Config) {
				assert.Equal(t, "ActiveDirectoryServicePrincipal", after.Parameters["fedauth"], "fedauth")
				assert.Equal(t, "client-id", after.Parameters["applicationclientid"], "applicationclientid")
			},
		},
		{
			name: "authenticator and its krb5 settings",
			dsn:  "server=host.example.com;authenticator=krb5;krb5-realm=EXAMPLE.COM;krb5-configfile=/etc/krb5.conf",
			check: func(t *testing.T, after Config) {
				assert.Equal(t, "krb5", after.Parameters["authenticator"], "authenticator")
				assert.Equal(t, "EXAMPLE.COM", after.Parameters["krb5-realm"], "krb5-realm")
				assert.Equal(t, "/etc/krb5.conf", after.Parameters["krb5-configfile"], "krb5-configfile")
			},
		},
		{
			name: "applicationintent=ReadOnly",
			dsn:  "server=host.example.com;database=db;applicationintent=ReadOnly",
			check: func(t *testing.T, after Config) {
				assert.True(t, after.ReadOnlyIntent, "ReadOnlyIntent")
			},
		},
		{
			name: "multisubnetfailover=false",
			dsn:  "server=host.example.com;multisubnetfailover=false",
			check: func(t *testing.T, after Config) {
				assert.False(t, after.MultiSubnetFailover, "MultiSubnetFailover")
			},
		},
		{
			name: "multisubnetfailover=true",
			dsn:  "server=host.example.com;multisubnetfailover=true",
			check: func(t *testing.T, after Config) {
				assert.True(t, after.MultiSubnetFailover, "MultiSubnetFailover")
			},
		},
		{
			name: "notraceid=true",
			dsn:  "server=host.example.com;notraceid=true",
			check: func(t *testing.T, after Config) {
				assert.True(t, after.NoTraceID, "NoTraceID")
			},
		},
		{
			name: "packet size",
			dsn:  "server=host.example.com;packet size=8192",
			check: func(t *testing.T, after Config) {
				assert.Equal(t, uint16(8192), after.PacketSize, "PacketSize")
			},
		},
		{
			name: "connection timeout",
			dsn:  "server=host.example.com;connection timeout=7",
			check: func(t *testing.T, after Config) {
				assert.Equal(t, 7*time.Second, after.ConnTimeout, "ConnTimeout")
			},
		},
		{
			name: "keepalive",
			dsn:  "server=host.example.com;keepalive=9",
			check: func(t *testing.T, after Config) {
				assert.Equal(t, 9*time.Second, after.KeepAlive, "KeepAlive")
			},
		},
		{
			name: "app name and workstation id",
			dsn:  "server=host.example.com;app name=roundtrip-demo;workstation id=roundtrip-client",
			check: func(t *testing.T, after Config) {
				assert.Equal(t, "roundtrip-demo", after.AppName, "AppName")
				assert.Equal(t, "roundtrip-client", after.Workstation, "Workstation")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, after := roundTrip(t, tt.dsn)
			tt.check(t, after)
		})
	}
}

// TestConfigURLRoundTripPreservesCertificateTrust covers the two settings that
// name a file on disk. They are not recoverable from the built tls.Config, so
// they have to reach the URL as parameters.
func TestConfigURLRoundTripPreservesCertificateTrust(t *testing.T) {
	certFile, der := newSelfSignedCert(t)

	t.Run("certificate keeps the configured trust", func(t *testing.T) {
		before, after := roundTrip(t, "server=host.example.com;encrypt=true;certificate="+certFile)
		require.NotNil(t, before.TLSConfig, "TLSConfig before")
		require.NotNil(t, after.TLSConfig, "TLSConfig after")
		require.NotNil(t, before.TLSConfig.RootCAs, "RootCAs before")
		require.NotNil(t, after.TLSConfig.RootCAs, "RootCAs after: the configured trust was replaced by system trust")
		// A pool that failed to load the certificate is still non-nil, so the
		// assertion has to be that the pool is the same one and that it is not
		// empty.
		assert.True(t, after.TLSConfig.RootCAs.Equal(before.TLSConfig.RootCAs), "RootCAs after: a different set of roots")
		assert.False(t, after.TLSConfig.RootCAs.Equal(x509.NewCertPool()), "RootCAs after: pool is empty and trusts nothing")
	})

	t.Run("servercertificate keeps the pin", func(t *testing.T) {
		_, after := roundTrip(t, "server=host.example.com;encrypt=true;servercertificate="+certFile)
		require.NotNil(t, after.TLSConfig, "TLSConfig after")
		verify := after.TLSConfig.VerifyPeerCertificate
		require.NotNil(t, verify, "VerifyPeerCertificate after: the pin was replaced by ordinary chain validation")

		// Call the callback rather than test it for nil, so the assertion holds
		// only if the pin still points at the configured certificate.
		assert.NoError(t, verify([][]byte{der}, nil), "the pinned certificate should be accepted")
		_, otherDER := newSelfSignedCert(t)
		assert.Error(t, verify([][]byte{otherDER}, nil), "a different certificate should be rejected")
	})
}

// TestConfigURLOmitsUnsuppliedSettings pins the other half of the contract. A
// setting the connection string never mentioned must stay absent, because
// emitting a default would turn it into an explicit choice. For encrypt that
// would change the meaning of every Config that never set encryption.
func TestConfigURLOmitsUnsuppliedSettings(t *testing.T) {
	config, err := Parse("server=host.example.com;database=db")
	require.NoError(t, err, "parsing")

	query := config.URL().Query()
	for _, key := range []string{
		Encrypt, TrustServerCertificate, Certificate, ServerCertificate, TLSMin,
		HostNameInCertificate, ApplicationIntent, MultiSubnetFailover, NoTraceID,
		EpaEnabled, PacketSize, ConnectionTimeout, KeepAlive, AppName,
		WorkstationID, ChangePassword,
	} {
		assert.NotContains(t, query, key, "URL() emitted %q although the connection string did not supply it", key)
	}
}

// TestConfigURLOmitsSecretParameters covers the deliberate omissions. These
// parameters carry credentials rather than configuration, and url.URL.Redacted()
// masks only the userinfo password, so anything put in the query survives the
// call Go offers as the safe one to log. A password change is also a login-time
// operation that replaying a serialized DSN should not reissue.
func TestConfigURLOmitsSecretParameters(t *testing.T) {
	const secret = "s3cr3t-value"

	tests := []struct {
		parameter string
		dsn       string
	}{
		{ChangePassword, "server=host.example.com;user id=u;password=p;change password=" + secret},
		{"systemtoken", "sqlserver://host.example.com?fedauth=ActiveDirectoryAzurePipelines&systemtoken=" + secret},
		{"clientassertion", "sqlserver://host.example.com?fedauth=ActiveDirectoryDefault&clientassertion=" + secret},
		{"userassertion", "sqlserver://host.example.com?fedauth=ActiveDirectoryDefault&userassertion=" + secret},
	}

	for _, tt := range tests {
		t.Run(tt.parameter, func(t *testing.T) {
			config, err := Parse(tt.dsn)
			require.NoError(t, err, "parsing")
			require.Equal(t, secret, config.Parameters[tt.parameter], "the parameter should have been parsed")

			u := config.URL()
			assert.NotContains(t, u.Query(), tt.parameter, "URL() emitted %q", tt.parameter)
			assert.NotContains(t, u.String(), secret, "URL().String() leaked %q", tt.parameter)
			assert.NotContains(t, u.Redacted(), secret, "URL().Redacted() leaked %q", tt.parameter)
		})
	}
}

// TestConfigURLCarriesAuthenticationSettings covers the settings azuread and
// integratedauth read out of Config.Parameters. msdsn has no field for any of
// them, so losing one leaves those packages selecting a different workflow.
func TestConfigURLCarriesAuthenticationSettings(t *testing.T) {
	tests := []struct {
		parameter string
		dsn       string
	}{
		{"fedauth", "sqlserver://host.example.com?fedauth=ActiveDirectoryDefault"},
		{"applicationclientid", "sqlserver://host.example.com?fedauth=ActiveDirectoryDefault&applicationclientid=client-id"},
		{"clientcertpath", "sqlserver://host.example.com?fedauth=ActiveDirectoryApplication&applicationclientid=client-id&clientcertpath=%2Fcerts%2Fclient.pfx"},
		{"resource id", "sqlserver://host.example.com?fedauth=ActiveDirectoryMSI&resource+id=resource"},
		{"serviceconnectionid", "sqlserver://host.example.com?fedauth=ActiveDirectoryAzurePipelines&serviceconnectionid=connection"},
		{"tokenfilepath", "sqlserver://host.example.com?fedauth=ActiveDirectoryDefault&tokenfilepath=%2Ftokens%2Ffile"},
		{"additionallyallowedtenants", "sqlserver://host.example.com?fedauth=ActiveDirectoryDefault&additionallyallowedtenants=one%2Ctwo"},
		{"disableinstancediscovery", "sqlserver://host.example.com?fedauth=ActiveDirectoryDefault&disableinstancediscovery=true"},
		{"sendcertificatechain", "sqlserver://host.example.com?fedauth=ActiveDirectoryApplication&applicationclientid=client-id&sendcertificatechain=true"},
		{"authenticator", "server=host.example.com;authenticator=krb5;krb5-realm=EXAMPLE.COM;krb5-configfile=/etc/krb5.conf"},
		{"krb5-realm", "server=host.example.com;authenticator=krb5;krb5-realm=EXAMPLE.COM;krb5-configfile=/etc/krb5.conf"},
		{"krb5-configfile", "server=host.example.com;authenticator=krb5;krb5-realm=EXAMPLE.COM;krb5-configfile=/etc/krb5.conf"},
		{"krb5-keytabfile", "server=host.example.com;authenticator=krb5;krb5-keytabfile=/etc/sql.keytab;krb5-realm=EXAMPLE.COM;krb5-configfile=/etc/krb5.conf"},
		{"krb5-credcachefile", "server=host.example.com;authenticator=krb5;krb5-credcachefile=/tmp/krb5cc;krb5-realm=EXAMPLE.COM;krb5-configfile=/etc/krb5.conf"},
		{"krb5-dnslookupkdc", "server=host.example.com;authenticator=krb5;krb5-dnslookupkdc=false;krb5-realm=EXAMPLE.COM;krb5-configfile=/etc/krb5.conf"},
		{"krb5-udppreferencelimit", "server=host.example.com;authenticator=krb5;krb5-udppreferencelimit=1024;krb5-realm=EXAMPLE.COM;krb5-configfile=/etc/krb5.conf"},
	}

	for _, tt := range tests {
		t.Run(tt.parameter, func(t *testing.T) {
			before, after := roundTrip(t, tt.dsn)
			supplied := before.Parameters[tt.parameter]
			require.NotEmpty(t, supplied, "the parameter should have been parsed")
			assert.Equal(t, supplied, after.Parameters[tt.parameter], "%q did not survive the round trip", tt.parameter)
		})
	}
}

// TestConfigURLPrefersEditedFields covers the precedence half of the contract:
// a field edited after parsing wins over the text it was parsed from, whether
// or not the connection string mentioned that setting.
func TestConfigURLPrefersEditedFields(t *testing.T) {
	t.Run("settings the connection string supplied", func(t *testing.T) {
		config, err := Parse("server=host.example.com;database=db;keepalive=9;app name=original;multisubnetfailover=true;packet size=1024")
		require.NoError(t, err, "parsing")

		config.KeepAlive = 20 * time.Second
		config.AppName = "edited"
		config.MultiSubnetFailover = false
		config.PacketSize = 4096

		reparsed, err := Parse(config.URL().String())
		require.NoError(t, err, "reparsing")

		assert.Equal(t, 20*time.Second, reparsed.KeepAlive, "KeepAlive")
		assert.Equal(t, "edited", reparsed.AppName, "AppName")
		assert.False(t, reparsed.MultiSubnetFailover, "MultiSubnetFailover")
		assert.Equal(t, uint16(4096), reparsed.PacketSize, "PacketSize")
	})

	t.Run("settings the connection string did not mention", func(t *testing.T) {
		config, err := Parse("server=host.example.com;database=db")
		require.NoError(t, err, "parsing")

		config.NoTraceID = true
		config.PacketSize = 8192
		config.ConnTimeout = 30 * time.Second
		config.ReadOnlyIntent = true

		reparsed, err := Parse(config.URL().String())
		require.NoError(t, err, "reparsing")

		assert.True(t, reparsed.NoTraceID, "NoTraceID")
		assert.Equal(t, uint16(8192), reparsed.PacketSize, "PacketSize")
		assert.Equal(t, 30*time.Second, reparsed.ConnTimeout, "ConnTimeout")
		assert.True(t, reparsed.ReadOnlyIntent, "ReadOnlyIntent")
	})

	t.Run("settings whose default Parse supplies is not the zero value", func(t *testing.T) {
		// These are the ones a "was the parameter present" test gets wrong:
		// Parse fills them in whether or not the connection string mentioned
		// them, so an edit is only visible by comparing against the default.
		config, err := Parse("server=host.example.com;database=db")
		require.NoError(t, err, "parsing")
		require.True(t, config.MultiSubnetFailover, "MultiSubnetFailover starts at the parser default")
		require.Equal(t, 30*time.Second, config.KeepAlive, "KeepAlive starts at the parser default")
		require.Equal(t, "go-mssqldb", config.AppName, "AppName starts at the parser default")
		require.True(t, config.TrustServerCertificate, "TrustServerCertificate starts trusting with no encrypt parameter")

		config.MultiSubnetFailover = false
		config.KeepAlive = 10 * time.Second
		config.AppName = "my-app"
		config.Workstation = "my-machine"
		config.TrustServerCertificate = false

		reparsed, err := Parse(config.URL().String())
		require.NoError(t, err, "reparsing")

		assert.False(t, reparsed.MultiSubnetFailover, "MultiSubnetFailover")
		assert.Equal(t, 10*time.Second, reparsed.KeepAlive, "KeepAlive")
		assert.Equal(t, "my-app", reparsed.AppName, "AppName")
		assert.Equal(t, "my-machine", reparsed.Workstation, "Workstation")
		// Turning verification on must not be undone by the round trip.
		assert.False(t, reparsed.TrustServerCertificate, "TrustServerCertificate")
		require.NotNil(t, reparsed.TLSConfig, "TLSConfig")
		assert.False(t, reparsed.TLSConfig.InsecureSkipVerify, "InsecureSkipVerify")
	})

	t.Run("a tls.Config edited after parsing", func(t *testing.T) {
		// TLSConfig.MinVersion is read back from the built config, so an edit to
		// it travels. ServerName deliberately does not: see the comment on the
		// hostnameincertificate emission in conn_str.go. Setting
		// HostInCertificateProvided alongside it is how a caller says the name
		// was chosen rather than defaulted.
		config, err := Parse("server=host.example.com;encrypt=true")
		require.NoError(t, err, "parsing")
		require.NotNil(t, config.TLSConfig, "TLSConfig")

		config.TLSConfig.MinVersion = tls.VersionTLS13
		config.TLSConfig.ServerName = "cert.example.com"
		config.HostInCertificateProvided = true

		reparsed, err := Parse(config.URL().String())
		require.NoError(t, err, "reparsing")
		require.NotNil(t, reparsed.TLSConfig, "TLSConfig after")
		assert.Equal(t, uint16(tls.VersionTLS13), reparsed.TLSConfig.MinVersion, "MinVersion")
		assert.Equal(t, "cert.example.com", reparsed.TLSConfig.ServerName, "ServerName")
	})

	t.Run("a cleared field is not reinstated by the parsed text", func(t *testing.T) {
		config, err := Parse("server=host.example.com;database=db;app name=original")
		require.NoError(t, err, "parsing")

		config.Database = ""

		reparsed, err := Parse(config.URL().String())
		require.NoError(t, err, "reparsing")
		assert.Empty(t, reparsed.Database, "Database")
	})
}

// TestConfigURLOmitsUnrepresentableValues covers fields holding a value the
// connection string grammar cannot spell. Emitting one produces a URL Parse
// rejects, and leaving the parsed text in its place would quietly restore a
// setting the caller has overridden, so the parameter is dropped.
func TestConfigURLOmitsUnrepresentableValues(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		edit func(*Config)
		// absent is the parameter the URL must not carry. Asserting that the
		// old value did not come back is not enough: an implementation that
		// truncated to zero instead of dropping would satisfy that too.
		absent string
		check  func(t *testing.T, reparsed Config)
	}{
		{
			name:   "negative keepalive disables keep-alives and has no spelling",
			absent: KeepAlive,
			dsn:    "server=host.example.com;keepalive=9",
			edit:   func(c *Config) { c.KeepAlive = -1 },
			check: func(t *testing.T, reparsed Config) {
				assert.NotEqual(t, 9*time.Second, reparsed.KeepAlive, "KeepAlive: the overridden value was restored")
			},
		},
		{
			name:   "negative connection timeout",
			absent: ConnectionTimeout,
			dsn:    "server=host.example.com;connection timeout=7",
			edit:   func(c *Config) { c.ConnTimeout = -5 * time.Second },
			check: func(t *testing.T, reparsed Config) {
				assert.NotEqual(t, 7*time.Second, reparsed.ConnTimeout, "ConnTimeout: the overridden value was restored")
			},
		},
		{
			name:   "sub-second connection timeout",
			absent: ConnectionTimeout,
			dsn:    "server=host.example.com;connection timeout=7",
			edit:   func(c *Config) { c.ConnTimeout = 500 * time.Millisecond },
			check: func(t *testing.T, reparsed Config) {
				assert.NotEqual(t, 7*time.Second, reparsed.ConnTimeout, "ConnTimeout: the overridden value was restored")
			},
		},
		{
			name:   "zero packet size means the driver default, not a size",
			absent: PacketSize,
			dsn:    "server=host.example.com;packet size=8192",
			edit:   func(c *Config) { c.PacketSize = 0 },
			check: func(t *testing.T, reparsed Config) {
				assert.Zero(t, reparsed.PacketSize, "PacketSize: reparsing must leave the driver default in place")
			},
		},
		{
			// DialTimeout is documented as negative to disable, so this is a
			// supported field value with no connection string spelling.
			name:   "negative dial timeout",
			absent: DialTimeout,
			dsn:    "server=host.example.com;dial timeout=7",
			edit:   func(c *Config) { c.DialTimeout = -1 * time.Second },
			check: func(t *testing.T, reparsed Config) {
				assert.NotEqual(t, 7*time.Second, reparsed.DialTimeout, "DialTimeout: the overridden value was restored")
			},
		},
		{
			name:   "sub-second dial timeout",
			absent: DialTimeout,
			dsn:    "server=host.example.com;dial timeout=7",
			edit:   func(c *Config) { c.DialTimeout = 500 * time.Millisecond },
			check: func(t *testing.T, reparsed Config) {
				assert.NotEqual(t, 7*time.Second, reparsed.DialTimeout, "DialTimeout: the overridden value was restored")
			},
		},
		{
			name:   "read-only intent without a database, which Parse rejects",
			absent: ApplicationIntent,
			dsn:    "server=host.example.com;database=db;applicationintent=ReadOnly",
			edit:   func(c *Config) { c.Database = "" },
			check: func(t *testing.T, reparsed Config) {
				assert.False(t, reparsed.ReadOnlyIntent, "ReadOnlyIntent")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := Parse(tt.dsn)
			require.NoError(t, err, "parsing")
			tt.edit(&config)

			u := config.URL()
			assert.NotContains(t, u.Query(), tt.absent, "URL() wrote %q for a value it cannot spell", tt.absent)

			// The point of the case: whatever else happens, the URL has to
			// remain something this package can read back.
			reparsed, err := Parse(u.String())
			require.NoError(t, err, "reparsing %q", u.String())
			tt.check(t, reparsed)
		})
	}
}

// TestConfigURLWithNilParameters covers a Config built as a struct literal,
// which has no Parameters map to consult. Its fields are what such a Config
// would connect with, so they are what the URL has to describe: reparsing must
// not quietly apply the parser's defaults over the top of them. The one field
// that is left to the parser's default is TrustServerCertificate, and only
// while the Config holds no view on TLS at all; see the end of the test.
func TestConfigURLWithNilParameters(t *testing.T) {
	config := Config{Host: "host.example.com", Port: 1433, Database: "db"}
	require.Nil(t, config.Parameters, "Parameters")

	u := config.URL()
	require.NotNil(t, u, "URL()")
	// encrypt stays out: EncryptionOff is the zero value, and emitting it for
	// every unconfigured Config would turn a default into a choice.
	assert.NotContains(t, u.Query(), Encrypt, "encrypt")

	reparsed, err := Parse(u.String())
	require.NoError(t, err, "reparsing")

	assert.Equal(t, "db", reparsed.Database, "Database")
	assert.Equal(t, config.MultiSubnetFailover, reparsed.MultiSubnetFailover, "MultiSubnetFailover")
	assert.Equal(t, config.KeepAlive, reparsed.KeepAlive, "KeepAlive")
	assert.Equal(t, config.AppName, reparsed.AppName, "AppName")
	assert.Equal(t, config.Workstation, reparsed.Workstation, "Workstation")
	// TLS is the exception. This Config has no tls.Config, the field at its
	// zero value and no parameter, which is no view on trust rather than a
	// choice to verify, so the URL says nothing and the reader applies the
	// parser's default for a DSN that names no encrypt. See
	// TestConfigURLLeavesAZeroValueTLSChoiceToTheReader for why: the driver's
	// own integration harness builds its Config this way.
	assert.NotContains(t, u.Query(), TrustServerCertificate, "trustservercertificate")
	assert.True(t, reparsed.TrustServerCertificate, "TrustServerCertificate")
	require.NotNil(t, reparsed.TLSConfig, "TLSConfig")
	assert.True(t, reparsed.TLSConfig.InsecureSkipVerify, "InsecureSkipVerify")
}

// TestConfigURLIsIdempotent checks that serializing a reparsed Config produces
// the same string again. A serializer that drops a setting loses it on the
// first round trip, so a stable second one is evidence that nothing is being
// shed along the way.
func TestConfigURLIsIdempotent(t *testing.T) {
	certFile, _ := newSelfSignedCert(t)

	for _, dsn := range []string{
		"server=host.example.com;encrypt=false",
		"server=host.example.com;encrypt=true;tlsmin=1.3;hostnameincertificate=other.example.com",
		"server=host.example.com;encrypt=true;certificate=" + certFile,
		"server=host.example.com;database=db;applicationintent=ReadOnly;multisubnetfailover=false;notraceid=true;packet size=8192;connection timeout=7;keepalive=9;app name=a;workstation id=w",
		"server=host.example.com;fedauth=ActiveDirectoryDefault;applicationclientid=client-id",
		"sqlserver://sa:sa@localhost/sqlexpress?database=master&log=127&disableretry=true&dial+timeout=30",
	} {
		t.Run(dsn, func(t *testing.T) {
			first, err := Parse(dsn)
			require.NoError(t, err, "parsing")
			firstURL := first.URL().String()

			second, err := Parse(firstURL)
			require.NoError(t, err, "reparsing")
			assert.Equal(t, firstURL, second.URL().String(), "URL() is not stable across a second round trip")
		})
	}
}

func TestTLSVersionToString(t *testing.T) {
	tests := []struct {
		version  uint16
		expected string
	}{
		{tls.VersionTLS10, "1.0"},
		{tls.VersionTLS11, "1.1"},
		{tls.VersionTLS12, "1.2"},
		{tls.VersionTLS13, "1.3"},
		{0, ""},
		{tls.VersionSSL30, ""},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, tlsVersionToString(tt.version), "tlsVersionToString(0x%04x)", tt.version)
		if tt.expected != "" {
			assert.Equal(t, tt.version, TLSVersionFromString(tt.expected), "TLSVersionFromString(%q)", tt.expected)
		}
	}
}

func TestTLSMinParameter(t *testing.T) {
	tests := []struct {
		version uint16
		want    string
	}{
		{0, ""},
		{tls.VersionSSL30, ""},
		{tls.VersionTLS10, "1.0"},
		{tls.VersionTLS11, "1.1"},
		{tls.VersionTLS12, "1.2"},
		{highestNamedTLSVersion, tlsVersionToString(highestNamedTLSVersion)},
		{highestNamedTLSVersion + 1, fmt.Sprintf("0x%04x", highestNamedTLSVersion+1)},
		{0xffff, "0xffff"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, tlsMinParameter(tt.version), "tlsMinParameter(0x%04x)", tt.version)
		if tt.want != "" {
			assert.Equal(t, tt.version, TLSVersionFromString(tt.want),
				"TLSVersionFromString(%q) should read back what tlsMinParameter wrote", tt.want)
		}
	}
}

// TestParseReadsTheVersionFloorURLWrites covers the one spelling this change
// adds to tlsmin, and its edges: 0x and exactly four hex digits, read only above
// the highest version tlsmin has a name for. Everything else reads as it always
// has.
func TestParseReadsTheVersionFloorURLWrites(t *testing.T) {
	tests := []struct {
		tlsMin string
		want   uint16
	}{
		{"1.2", tls.VersionTLS12},
		{fmt.Sprintf("0x%04x", highestNamedTLSVersion+1), highestNamedTLSVersion + 1},
		{"0xffff", 0xffff},
		// The named range is spelled by name only; a number there meant the
		// default before and still does.
		{fmt.Sprintf("0x%04x", highestNamedTLSVersion), 0},
		{"0x0301", 0},
		{"0x0300", 0},
		// Not the spelling: the tls package default, as before.
		{"", 0},
		{"1.4", 0},
		{"12", 0},
		{"772", 0},
		{"0x", 0},
		{"0x1", 0},
		{"0x300", 0},
		{"0x00305", 0},
		{"0X0305", 0},
		{"0xzzzz", 0},
	}

	for _, tt := range tests {
		t.Run(tt.tlsMin, func(t *testing.T) {
			config, err := Parse("server=host.example.com;encrypt=true;tlsmin=" + tt.tlsMin)
			require.NoError(t, err, "parsing")
			require.NotNil(t, config.TLSConfig, "TLSConfig")
			assert.Equal(t, tt.want, config.TLSConfig.MinVersion, "MinVersion")
		})
	}
}

// TestParseDoesNotReadANumberIntoTheNamedRange is the reason for the bound in
// tlsVersionFromNumber, shown on the wire. crypto/tls keeps TLS 1.0 and 1.1 out
// only while MinVersion is zero, so reading 0x0300 as a floor would give a
// string that meant the default yesterday a looser meaning today, and a
// connection string that has always negotiated TLS 1.2 would reach a TLS 1.1
// server after an upgrade. The server here speaks nothing newer than TLS 1.1.
func TestParseDoesNotReadANumberIntoTheNamedRange(t *testing.T) {
	serverCert, _ := newSelfSignedServer(t)
	oldServer := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		MinVersion:   tls.VersionTLS10,
		MaxVersion:   tls.VersionTLS11,
	}

	// The control: what reading the number would have done.
	loosened := &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionSSL30}
	require.NoError(t, handshakeBetween(t, oldServer, loosened),
		"a floor of 0x0300 reaches a TLS 1.1 server; that is what must not come out of a DSN")

	for _, tlsMin := range []string{"", "0x0300", "0x0301", "0x300", "0x1"} {
		t.Run("tlsmin="+tlsMin, func(t *testing.T) {
			config, err := Parse("server=host.example.com;encrypt=true;trustservercertificate=true;tlsmin=" + tlsMin)
			require.NoError(t, err, "parsing")
			require.NotNil(t, config.TLSConfig, "TLSConfig")
			assert.Zero(t, config.TLSConfig.MinVersion, "MinVersion: the tls package default, as before")
			assert.Error(t, handshakeBetween(t, oldServer, config.TLSConfig),
				"the default floor keeps TLS 1.0 and 1.1 out")
		})
	}
}

// TestConfigURLCarriesAVersionFloorItCannotName covers a MinVersion tlsmin has
// no name for. On today's crypto/tls a floor above TLS 1.3 is a Config no
// handshake can satisfy - the direct handshake below fails saying so - and
// dropping the parameter or rounding it down to a name would have come back
// as a Config that connects. Written as its protocol number it comes back as
// exactly what it was, and the handshake after fails for the same reason.
func TestConfigURLCarriesAVersionFloorItCannotName(t *testing.T) {
	server, _ := newSelfSignedServer(t)

	config, err := Parse("server=host.example.com;encrypt=true;trustservercertificate=true;tlsmin=1.2")
	require.NoError(t, err, "parsing")
	config.TLSConfig.MinVersion = tls.VersionTLS13 + 1

	before := handshakeWith(t, server, config.TLSConfig)
	require.Error(t, before, "no handshake satisfies this floor today")
	assert.Contains(t, before.Error(), "no supported versions", "crypto/tls says why")

	u := config.URL()
	assert.Equal(t, "0x0305", u.Query().Get(TLSMin), "the number, since there is no name")

	reparsed, err := Parse(u.String())
	require.NoError(t, err, "reparsing %q", u.String())
	require.NotNil(t, reparsed.TLSConfig, "TLSConfig after")
	assert.Equal(t, uint16(tls.VersionTLS13+1), reparsed.TLSConfig.MinVersion, "MinVersion after")

	after := handshakeWith(t, server, reparsed.TLSConfig)
	require.Error(t, after, "the floor came back and still refuses everything")
	assert.Equal(t, before.Error(), after.Error(), "for the same reason")
}

// TestConfigURLCannotCarryACeilingOrCipherSuites pins the parts of a tls.Config
// the grammar has no parameter for at all. A Config restricted by one of them
// reparses as one that is not, and that is a place a round trip accepts more
// than the Config did; it belongs in the file rather than left to be found.
// Carrying either would mean a new connection string parameter, which is not
// this change's to add.
func TestConfigURLCannotCarryACeilingOrCipherSuites(t *testing.T) {
	const dsn = "server=host.example.com;encrypt=true;trustservercertificate=true"

	t.Run("a version ceiling", func(t *testing.T) {
		config, err := Parse(dsn)
		require.NoError(t, err, "parsing")
		config.TLSConfig.MaxVersion = tls.VersionTLS12

		reparsed, err := Parse(config.URL().String())
		require.NoError(t, err, "reparsing")
		require.NotNil(t, reparsed.TLSConfig, "TLSConfig after")
		assert.Zero(t, reparsed.TLSConfig.MaxVersion, "MaxVersion after: there is no tlsmax")
	})

	t.Run("a cipher suite list", func(t *testing.T) {
		config, err := Parse(dsn)
		require.NoError(t, err, "parsing")
		config.TLSConfig.CipherSuites = []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384}

		reparsed, err := Parse(config.URL().String())
		require.NoError(t, err, "reparsing")
		require.NotNil(t, reparsed.TLSConfig, "TLSConfig after")
		assert.Nil(t, reparsed.TLSConfig.CipherSuites, "CipherSuites after: every default suite")
	})
}

func TestWholeSeconds(t *testing.T) {
	tests := []struct {
		duration time.Duration
		expected string
		ok       bool
	}{
		{7 * time.Second, "7", true},
		{time.Second, "1", true},
		{0, "0", true},
		{-5 * time.Second, "", false},
		{500 * time.Millisecond, "", false},
		{1500 * time.Millisecond, "", false},
	}

	for _, tt := range tests {
		seconds, ok := wholeSeconds(tt.duration)
		assert.Equal(t, tt.ok, ok, "wholeSeconds(%v) ok", tt.duration)
		assert.Equal(t, tt.expected, seconds, "wholeSeconds(%v) value", tt.duration)
	}
}

// epa enabled is the one setting whose absence from a connection string does not
// mean a fixed value: Parse falls back to MSSQL_USE_EPA. The contract is that a
// choice somebody made travels with the URL, while a value the environment
// supplied is left for the reading environment to supply again.
//
// TestConfigURLCarriesAnExplicitEpaChoice covers the first half. Losing it is
// what issue #455 reports for this row, and the consequence it names is channel
// binding changing underneath the connection.
func TestConfigURLCarriesAnExplicitEpaChoice(t *testing.T) {
	tests := []struct {
		name     string
		writeEnv string
		dsn      string
		edit     func(*Config)
		readEnv  string
		want     bool
	}{
		{
			name:     "explicit true survives a reader with no environment",
			writeEnv: "", dsn: "server=host.example.com;epa enabled=true",
			readEnv: "", want: true,
		},
		{
			name:     "explicit false is not overridden by the reader's environment",
			writeEnv: "", dsn: "server=host.example.com;epa enabled=false",
			readEnv: "true", want: false,
		},
		{
			name:     "explicit false survives an environment that says otherwise at both ends",
			writeEnv: "true", dsn: "server=host.example.com;epa enabled=false",
			readEnv: "true", want: false,
		},
		{
			// Parse rejects an unreadable environment value when it has no
			// parameter to prefer, so dropping the parameter here would build a
			// URL that cannot be read back at all.
			name:     "an explicit value keeps the URL readable under an invalid environment",
			writeEnv: "invalid", dsn: "server=host.example.com;epa enabled=false",
			readEnv: "invalid", want: false,
		},
		{
			name:     "turning it on in code is carried even with no parameter",
			writeEnv: "", dsn: "server=host.example.com",
			edit:    func(c *Config) { c.EpaEnabled = true },
			readEnv: "", want: true,
		},
		{
			name:     "turning it off in code is not undone by the reader's environment",
			writeEnv: "true", dsn: "server=host.example.com",
			edit:    func(c *Config) { c.EpaEnabled = false },
			readEnv: "true", want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("MSSQL_USE_EPA", tt.writeEnv)
			config, err := Parse(tt.dsn)
			require.NoError(t, err, "parsing")
			if tt.edit != nil {
				tt.edit(&config)
			}
			require.Equal(t, tt.want, config.EpaEnabled, "EpaEnabled before the round trip")

			u := config.URL().String()

			t.Setenv("MSSQL_USE_EPA", tt.readEnv)
			reparsed, err := Parse(u)
			require.NoError(t, err, "reparsing %q", u)
			assert.Equal(t, tt.want, reparsed.EpaEnabled, "EpaEnabled after the round trip of %q", u)
		})
	}
}

// TestConfigURLLeavesAnAmbientEpaValueToTheReader covers the other half of the
// contract, in both directions. A connection string that never named the setting
// was already asking whichever process read it; the round trip keeps asking
// rather than freezing the writing process's answer into the URL, which also
// keeps epa enabled out of every URL that nobody configured.
func TestConfigURLLeavesAnAmbientEpaValueToTheReader(t *testing.T) {
	tests := []struct {
		name     string
		writeEnv string
		readEnv  string
		want     bool
	}{
		{name: "on at the writer, off at the reader", writeEnv: "true", readEnv: "", want: false},
		{name: "off at the writer, on at the reader", writeEnv: "", readEnv: "true", want: true},
		{name: "off at both ends", writeEnv: "", readEnv: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("MSSQL_USE_EPA", tt.writeEnv)
			config, err := Parse("server=host.example.com;database=db")
			require.NoError(t, err, "parsing")
			assert.NotContains(t, config.URL().Query(), EpaEnabled,
				"an environment-supplied value should not be written into the URL")

			u := config.URL().String()

			t.Setenv("MSSQL_USE_EPA", tt.readEnv)
			reparsed, err := Parse(u)
			require.NoError(t, err, "reparsing")
			assert.Equal(t, tt.want, reparsed.EpaEnabled, "EpaEnabled after the round trip")
		})
	}
}

// serialization records what Config.URL() does with a connection string
// parameter.
type serialization int

const (
	// inQuery: URL() writes the setting into the query string.
	inQuery serialization = iota
	// inURLStructure: the setting travels in the URL's host or userinfo.
	inURLStructure
	// notSerialized: URL() deliberately drops the setting.
	notSerialized
)

// parameterSerialization classifies every parameter the connection string parser
// declares. TestEveryParameterIsClassified reads that declaration out of the
// source and fails when a parameter is missing here, which is the drift issue
// #455 names as its mechanism: "parsing retains source parameters and populates
// typed fields, while URL() independently maintains a small allowlist of emitted
// parameters. A newly supported parser setting can therefore be forgotten by the
// serializer."
var parameterSerialization = map[string]serialization{
	// Spelled inline in parse() rather than declared as a constant.
	"columnencryption":     inQuery,
	AppName:                inQuery,
	ApplicationIntent:      inQuery,
	Certificate:            inQuery,
	ConnectionTimeout:      inQuery,
	Database:               inQuery,
	DialTimeout:            inQuery,
	DisableRetry:           inQuery,
	Encrypt:                inQuery,
	EpaEnabled:             inQuery,
	FailoverPartner:        inQuery,
	FailoverPartnerSpn:     inQuery,
	FailOverPort:           inQuery,
	GuidConversion:         inQuery,
	HostNameInCertificate:  inQuery,
	KeepAlive:              inQuery,
	LogParam:               inQuery,
	MultiSubnetFailover:    inQuery,
	NoTraceID:              inQuery,
	PacketSize:             inQuery,
	Pipe:                   inQuery,
	Protocol:               inQuery,
	ServerCertificate:      inQuery,
	ServerSpn:              inQuery,
	Timezone:               inQuery,
	TLSMin:                 inQuery,
	TrustServerCertificate: inQuery,
	WorkstationID:          inQuery,

	Password: inURLStructure,
	Port:     inURLStructure,
	Server:   inURLStructure,
	UserID:   inURLStructure,

	// A login-time credential. See carriedVerbatim in conn_str.go.
	ChangePassword: notSerialized,
}

// declaredParameters reads the connection string parameter constants out of
// conn_str.go. Go cannot enumerate a package's constants at run time, so the
// test reads the declaration it is guarding.
func declaredParameters(t *testing.T) map[string]string {
	t.Helper()

	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "conn_str.go", nil, 0)
	require.NoError(t, err, "parsing conn_str.go")

	for _, decl := range file.Decls {
		declaration, ok := decl.(*ast.GenDecl)
		if !ok || declaration.Tok != token.CONST {
			continue
		}
		names := map[string]string{}
		for _, spec := range declaration.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || len(value.Names) != 1 || len(value.Values) != 1 {
				continue
			}
			literal, ok := value.Values[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				continue
			}
			unquoted, err := strconv.Unquote(literal.Value)
			if err != nil {
				continue
			}
			names[unquoted] = value.Names[0].Name
		}
		// The block that declares Database is the parameter block; the other
		// const blocks in this file hold enums and flags.
		if _, ok := names[Database]; ok {
			addInlineParameters(file, names)
			return names
		}
	}

	t.Fatal("conn_str.go has no const block declaring the connection string parameters")
	return nil
}

// addInlineParameters collects the parameters the parser reads out of params
// with a string literal instead of a constant. columnencryption is the one that
// does today, and it is exactly the kind of name this guard exists for: spelling
// a parameter inline must not be a way of slipping past the check.
func addInlineParameters(file *ast.File, names map[string]string) {
	ast.Inspect(file, func(node ast.Node) bool {
		index, ok := node.(*ast.IndexExpr)
		if !ok {
			return true
		}
		identifier, ok := index.X.(*ast.Ident)
		if !ok || identifier.Name != "params" {
			return true
		}
		literal, ok := index.Index.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		unquoted, err := strconv.Unquote(literal.Value)
		if err != nil {
			return true
		}
		if _, ok := names[unquoted]; !ok {
			names[unquoted] = strconv.Quote(unquoted) + " (spelled inline)"
		}
		return true
	})
}

func TestEveryParameterIsClassified(t *testing.T) {
	declared := declaredParameters(t)

	for parameter, constant := range declared {
		if _, ok := parameterSerialization[parameter]; !ok {
			t.Errorf("%s (%q) is parsed but Config.URL() has no decision about it. "+
				"Add it to parameterSerialization in this file, and serialize it unless there is a reason not to",
				constant, parameter)
		}
	}
	for parameter := range parameterSerialization {
		if _, ok := declared[parameter]; !ok {
			t.Errorf("%q is classified here but conn_str.go no longer declares it", parameter)
		}
	}
}

// TestClassifiedQueryParametersAreEmitted holds the classification to the real
// behaviour of URL(), so a parameter cannot be recorded as serialized without
// the serializer actually writing it.
func TestClassifiedQueryParametersAreEmitted(t *testing.T) {
	t.Setenv("MSSQL_USE_EPA", "")
	certFile, _ := newSelfSignedCert(t)

	values := map[string]string{
		"columnencryption":    "true",
		AppName:               "roundtrip-demo",
		ApplicationIntent:     "ReadOnly",
		Certificate:           certFile,
		ConnectionTimeout:     "7",
		Database:              "db",
		DialTimeout:           "30",
		DisableRetry:          "true",
		Encrypt:               "false",
		EpaEnabled:            "true",
		FailoverPartner:       "mirror",
		FailoverPartnerSpn:    "MSSQLSvc/mirror:2000",
		FailOverPort:          "2000",
		GuidConversion:        "true",
		HostNameInCertificate: "other.example.com",
		KeepAlive:             "9",
		LogParam:              "127",
		MultiSubnetFailover:   "false",
		NoTraceID:             "true",
		PacketSize:            "8192",
		Pipe:                  "sqlquery",
		Protocol:              "tcp",
		ServerCertificate:     certFile,
		ServerSpn:             "MSSQLSvc/primary:1433",
		// Anything but UTC, which URL() reads as "not configured".
		Timezone:               "America/New_York",
		TLSMin:                 "1.2",
		TrustServerCertificate: "false",
		WorkstationID:          "roundtrip-client",
	}

	for parameter, class := range parameterSerialization {
		if class != inQuery {
			continue
		}
		value, ok := values[parameter]
		if !ok {
			t.Errorf("%q is classified inQuery but this test has no value to try it with", parameter)
			continue
		}

		t.Run(parameter, func(t *testing.T) {
			dsn := "server=host.example.com;" + parameter + "=" + value
			switch parameter {
			case ApplicationIntent:
				dsn += ";" + Database + "=db"
			case Certificate, ServerCertificate, TLSMin, HostNameInCertificate:
				dsn += ";" + Encrypt + "=true"
			case Timezone:
				if _, err := time.LoadLocation(value); err != nil {
					t.Skipf("no time zone database available: %v", err)
				}
			}

			config, err := Parse(dsn)
			require.NoError(t, err, "parsing %q", dsn)
			assert.Contains(t, config.URL().Query(), parameter,
				"URL() dropped %q although the connection string supplied it", parameter)
		})
	}
}

// TestConfigURLKeepsWorkstationIDMatchingTheLocalHost covers the other setting
// whose default is ambient rather than a constant. Parse fills Workstation from
// os.Hostname(), so a connection string that names this machine produces a field
// indistinguishable from an unset one. Dropping it would hand the reading
// machine's name to the server instead of the one that was asked for.
func TestConfigURLKeepsWorkstationIDMatchingTheLocalHost(t *testing.T) {
	hostname, err := os.Hostname()
	require.NoError(t, err, "reading the local host name")
	require.NotEmpty(t, hostname, "local host name")

	before, after := roundTrip(t, "server=host.example.com;workstation id="+hostname)
	require.Equal(t, hostname, before.Workstation, "Workstation before")

	assert.Contains(t, before.URL().Query(), WorkstationID, "workstation id")
	assert.Equal(t, hostname, after.Parameters[WorkstationID], "workstation id parameter")
	assert.Equal(t, hostname, after.Workstation, "Workstation after")
}

// TestConfigURLNeverAdvertisesTrustOnStrictEncryption pins a deliberate silence.
// parseTLS forces TrustServerCertificate to false under encrypt=strict whatever
// the parameter says, so writing one would put a trust setting into the string
// that this driver ignores and another client reading the same DSN might not.
func TestConfigURLNeverAdvertisesTrustOnStrictEncryption(t *testing.T) {
	// The connection string has to have supplied the parameter, because a
	// trusting value is only ever written back when it did; without that the
	// guard under test is never reached and this passes for the wrong reason.
	config, err := Parse("server=host.example.com;encrypt=strict;trustservercertificate=true")
	require.NoError(t, err, "parsing")
	require.False(t, config.TrustServerCertificate, "parseTLS forces this to false under strict")

	// An inconsistent Config: it trusts anything, strict encryption does not.
	// The tls.Config has to say so too, because that is what the serializer
	// reads rather than the field.
	config.TrustServerCertificate = true
	config.TLSConfig.InsecureSkipVerify = true
	require.True(t, config.trustsAnyCertificate(), "the guard under test only runs for a trusting Config")

	u := config.URL()
	assert.Contains(t, u.Query(), Encrypt, "encrypt")
	assert.NotContains(t, u.Query(), TrustServerCertificate, "trustservercertificate")

	reparsed, err := Parse(u.String())
	require.NoError(t, err, "reparsing")
	assert.Equal(t, Encryption(EncryptionStrict), reparsed.Encryption, "Encryption")
	assert.False(t, reparsed.TrustServerCertificate, "TrustServerCertificate")
	require.NotNil(t, reparsed.TLSConfig, "TLSConfig")
	assert.False(t, reparsed.TLSConfig.InsecureSkipVerify, "InsecureSkipVerify")
}

// TestConfigURLWritesEpaEnabledWhenTheEnvironmentIsUnreadable covers the corner
// where deferring to the reading environment is not an option. Parse rejects an
// MSSQL_USE_EPA value it cannot read, so a URL that leaves the parameter out
// cannot be read back at all in such a process. An unusable environment is
// therefore not a default to compare against, and the field is written.
func TestConfigURLWritesEpaEnabledWhenTheEnvironmentIsUnreadable(t *testing.T) {
	t.Setenv("MSSQL_USE_EPA", "invalid")

	// A struct literal has no Parameters map to fall back on, which is what
	// makes this the case the environment comparison has to get right.
	config := Config{Host: "host.example.com"}
	require.Nil(t, config.Parameters, "Parameters")

	u := config.URL().String()
	assert.Contains(t, config.URL().Query(), EpaEnabled, "epa enabled")

	reparsed, err := Parse(u)
	require.NoError(t, err, "reparsing %q", u)
	assert.False(t, reparsed.EpaEnabled, "EpaEnabled")
}

// TestConfigURLDoesNotPinACertificateNameFromAHostEdit covers the reason
// hostnameincertificate is written from HostInCertificateProvided alone rather
// than from ServerName differing from Host. The two also diverge when a caller
// moves Host and leaves the tls.Config alone, which failoverPartnerParams in the
// root package does, and writing the old name there would check the certificate
// of a server the connection is no longer going to. Worse, it would come back
// with HostInCertificateProvided set, which is the flag connect() reads to
// decide whether to retarget ServerName after a routing redirect.
func TestConfigURLDoesNotPinACertificateNameFromAHostEdit(t *testing.T) {
	config, err := Parse("server=primary.example.com;encrypt=true")
	require.NoError(t, err, "parsing")
	require.False(t, config.HostInCertificateProvided, "HostInCertificateProvided")

	config.Host = "mirror.example.com"

	u := config.URL()
	assert.NotContains(t, u.Query(), HostNameInCertificate, "hostnameincertificate")

	reparsed, err := Parse(u.String())
	require.NoError(t, err, "reparsing")
	require.NotNil(t, reparsed.TLSConfig, "TLSConfig")
	assert.Equal(t, "mirror.example.com", reparsed.TLSConfig.ServerName, "ServerName follows the host")
	assert.False(t, reparsed.HostInCertificateProvided, "HostInCertificateProvided must stay unset so a redirect can still retarget")
}

// TestConfigURLKeepsAPinnedCertificateReadable covers a combination Parse
// rejects: servercertificate and hostnameincertificate together. The pin skips
// hostname validation by design, so a certificate name has nothing to say there,
// and writing one would produce a URL this package cannot read back.
func TestConfigURLKeepsAPinnedCertificateReadable(t *testing.T) {
	certFile, _ := newSelfSignedCert(t)

	config, err := Parse("server=primary.example.com;encrypt=true;servercertificate=" + certFile)
	require.NoError(t, err, "parsing")

	// Both of the edits that used to make URL() write hostnameincertificate.
	config.Host = "mirror.example.com"
	config.TLSConfig.ServerName = "ignored.example.com"

	u := config.URL()
	assert.NotContains(t, u.Query(), HostNameInCertificate, "hostnameincertificate")
	assert.Contains(t, u.Query(), ServerCertificate, "servercertificate")

	reparsed, err := Parse(u.String())
	require.NoError(t, err, "reparsing %q", u.String())
	require.NotNil(t, reparsed.TLSConfig, "TLSConfig")
	assert.NotNil(t, reparsed.TLSConfig.VerifyPeerCertificate, "the pin survived")
}

// TestConfigURLDropsTrustBesideAPinAndKeepsTheHandshake records a deliberate
// loss of field fidelity. A servercertificate pin installs a VerifyPeerCertificate
// callback alongside InsecureSkipVerify, and URL() treats any callback as
// verification, so trustservercertificate is not written even when the
// connection string supplied it. The pin governs the handshake either way, so
// what the connection does is unchanged - which is the thing that has to hold.
func TestConfigURLDropsTrustBesideAPinAndKeepsTheHandshake(t *testing.T) {
	certFile, der := newSelfSignedCert(t)

	before, after := roundTrip(t,
		"server=host.example.com;encrypt=true;servercertificate="+certFile+";trustservercertificate=true")

	require.True(t, before.TrustServerCertificate, "TrustServerCertificate before")
	assert.False(t, after.TrustServerCertificate, "TrustServerCertificate after: deliberate, see the comment")

	require.NotNil(t, before.TLSConfig, "TLSConfig before")
	require.NotNil(t, after.TLSConfig, "TLSConfig after")
	assert.Equal(t, before.TLSConfig.InsecureSkipVerify, after.TLSConfig.InsecureSkipVerify, "InsecureSkipVerify")

	verify := after.TLSConfig.VerifyPeerCertificate
	require.NotNil(t, verify, "the pin survived")
	assert.NoError(t, verify([][]byte{der}, nil), "the pinned certificate is still accepted")
	_, otherDER := newSelfSignedCert(t)
	assert.Error(t, verify([][]byte{otherDER}, nil), "another certificate is still rejected")
}

// TestConfigURLKeepsACommonNameCheckVerifying covers the other place SetupTLS
// turns InsecureSkipVerify on for a configuration that still verifies: a
// certificate whose common name contains a colon gets its own VerifyConnection
// callback. Reading InsecureSkipVerify alone as "trusts anything" would turn
// that into trustservercertificate=true and throw the callback away.
func TestConfigURLKeepsACommonNameCheckVerifying(t *testing.T) {
	certFile, _ := newSelfSignedCert(t)

	before, after := roundTrip(t,
		"server=host.example.com;encrypt=true;trustservercertificate=false;certificate="+certFile+";hostnameincertificate=a:b")

	require.NotNil(t, before.TLSConfig, "TLSConfig before")
	require.True(t, before.TLSConfig.InsecureSkipVerify, "SetupTLS turns this on for the common name path")
	require.NotNil(t, before.TLSConfig.VerifyConnection, "VerifyConnection before")

	assert.False(t, after.TrustServerCertificate, "TrustServerCertificate")
	require.NotNil(t, after.TLSConfig, "TLSConfig after")
	assert.NotNil(t, after.TLSConfig.VerifyConnection, "VerifyConnection after: the check was replaced by plain trust")
}

// TestConfigURLKeepsAnAnchorItCanStillName covers a tls.Config a caller has
// added a check to. The check itself has no connection string spelling and does
// not travel, but the anchor underneath it does, and that is the part worth
// keeping: a pin names one certificate and a private pool names one CA, while a
// URL with neither verifies against system roots and accepts every certificate a
// public CA has issued for the host.
//
// Reading "SetupTLS would not rebuild this whole tls.Config" as a reason to drop
// the parameter throws the anchor away over a check that is lost either way. The
// parameter stays while it is still a true statement about what the connection
// does, even where it is no longer the whole of it.
func TestConfigURLKeepsAnAnchorItCanStillName(t *testing.T) {
	certificateFile, _ := newSelfSignedCert(t)
	pinFile, pinDER := newSelfSignedCert(t)
	_, otherDER := newSelfSignedCert(t)

	callerRejects := func(tls.ConnectionState) error { return errors.New("the caller rejects it") }

	t.Run("a pin beside a check of the caller's own", func(t *testing.T) {
		config, err := Parse("server=host.example.com;encrypt=true;servercertificate=" + pinFile)
		require.NoError(t, err, "parsing")
		config.TLSConfig.VerifyConnection = callerRejects

		assertPinSurvives(t, config, pinDER, otherDER)
	})

	t.Run("a root pool beside a check of the caller's own", func(t *testing.T) {
		config, err := Parse("server=host.example.com;encrypt=true;certificate=" + certificateFile)
		require.NoError(t, err, "parsing")
		config.TLSConfig.VerifyConnection = callerRejects

		assertPoolSurvives(t, config, certificateFile)
	})

	t.Run("a root pool beside a verification callback of the caller's own", func(t *testing.T) {
		// The pool is still the file's, byte for byte, and the chain is still
		// built against it on every handshake.
		config, err := Parse("server=host.example.com;encrypt=true;certificate=" + certificateFile)
		require.NoError(t, err, "parsing")
		config.TLSConfig.VerifyPeerCertificate = func([][]byte, [][]*x509.Certificate) error {
			return errors.New("the caller rejects it")
		}

		assertPoolSurvives(t, config, certificateFile)
	})
}

func assertPinSurvives(t *testing.T, config Config, pinned, other []byte) {
	t.Helper()
	u := config.URL()
	require.Contains(t, u.Query(), ServerCertificate,
		"the pin is the narrowest thing a connection string can name")

	reparsed, err := Parse(u.String())
	require.NoError(t, err, "reparsing %q", u.String())
	require.NotNil(t, reparsed.TLSConfig, "TLSConfig after")
	verify := reparsed.TLSConfig.VerifyPeerCertificate
	require.NotNil(t, verify, "the pin came back")
	assert.NoError(t, verify([][]byte{pinned}, nil), "the pinned certificate is still accepted")
	assert.Error(t, verify([][]byte{other}, nil), "another certificate is still rejected")
}

func assertPoolSurvives(t *testing.T, config Config, certificateFile string) {
	t.Helper()
	pemBytes, err := readCertificate(certificateFile)
	require.NoError(t, err, "reading the certificate file")
	fromFile := x509.NewCertPool()
	require.True(t, fromFile.AppendCertsFromPEM(pemBytes), "building a pool from the file")
	require.True(t, config.TLSConfig.RootCAs.Equal(fromFile), "the pool is still the file's")
	require.False(t, config.TLSConfig.InsecureSkipVerify, "and a chain is still built against it")

	u := config.URL()
	require.Contains(t, u.Query(), Certificate, "the file still names the roots in use")

	reparsed, err := Parse(u.String())
	require.NoError(t, err, "reparsing %q", u.String())
	require.NotNil(t, reparsed.TLSConfig, "TLSConfig after")
	require.NotNil(t, reparsed.TLSConfig.RootCAs, "RootCAs after: the chain fell back to system roots")
	assert.True(t, reparsed.TLSConfig.RootCAs.Equal(fromFile), "and it is the same pool")
	assert.False(t, reparsed.TLSConfig.InsecureSkipVerify, "InsecureSkipVerify after")
}

// TestConfigURLCarriesCertificateSettingsWithEncryptionOff covers the shortcut
// at the top of retainedCertificateParameterApplies. With encryption disabled no
// certificate is exchanged and there is no tls.Config to read the settings back
// out of, so the parsed text is the only record they have and dropping it would
// lose a named file from the DSN for a setting that is merely inert.
func TestConfigURLCarriesCertificateSettingsWithEncryptionOff(t *testing.T) {
	certificateFile, _ := newSelfSignedCert(t)

	config, err := Parse("server=host.example.com;encrypt=disable;certificate=" + certificateFile)
	require.NoError(t, err, "parsing")
	require.Nil(t, config.TLSConfig, "encryption disabled builds no tls.Config")

	u := config.URL()
	assert.Equal(t, certificateFile, u.Query().Get(Certificate), "certificate")

	reparsed, err := Parse(u.String())
	require.NoError(t, err, "reparsing %q", u.String())
	assert.Equal(t, EncryptionDisabled, int(reparsed.Encryption), "Encryption")
	assert.Equal(t, certificateFile, reparsed.Parameters[Certificate], "certificate after")
}

// TestConfigURLRefusesToWriteAChainAndAPin covers the one Config URL() writes
// so that Parse refuses it.
//
// A servercertificate pin with ordinary verification left on accepts only a
// certificate that passes both the chain check and the byte comparison:
// crypto/tls runs the chain first and calls VerifyPeerCertificate after. No
// connection string produces that, because the pin path turns verification
// off, and each half on its own accepts certificates the Config rejects. The
// pin alone accepts the self-signed certificate a pin usually names, which the
// chain would refuse - the handshake below shows exactly that. The chain alone
// accepts every certificate a public CA has issued for the host. Both are true
// statements about the Config, so the URL names both, and parseTLS rejects the
// pair with an error that says which two it cannot combine.
//
// This is the only answer that accepts nothing the Config would not, at the
// cost of a DSN that does not read back. Which of the three is wanted is a
// decision for a maintainer; it is written this way so that the cost is an
// error rather than a wider connection.
func TestConfigURLRefusesToWriteAChainAndAPin(t *testing.T) {
	server, pinFile := newSelfSignedServer(t)

	config, err := Parse("server=host.example.com;encrypt=true;servercertificate=" + pinFile)
	require.NoError(t, err, "parsing")
	require.True(t, config.TLSConfig.InsecureSkipVerify, "the pin path turns verification off")
	config.TLSConfig.InsecureSkipVerify = false

	// What the two halves do on their own against the pinned server itself.
	require.Error(t, handshakeWith(t, server, config.TLSConfig),
		"chain and pin: the self-signed certificate fails the chain")
	pinOnly, err := Parse("server=host.example.com;encrypt=true;servercertificate=" + pinFile)
	require.NoError(t, err, "parsing the pin alone")
	require.NoError(t, handshakeWith(t, server, pinOnly.TLSConfig),
		"the pin alone accepts it - writing only the pin would widen the connection")

	u := config.URL()
	assert.Contains(t, u.Query(), ServerCertificate, "the pin half")
	assert.Contains(t, u.Query(), HostNameInCertificate, "the chain half, as the name it checks")

	_, err = Parse(u.String())
	require.Error(t, err, "the pair has to be refused: %q", u.String())
	assert.Contains(t, err.Error(), "cannot specify both", "and the refusal should say which pair")
}

// TestConfigURLWritesTheNameChosenAfterAPinIsRemoved covers a connection string
// that named a pin and a caller who then took it out and chose a certificate
// name instead. The stale parameter says nothing about what the connection
// checks now; only a pin that is actually written has a reason to suppress the
// name, because that is the pair parseTLS rejects.
func TestConfigURLWritesTheNameChosenAfterAPinIsRemoved(t *testing.T) {
	pinFile, _ := newSelfSignedCert(t)

	config, err := Parse("server=host.example.com;encrypt=true;servercertificate=" + pinFile)
	require.NoError(t, err, "parsing")
	config.TLSConfig.VerifyPeerCertificate = nil
	config.TLSConfig.InsecureSkipVerify = false
	config.TLSConfig.ServerName = "cert.example.com"
	config.HostInCertificateProvided = true

	u := config.URL()
	assert.NotContains(t, u.Query(), ServerCertificate, "the pin is gone")
	assert.Equal(t, "cert.example.com", u.Query().Get(HostNameInCertificate), "the name the caller chose")

	reparsed, err := Parse(u.String())
	require.NoError(t, err, "reparsing %q", u.String())
	require.NotNil(t, reparsed.TLSConfig, "TLSConfig after")
	assert.Equal(t, "cert.example.com", reparsed.TLSConfig.ServerName,
		"the certificate is checked under the name the caller chose, not the host")
	assert.False(t, reparsed.TLSConfig.InsecureSkipVerify, "InsecureSkipVerify after")
	assert.Nil(t, reparsed.TLSConfig.VerifyPeerCertificate, "VerifyPeerCertificate after")
}

// TestConfigURLDropsACertificateItNoLongerNames covers the other half: a
// parameter that has stopped being true rather than merely stopped being the
// whole truth. Writing one of these would hand whoever reads the URL an anchor
// the connection does not use, with nothing to give it away, so the URL says
// nothing and the reader chains to system roots - see
// TestConfigURLCannotCarryAPoolTheCallerBuilt for what that costs.
func TestConfigURLDropsACertificateItNoLongerNames(t *testing.T) {
	certificateFile, _ := newSelfSignedCert(t)

	_, otherDER := newSelfSignedCert(t)
	otherCertificate, err := x509.ParseCertificate(otherDER)
	require.NoError(t, err, "parsing the generated certificate")
	otherRoots := x509.NewCertPool()
	otherRoots.AddCert(otherCertificate)

	tests := []struct {
		name string
		dsn  string
		edit func(*Config)
	}{
		{
			// The pool comparison is the whole point, and a callback must not
			// answer before it runs.
			name: "a pool the caller swapped, under a check of their own",
			dsn:  "server=host.example.com;encrypt=true;certificate=" + certificateFile,
			edit: func(c *Config) {
				c.TLSConfig.RootCAs = otherRoots
				c.TLSConfig.VerifyConnection = func(tls.ConnectionState) error { return nil }
			},
		},
		{
			// setupTLSCommonName only runs for a name with a colon in it, so
			// once the name loses its colon the callback cannot have come from
			// this file either.
			name: "the common name callback under a name SetupTLS would not use it for",
			dsn:  "server=host.example.com;encrypt=true;certificate=" + certificateFile + ";hostnameincertificate=a:b",
			edit: func(c *Config) { c.TLSConfig.ServerName = "plain.example.com" },
		},
		{
			// A pool sits unread with verification off, so the file names
			// nothing the connection is enforcing.
			name: "a pool no chain is built against",
			dsn:  "server=host.example.com;encrypt=true;certificate=" + certificateFile,
			edit: func(c *Config) { c.TLSConfig.InsecureSkipVerify = true },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := Parse(tt.dsn)
			require.NoError(t, err, "parsing %q", tt.dsn)
			require.NotEmpty(t, config.Parameters[Certificate], "the connection string supplied certificate")
			tt.edit(&config)

			u := config.URL()
			assert.NotContains(t, u.Query(), Certificate,
				"certificate still names roots this Config no longer builds a chain against")

			reparsed, err := Parse(u.String())
			require.NoError(t, err, "reparsing %q", u.String())
			require.NotNil(t, reparsed.TLSConfig, "TLSConfig after")
			assert.Nil(t, reparsed.TLSConfig.RootCAs, "RootCAs after")
			assert.Nil(t, reparsed.TLSConfig.VerifyPeerCertificate, "VerifyPeerCertificate after")
			assert.Nil(t, reparsed.TLSConfig.VerifyConnection, "VerifyConnection after")
			assert.False(t, reparsed.TLSConfig.InsecureSkipVerify,
				"InsecureSkipVerify after: dropping the parameter must leave ordinary verification, not plain trust")
		})
	}
}

// TestConfigURLKeepsAPinBesideAPoolTheHandshakeIgnores covers a
// servercertificate pin on a tls.Config the caller has also left a root pool on.
// setupTLSServerCertificateOnly turns InsecureSkipVerify on so that the pin
// decides alone, and crypto/tls builds no chain in that state, so the pool is
// read by nothing and the connection accepts exactly the pinned certificate
// either way.
//
// Dropping the pin over it would be the sharpest downgrade this file guards
// against: the URL would come back verifying against system roots, which accepts
// every certificate a public CA has issued for the host and not just the one
// byte for byte. That is why RootCAs is not part of the condition that keeps the
// pin, and it is the one place a pool being present is not evidence of anything.
func TestConfigURLKeepsAPinBesideAPoolTheHandshakeIgnores(t *testing.T) {
	pinFile, der := newSelfSignedCert(t)

	_, otherDER := newSelfSignedCert(t)
	otherCertificate, err := x509.ParseCertificate(otherDER)
	require.NoError(t, err, "parsing the generated certificate")
	otherRoots := x509.NewCertPool()
	otherRoots.AddCert(otherCertificate)

	config, err := Parse("server=host.example.com;encrypt=true;servercertificate=" + pinFile)
	require.NoError(t, err, "parsing")
	require.NotNil(t, config.TLSConfig, "TLSConfig")
	require.True(t, config.TLSConfig.InsecureSkipVerify, "the pin runs with ordinary verification off")
	require.Nil(t, config.TLSConfig.RootCAs, "SetupTLS leaves no pool on the pin path")
	config.TLSConfig.RootCAs = otherRoots

	u := config.URL()
	assert.Contains(t, u.Query(), ServerCertificate,
		"the pin must survive a pool the handshake never reads")

	reparsed, err := Parse(u.String())
	require.NoError(t, err, "reparsing %q", u.String())
	require.NotNil(t, reparsed.TLSConfig, "TLSConfig after")
	assert.Nil(t, reparsed.TLSConfig.RootCAs, "RootCAs after: it was never part of the policy")
	assert.True(t, reparsed.TLSConfig.InsecureSkipVerify, "InsecureSkipVerify after")

	verify := reparsed.TLSConfig.VerifyPeerCertificate
	require.NotNil(t, verify, "the pin came back")
	assert.NoError(t, verify([][]byte{der}, nil), "the pinned certificate is still accepted")
	assert.Error(t, verify([][]byte{otherDER}, nil), "another certificate is still rejected")
}

// TestConfigURLCannotTellACommonNameCallbackApart records the one case the rule
// above cannot reach, so that it is a decision in the file rather than
// something found later.
//
// Go function values cannot be compared, so a callback a caller installs in
// place of the one setupTLSCommonName built - leaving the rest of the tls.Config
// exactly as SetupTLS left it - is indistinguishable from the original. The
// certificate behind it travels, and reparsing rebuilds SetupTLS's callback from
// the file, which is a different check from the caller's. Refusing to write the
// parameter whenever a VerifyConnection is present would close it, at the cost
// of every working common name round trip in
// TestConfigURLKeepsACommonNameCheckVerifying, since those are the same shape.
// servercertificate has had the same limit since the pin was added.
func TestConfigURLCannotTellACommonNameCallbackApart(t *testing.T) {
	certificateFile, _ := newSelfSignedCert(t)

	config, err := Parse("server=host.example.com;encrypt=true;certificate=" + certificateFile + ";hostnameincertificate=a:b")
	require.NoError(t, err, "parsing")
	require.NotNil(t, config.TLSConfig.VerifyConnection, "SetupTLS installed the common name check")

	config.TLSConfig.VerifyConnection = func(tls.ConnectionState) error {
		return errors.New("the caller rejects it")
	}

	u := config.URL()
	assert.Contains(t, u.Query(), Certificate, "documented limit: the file travels under the caller's callback")

	reparsed, err := Parse(u.String())
	require.NoError(t, err, "reparsing %q", u.String())
	require.NotNil(t, reparsed.TLSConfig, "TLSConfig after")
	assert.NotNil(t, reparsed.TLSConfig.VerifyConnection,
		"the check that comes back is SetupTLS's, rebuilt from the file, not the caller's")
}

// TestConfigURLLeavesAZeroValueTLSChoiceToTheReader covers a Config built by
// hand with no view on TLS at all: no tls.Config, TrustServerCertificate at its
// zero value, and no parameter behind it. Parse never produces that shape, since
// it builds a tls.Config whenever encryption is on, so nothing a connection
// string said is being lost, and for such a Config the zero value has always
// meant the parser's default rather than a choice.
//
// The Config here is the one this driver's own integration harness builds from
// HOST and DATABASE (GetConnParams in tds_test.go) before round-tripping it
// through URL(). Writing trustservercertificate=false for it made every
// AppVeyor job fail with "certificate signed by unknown authority", because the
// SQL Server there presents a self-signed certificate and the reparsed DSN
// verified it. A Config that does hold a view, a tls.Config here, still has that
// view written.
func TestConfigURLLeavesAZeroValueTLSChoiceToTheReader(t *testing.T) {
	harness := Config{
		Host:       "localhost",
		Instance:   "SQL2025",
		Database:   "test",
		User:       "sa",
		Password:   "p",
		LogFlags:   127,
		Parameters: map[string]string{},
		Encoding:   EncodeParameters{Timezone: time.UTC},
	}

	u := harness.URL()
	assert.NotContains(t, u.Query(), TrustServerCertificate, "nothing to say, so nothing is written")
	assert.NotContains(t, u.Query(), Encrypt, "encrypt")

	reparsed, err := Parse(u.String())
	require.NoError(t, err, "reparsing %q", u.String())
	assert.True(t, reparsed.TrustServerCertificate, "the parser's default for a DSN that names no encrypt, as on main")
	require.NotNil(t, reparsed.TLSConfig, "TLSConfig after")
	assert.True(t, reparsed.TLSConfig.InsecureSkipVerify, "InsecureSkipVerify after")

	t.Run("a tls.Config is a view and is written", func(t *testing.T) {
		withView := harness
		withView.TLSConfig = &tls.Config{}

		u := withView.URL()
		assert.Equal(t, "false", u.Query().Get(TrustServerCertificate), "a verifying tls.Config travels")

		reparsed, err := Parse(u.String())
		require.NoError(t, err, "reparsing %q", u.String())
		require.NotNil(t, reparsed.TLSConfig, "TLSConfig after")
		assert.False(t, reparsed.TLSConfig.InsecureSkipVerify, "InsecureSkipVerify after")
	})
}

// TestConfigURLDoesNotInventATrustingParameter covers the clause that keeps a
// trusting value out of the URL unless the connection string asked for it. A
// hand-built Config that trusts, with no parameter behind it, must not have that
// choice written into a DSN somebody else can read.
func TestConfigURLDoesNotInventATrustingParameter(t *testing.T) {
	config := Config{
		Host:                   "host.example.com",
		Encryption:             EncryptionRequired,
		TrustServerCertificate: true,
		TLSConfig:              &tls.Config{InsecureSkipVerify: true},
	}
	require.Nil(t, config.Parameters, "Parameters")

	u := config.URL()
	assert.Contains(t, u.Query(), Encrypt, "encrypt")
	assert.NotContains(t, u.Query(), TrustServerCertificate, "trustservercertificate")

	reparsed, err := Parse(u.String())
	require.NoError(t, err, "reparsing")
	assert.False(t, reparsed.TrustServerCertificate, "TrustServerCertificate")
	require.NotNil(t, reparsed.TLSConfig, "TLSConfig")
	assert.False(t, reparsed.TLSConfig.InsecureSkipVerify, "InsecureSkipVerify")
}

// TestConfigURLNeverWritesAKeyTwice guards the one failure mode that turns a
// redundant parameter into an unreadable DSN: splitConnectionStringURL rejects a
// duplicate key outright.
func TestConfigURLNeverWritesAKeyTwice(t *testing.T) {
	certFile, _ := newSelfSignedCert(t)

	config, err := Parse("server=host.example.com;database=db;encrypt=true;certificate=" + certFile +
		";fedauth=ActiveDirectoryDefault;applicationclientid=cid;authenticator=krb5;krb5-realm=EXAMPLE.COM" +
		";app name=a;workstation id=w;keepalive=9;connection timeout=7;packet size=8192" +
		";multisubnetfailover=false;notraceid=true;epa enabled=true;guid conversion=true;log=127" +
		";serverspn=MSSQLSvc/primary:1433;failoverpartner=mirror;failoverport=2000")
	require.NoError(t, err, "parsing")

	for key, values := range config.URL().Query() {
		assert.Len(t, values, 1, "URL() wrote %q more than once", key)
	}
}

// TestConfigURLNeverTurnsVerificationOff pins the asymmetry in how
// trustservercertificate is written. Config.TrustServerCertificate is not read
// at connect time - getTLSConn uses TLSConfig - so setting it cannot loosen a
// live connection, and serializing a Config must not be the thing that does.
func TestConfigURLNeverTurnsVerificationOff(t *testing.T) {
	t.Run("a field set to true does not loosen a verifying tls.Config", func(t *testing.T) {
		config, err := Parse("server=host.example.com;encrypt=true")
		require.NoError(t, err, "parsing")
		require.False(t, config.TLSConfig.InsecureSkipVerify, "InsecureSkipVerify before")

		config.TrustServerCertificate = true

		u := config.URL()
		assert.NotContains(t, u.Query(), TrustServerCertificate, "trustservercertificate")

		reparsed, err := Parse(u.String())
		require.NoError(t, err, "reparsing")
		require.NotNil(t, reparsed.TLSConfig, "TLSConfig")
		assert.False(t, reparsed.TLSConfig.InsecureSkipVerify, "InsecureSkipVerify after")
	})

	t.Run("a connection string that asked for it still round trips", func(t *testing.T) {
		_, after := roundTrip(t, "server=host.example.com;encrypt=true;trustservercertificate=true")
		assert.True(t, after.TrustServerCertificate, "TrustServerCertificate")
		require.NotNil(t, after.TLSConfig, "TLSConfig")
		assert.True(t, after.TLSConfig.InsecureSkipVerify, "InsecureSkipVerify")
	})

	t.Run("turning verification on travels either way it is expressed", func(t *testing.T) {
		for _, tighten := range []struct {
			name string
			edit func(*Config)
		}{
			{"via the field", func(c *Config) { c.TrustServerCertificate = false }},
			{"via the tls.Config", func(c *Config) { c.TLSConfig.InsecureSkipVerify = false }},
		} {
			t.Run(tighten.name, func(t *testing.T) {
				config, err := Parse("server=host.example.com")
				require.NoError(t, err, "parsing")
				require.True(t, config.TrustServerCertificate, "a connection string with no encrypt parameter starts trusting")

				tighten.edit(&config)

				reparsed, err := Parse(config.URL().String())
				require.NoError(t, err, "reparsing")
				assert.False(t, reparsed.TrustServerCertificate, "TrustServerCertificate")
				require.NotNil(t, reparsed.TLSConfig, "TLSConfig")
				assert.False(t, reparsed.TLSConfig.InsecureSkipVerify, "InsecureSkipVerify")
			})
		}
	})
}

// certificatePolicy is what a Config would actually do with the certificate the
// server presents. It reads the tls.Config the way getTLSConn and crypto/tls do
// rather than trusting any single field, because that is the only standard a
// serializer can be held to.
//
// Every part of it earns its place from a defect that got past a coarser
// version. Collapsing the anchors hid a round trip turning system roots into a
// private CA; ignoring chainChecked hid one turning "chain and a pin" into the
// pin alone, since crypto/tls runs VerifyPeerCertificate only after ordinary
// verification has already succeeded; and treating any non-nil pool as the same
// pool hid one swapping the roots out from under the file name.
type certificatePolicy struct {
	// chainChecked is ordinary chain and host name verification.
	chainChecked bool
	// pinned is a byte comparison against one certificate.
	pinned bool
	// nameChecked is the common name check SetupTLS installs for a name
	// containing a colon.
	nameChecked bool
	// identity is the name checked, where anything checks one.
	identity string
	// roots is what a chain is built against; nil means the system pool. It is
	// only read where a chain is built at all - with InsecureSkipVerify on,
	// crypto/tls builds none, and the callbacks SetupTLS installs check against
	// the certificate file or against system roots explicitly, so a pool left on
	// such a config decides nothing and is not part of the policy.
	roots *x509.CertPool
	// noTLS is encryption disabled, where no certificate is exchanged.
	noTLS bool
}

func policyOf(config Config) certificatePolicy {
	if config.Encryption == EncryptionDisabled {
		return certificatePolicy{noTLS: true}
	}
	// getTLSConn replaces a nil tls.Config with SetupTLS("", "", false, ...),
	// so a Config without one verifies against system roots under the host name.
	if config.TLSConfig == nil {
		return certificatePolicy{chainChecked: true, identity: config.Host}
	}

	policy := certificatePolicy{
		chainChecked: !config.TLSConfig.InsecureSkipVerify,
		pinned:       config.TLSConfig.VerifyPeerCertificate != nil,
		nameChecked:  config.TLSConfig.VerifyConnection != nil,
	}
	if policy.chainChecked {
		policy.roots = config.TLSConfig.RootCAs
	}
	if policy.chainChecked || policy.nameChecked {
		policy.identity = config.TLSConfig.ServerName
	}
	return policy
}

// checksSomething reports whether the policy rejects anything at all.
func (c certificatePolicy) checksSomething() bool {
	return c.noTLS || c.chainChecked || c.pinned || c.nameChecked
}

// expressible reports whether the connection string in hand produces this
// policy, given whether it named a certificate file at all - the common name
// check has no other source, so without one there is nothing to rebuild it
// from however right the shape looks.
//
// SetupTLS forks on the name it is checking against: a colon in it, with
// ordinary verification still on, routes the certificate through
// setupTLSCommonName, which installs the common name callback, turns
// verification off so that the callback can do it, and sets no pool. Anywhere
// else it builds a root pool and leaves both callbacks nil, and
// setupTLSServerCertificateOnly builds the pin the same way, on its own. A
// policy that mixes those - a common name check beside a chain, a pin or a pool,
// or under a name with no colon in it; a pin beside a chain; a chain to a named
// pool under a name with a colon - is not one any connection string asks for.
//
// Where an edit produces one, the URL has nothing faithful to say and has to
// describe the one thing it always can, which the caller of this then pins down.
func (c certificatePolicy) expressible(namesACertificate bool) bool {
	if c.noTLS {
		return true
	}
	colon := strings.Contains(c.identity, ":")
	if c.nameChecked {
		return namesACertificate && colon && !c.chainChecked && !c.pinned
	}
	if c.pinned {
		return !c.chainChecked
	}
	return !(colon && c.chainChecked && c.roots != nil)
}

func (c certificatePolicy) equals(other certificatePolicy) bool {
	if c.noTLS != other.noTLS ||
		c.chainChecked != other.chainChecked ||
		c.pinned != other.pinned ||
		c.nameChecked != other.nameChecked ||
		c.identity != other.identity {
		return false
	}
	switch {
	case c.roots == nil && other.roots == nil:
		return true
	case c.roots == nil || other.roots == nil:
		return false
	default:
		return c.roots.Equal(other.roots)
	}
}

func (c certificatePolicy) String() string {
	if c.noTLS {
		return "no certificate is exchanged"
	}
	checks := []string{}
	if c.chainChecked {
		roots := "system roots"
		if c.roots != nil {
			roots = "the configured roots"
		}
		checks = append(checks, "a chain to "+roots)
	}
	if c.nameChecked {
		checks = append(checks, "a common name check")
	}
	if c.pinned {
		checks = append(checks, "a byte comparison against one certificate")
	}
	if len(checks) == 0 {
		return "nothing: it accepts anything the server presents"
	}
	described := strings.Join(checks, " and ")
	if c.identity != "" {
		described += " under " + c.identity
	}
	return described
}

// TestConfigURLPreservesCertificatePolicy walks the whole space of TLS settings
// a connection string can carry, crossed with the edits a caller can make to the
// Config afterwards, and holds URL() to three properties:
//
//   - every URL it writes is one Parse can read back;
//   - a Config that checks the certificate never reparses into one that accepts
//     anything; and
//   - reparsing arrives at the same anchor and the same name, unless the edit is
//     one the connection string has no way to express, in which case the
//     fallback is named here rather than discovered later.
//
// Every defect this sweep was written for was a combination rather than a single
// setting: a pin next to a certificate name, a cleared tls.Config, a caller's own
// callback, an empty servercertificate. Comparing the policy rather than the
// parameters is what makes the combinations visible.
func TestConfigURLPreservesCertificatePolicy(t *testing.T) {
	t.Setenv("MSSQL_USE_EPA", "")
	certificateFile, _ := newSelfSignedCert(t)
	pinFile, _ := newSelfSignedCert(t)

	// A pool a caller might swap in, holding a certificate none of the DSNs
	// below name. Nothing in a file path says which certificates a pool holds,
	// so this is the case a serializer has to read the file back to see.
	_, otherDER := newSelfSignedCert(t)
	otherRoots := x509.NewCertPool()
	otherCertificate, err := x509.ParseCertificate(otherDER)
	require.NoError(t, err, "parsing the generated certificate")
	otherRoots.AddCert(otherCertificate)

	encryptions := []string{"", "encrypt=false", "encrypt=optional", "encrypt=true", "encrypt=strict", "encrypt=disable"}
	trusts := []string{"", "trustservercertificate=true", "trustservercertificate=false"}
	certificates := []string{"", "certificate=" + certificateFile, "servercertificate=" + pinFile, "servercertificate="}
	// A name containing a colon is the one that routes SetupTLS through
	// setupTLSCommonName instead of the root pool - a whole branch of the
	// certificate handling, and the only one that produces a VerifyConnection.
	names := []string{"", "hostnameincertificate=other.example.com", "hostnameincertificate=", "hostnameincertificate=a:b"}

	edits := []struct {
		name string
		edit func(*Config)
		// losesTheAnchor reports whether this edit left the connection trusting
		// something no connection string can name at all - not merely a policy
		// that cannot be written whole, which expressible() covers, but one
		// whose trust anchor has no spelling. Only then is an ordinary chain to
		// system roots the right thing for the URL to say; everywhere else the
		// rule below applies.
		losesTheAnchor func(edited certificatePolicy) bool
		// opaque marks the one edit whose result cannot be named at all: a
		// function the caller supplied, which might be stricter or looser than
		// anything the connection string could say. There the URL describes
		// what the connection string still says, and the only thing left to
		// require is that it says something.
		opaque bool
		// identityFollowsHost marks an edit that leaves the name in the
		// tls.Config out of step with the Config. The URL resolves it the way
		// connect() does, by following the host - but only when nothing said the
		// name was chosen rather than defaulted.
		identityFollowsHost bool
	}{
		{name: "none", edit: func(*Config) {}},
		{name: "TLSConfig cleared", edit: func(c *Config) { c.TLSConfig = nil }},
		{name: "TrustServerCertificate=true", edit: func(c *Config) { c.TrustServerCertificate = true }},
		{name: "TrustServerCertificate=false", edit: func(c *Config) { c.TrustServerCertificate = false }},
		{name: "InsecureSkipVerify=true", edit: func(c *Config) {
			if c.TLSConfig != nil {
				c.TLSConfig.InsecureSkipVerify = true
			}
		}},
		{
			// Turning ordinary verification back on beside a pin, or beside the
			// common name check, asks for both, which neither parameter can say:
			// expressible() rules the result out and the URL describes the chain
			// check on its own.
			name: "InsecureSkipVerify=false",
			edit: func(c *Config) {
				if c.TLSConfig != nil {
					c.TLSConfig.InsecureSkipVerify = false
				}
			},
			// A colon in the name sends SetupTLS to the common name callback
			// rather than the pool, so once verification is back on there is no
			// connection string that chains to this pool under this name.
			losesTheAnchor: func(edited certificatePolicy) bool {
				return edited.roots != nil && strings.Contains(edited.identity, ":")
			},
		},
		{
			// A callback of the caller's own has no connection string spelling,
			// so the URL describes ordinary verification instead: stricter than
			// accepting anything, and visibly different from silently keeping a
			// check that will not exist on the other side.
			name: "a verification callback of the caller's own",
			edit: func(c *Config) {
				if c.TLSConfig != nil {
					c.TLSConfig.VerifyPeerCertificate = func([][]byte, [][]*x509.Certificate) error {
						return errors.New("the caller's check")
					}
				}
			},
			opaque: true,
		},
		{name: "HostInCertificateProvided", edit: func(c *Config) { c.HostInCertificateProvided = true }},
		{
			// Documented on URL(): a ServerName set straight onto the tls.Config
			// does not travel unless HostInCertificateProvided says it was
			// chosen rather than defaulted, so the name falls back to the host.
			name: "ServerName",
			edit: func(c *Config) {
				if c.TLSConfig != nil {
					c.TLSConfig.ServerName = "cert.example.com"
				}
			},
			identityFollowsHost: true,
		},
		{
			// Moving the host leaves a stale ServerName behind, which is the
			// state failoverPartnerParams produces. connect() resolves it the
			// same way, by following the host.
			name:                "Host",
			edit:                func(c *Config) { c.Host = "other.example.com" },
			identityFollowsHost: true,
		},
		{name: "RootCAs cleared", edit: func(c *Config) {
			if c.TLSConfig != nil {
				c.TLSConfig.RootCAs = nil
			}
		}},
		{
			name: "RootCAs replaced with another pool",
			edit: func(c *Config) {
				if c.TLSConfig != nil {
					c.TLSConfig.RootCAs = otherRoots
				}
			},
			// A pool a caller built has no file behind it, so nothing in a
			// connection string can name it. Only where it is read at all: with
			// verification off crypto/tls builds no chain, so under a pin or the
			// common name check swapping the pool changes nothing and the anchor
			// those still name is untouched.
			losesTheAnchor: func(edited certificatePolicy) bool { return edited.roots != nil },
		},
		{name: "VerifyPeerCertificate cleared", edit: func(c *Config) {
			if c.TLSConfig != nil {
				c.TLSConfig.VerifyPeerCertificate = nil
			}
		}},
		{
			// The other callback, and the one the serializer used to read as
			// proof of where it came from. A VerifyConnection the connection
			// string never produced is the caller's own and has nowhere to go,
			// so certificate has to be dropped along with it - keeping it would
			// advertise the file as the whole of a policy the caller has just
			// added to. Where the connection string did produce one, replacing
			// it in place is indistinguishable from leaving it alone, and the
			// parameter travels; that limit is recorded on URL().
			name: "a common name check of the caller's own",
			edit: func(c *Config) {
				if c.TLSConfig != nil {
					c.TLSConfig.VerifyConnection = func(tls.ConnectionState) error {
						return errors.New("the caller's check")
					}
				}
			},
		},
		{
			// Dropping the check setupTLSCommonName installed leaves a config
			// that verifies nothing at all while certificate still names a file.
			name: "VerifyConnection cleared",
			edit: func(c *Config) {
				if c.TLSConfig != nil {
					c.TLSConfig.VerifyConnection = nil
				}
			},
		},
	}

	combinations := 0
	for _, encryption := range encryptions {
		for _, trust := range trusts {
			for _, certificate := range certificates {
				for _, name := range names {
					settings := []string{"server=host.example.com"}
					for _, setting := range []string{encryption, trust, certificate, name} {
						if setting != "" {
							settings = append(settings, setting)
						}
					}
					dsn := strings.Join(settings, ";")
					parsed, err := Parse(dsn)
					if err != nil {
						// Not a combination the parser accepts; nothing to serialize.
						continue
					}
					// What this connection string can still name, which is what
					// the URL is held to when a policy cannot be written whole.
					namesAPin := parsed.Parameters[ServerCertificate] != ""
					namesACertificate := parsed.Parameters[Certificate] != ""

					for _, edit := range edits {
						combinations++
						config, err := Parse(dsn)
						require.NoError(t, err, "parsing %q", dsn)
						edit.edit(&config)

						u := config.URL().String()
						before := policyOf(config)

						if namesAPin && before.pinned && before.chainChecked {
							// The one Config URL() writes so that Parse refuses
							// it. It accepts only a certificate that passes both
							// the chain check and the byte comparison, and
							// either half alone accepts certificates it rejects,
							// so the URL names both and parseTLS rejects the
							// pair. That is the only output that accepts nothing
							// this Config would not; see the note on URL().
							_, err := Parse(u)
							if assert.Error(t, err, "a chain beside a pin should be written so that Parse refuses it, not as %q: %q then %s", u, dsn, edit.name) {
								assert.Contains(t, err.Error(), "cannot specify both",
									"the refusal should name the pair: %q then %s", dsn, edit.name)
							}
							continue
						}

						reparsed, err := Parse(u)
						if !assert.NoError(t, err, "%q then %s produced a URL Parse rejects: %q", dsn, edit.name, u) {
							continue
						}

						after := policyOf(reparsed)
						where := fmt.Sprintf("%q then %s, across %q", dsn, edit.name, u)

						// The property that holds whatever else does: checking
						// something never becomes checking nothing.
						if before.checksSomething() {
							assert.True(t, after.checksSomething(),
								"a config that checked the certificate now accepts anything: %s", where)
						}

						switch {
						case !before.checksSomething():
							// Only the edit itself can tighten this, and
							// tightening is not a defect.
						case edit.opaque:
							// Already covered by the property above.
						case !before.noTLS && (!before.expressible(namesACertificate) ||
							(edit.losesTheAnchor != nil && edit.losesTheAnchor(before))):
							// A policy no connection string produces has to give
							// something up. What it must not give up is the
							// anchor: a pin names one certificate and a private
							// pool names one CA, while a chain to system roots
							// accepts every certificate a public CA has issued
							// for the host. Falling to system roots while the
							// connection string still names something narrower
							// accepts more than the Config ever did, so the rule
							// is that the narrowest anchor still nameable has to
							// survive - only an anchor with no spelling at all
							// leaves the ordinary chain as the answer.
							switch {
							case edit.losesTheAnchor != nil && edit.losesTheAnchor(before):
								// The chain falls back to system roots, which is
								// as narrow as it can be made. Whatever else
								// survives alongside it only adds a check, and
								// the property above still requires one.
								assert.True(t, after.roots == nil,
									"an anchor nothing can name should leave the chain to system roots, not to %s: %s",
									after, where)
							case namesAPin && before.pinned:
								assert.True(t, after.pinned,
									"the pin is the narrowest thing the connection string names, and it went missing: %s", where)
							case namesACertificate && before.roots != nil:
								// Only a pool is an anchor narrower than the
								// system pool. The common name check is not one:
								// setupTLSCommonName passes Roots: nil and uses
								// the file as intermediates, so it chains to
								// system roots too and only adds a name to
								// match.
								assert.True(t, after.roots != nil && after.roots.Equal(before.roots),
									"the certificate file still names the anchor, and the round trip fell to system roots: %s", where)
							default:
								assert.True(t, after.chainChecked && !after.pinned && !after.nameChecked && after.roots == nil,
									"nothing named an anchor, so an ordinary chain to system roots is what is left, not %s: %s",
									after, where)
							}
						default:
							want := before
							if edit.identityFollowsHost && !config.HostInCertificateProvided &&
								(want.chainChecked || want.nameChecked) {
								// Nothing marked the name as chosen, so it is
								// the host that travels.
								want.identity = config.Host
							}
							assert.True(t, want.equals(after),
								"what the certificate is checked against changed: it was %s, it is now %s: %s",
								want, after, where)
						}
					}
				}
			}
		}
	}

	// Guards the sweep itself: a filter mistake would quietly shrink it.
	assert.Greater(t, combinations, 3500, "the sweep covered fewer combinations than expected")
}

// TestConfigURLWritesThePacketSizeTheConnectionWouldUse covers a Config whose
// PacketSize did not come from Parse, which is the only way a value outside the
// TDS range reaches URL(): Parse clamps on the way in. Both the login path and
// Parse clamp to the same range, so writing the raw value would come back as
// the clamped one anyway - and dropping it would come back as the driver's
// 4096 default instead, which is a different packet size from the one this
// Config would have connected with.
func TestConfigURLWritesThePacketSizeTheConnectionWouldUse(t *testing.T) {
	tests := []struct {
		name string
		size uint16
		want uint16
	}{
		{"below the range", 100, 512},
		{"at the floor", 512, 512},
		{"in the range", 8192, 8192},
		{"at the ceiling", 32767, 32767},
		{"above the range", 40000, 32767},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := Config{Host: "host.example.com", PacketSize: tt.size}
			u := config.URL()
			// Parse clamps on the way back in, so a reparsed Config cannot tell
			// the written value from the raw one. What the URL says is the only
			// place the difference shows, and it is the part a person reads.
			assert.Equal(t, strconv.FormatUint(uint64(tt.want), 10), u.Query().Get(PacketSize),
				"the URL should name the size the connection would use")

			reparsed, err := Parse(u.String())
			require.NoError(t, err, "reparsing")
			assert.Equal(t, tt.want, reparsed.PacketSize, "PacketSize")
		})
	}

	t.Run("zero stays out, so the driver default applies", func(t *testing.T) {
		config := Config{Host: "host.example.com"}
		assert.NotContains(t, config.URL().Query(), PacketSize, "packet size")

		reparsed, err := Parse(config.URL().String())
		require.NoError(t, err, "reparsing")
		assert.Zero(t, reparsed.PacketSize, "PacketSize")
	})
}

// TestConfigURLKeepsAuthenticationSettingsWhenTheTLSConfigIsCleared covers a
// mistake that is easy to make while tightening the certificate rules: the
// settings carried for other packages have nothing to do with TLS, and dropping
// them because the tls.Config changed would pick a different authentication
// method. integratedauth falls back to its platform default when authenticator
// is missing, and azuread selects no federated workflow at all without fedauth.
func TestConfigURLKeepsAuthenticationSettingsWhenTheTLSConfigIsCleared(t *testing.T) {
	tests := []struct {
		name      string
		dsn       string
		parameter string
		want      string
	}{
		{"authenticator", "server=host.example.com;authenticator=krb5;krb5-realm=EXAMPLE.COM;krb5-configfile=/etc/krb5.conf", "authenticator", "krb5"},
		{"krb5-realm", "server=host.example.com;authenticator=krb5;krb5-realm=EXAMPLE.COM;krb5-configfile=/etc/krb5.conf", "krb5-realm", "EXAMPLE.COM"},
		{"fedauth", "sqlserver://host.example.com?fedauth=ActiveDirectoryDefault&applicationclientid=client-id", "fedauth", "ActiveDirectoryDefault"},
		{"applicationclientid", "sqlserver://host.example.com?fedauth=ActiveDirectoryDefault&applicationclientid=client-id", "applicationclientid", "client-id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := Parse(tt.dsn)
			require.NoError(t, err, "parsing")
			require.Equal(t, tt.want, config.Parameters[tt.parameter], "the parameter should have been parsed")

			// The caller drops back to ordinary certificate verification. That
			// says nothing about how they authenticate.
			config.TLSConfig = nil

			reparsed, err := Parse(config.URL().String())
			require.NoError(t, err, "reparsing")
			assert.Equal(t, tt.want, reparsed.Parameters[tt.parameter], "%q did not survive", tt.parameter)
		})
	}
}

// TestConfigURLDropsACertificateItCannotReadBack covers a file behind a
// certificate parameter that no longer yields the roots in use. URL() has no
// other way to tell whether the pool a Config carries came from that file, so a
// file it cannot turn back into a pool means the parameter no longer describes
// anything and is not written.
//
// Each case asserts what the reader is left with rather than only that the
// parameter went missing, because that is where the cost is. See
// TestConfigURLCannotCarryAPoolTheCallerBuilt: this is the honest outcome, not a
// safe one.
func TestConfigURLDropsACertificateItCannotReadBack(t *testing.T) {
	certificateFile, _ := newSelfSignedCert(t)

	config, err := Parse("server=host.example.com;encrypt=true;certificate=" + certificateFile)
	require.NoError(t, err, "parsing")
	require.NotNil(t, config.TLSConfig, "TLSConfig")
	require.NotNil(t, config.TLSConfig.RootCAs, "RootCAs")

	withCertificate := func(path string) Config {
		edited := config
		edited.Parameters = map[string]string{}
		for key, value := range config.Parameters {
			edited.Parameters[key] = value
		}
		edited.Parameters[Certificate] = path
		return edited
	}

	assertFallsBackToSystemRoots := func(t *testing.T, edited Config) {
		t.Helper()
		u := edited.URL()
		assert.NotContains(t, u.Query(), Certificate, "certificate")

		reparsed, err := Parse(u.String())
		require.NoError(t, err, "reparsing %q", u.String())
		require.NotNil(t, reparsed.TLSConfig, "TLSConfig after")
		assert.False(t, reparsed.TLSConfig.InsecureSkipVerify,
			"InsecureSkipVerify after: the chain check has to survive")
		assert.Nil(t, reparsed.TLSConfig.RootCAs,
			"RootCAs after: nothing names the private pool, so the chain is built to system roots")
	}

	t.Run("the file is gone", func(t *testing.T) {
		assertFallsBackToSystemRoots(t, withCertificate(certificateFile+".missing"))
	})

	t.Run("the file holds no usable certificate", func(t *testing.T) {
		empty, err := os.CreateTemp("", "*.pem")
		require.NoError(t, err, "creating an empty certificate file")
		t.Cleanup(func() { _ = os.Remove(empty.Name()) })
		require.NoError(t, empty.Close(), "closing it")

		assertFallsBackToSystemRoots(t, withCertificate(empty.Name()))
	})
}

// TestConfigURLCannotCarryAPoolTheCallerBuilt pins the cost of the rule above,
// because it is a real one and belongs in the file rather than left to be
// inferred from a parameter going missing.
//
// certificate names a file. A caller who replaces RootCAs with a pool they built
// has a trust anchor no connection string can name, so the URL says nothing
// about it and the reader chains to system roots. That is the honest thing URL()
// can write - a reader can see that no certificate was named, whereas writing
// the old file name would state something about this Config that is no longer
// true - but it is not the safe one. A private pool accepts less than the system
// pool does, so the round trip can end up accepting a publicly issued
// certificate for the host that this Config would have rejected.
//
// There is no third option inside the grammar: readCertificate takes a .pem or
// .der path and nothing else, so a pool with no file behind it cannot be written
// in any form.
func TestConfigURLCannotCarryAPoolTheCallerBuilt(t *testing.T) {
	certificateFile, _ := newSelfSignedCert(t)

	_, otherDER := newSelfSignedCert(t)
	otherCertificate, err := x509.ParseCertificate(otherDER)
	require.NoError(t, err, "parsing the generated certificate")
	privatePool := x509.NewCertPool()
	privatePool.AddCert(otherCertificate)

	config, err := Parse("server=host.example.com;encrypt=true;certificate=" + certificateFile)
	require.NoError(t, err, "parsing")
	config.TLSConfig.RootCAs = privatePool

	u := config.URL()
	assert.NotContains(t, u.Query(), Certificate,
		"the file no longer describes the roots in use and must not be written as if it did")

	reparsed, err := Parse(u.String())
	require.NoError(t, err, "reparsing %q", u.String())
	require.NotNil(t, reparsed.TLSConfig, "TLSConfig after")
	assert.False(t, reparsed.TLSConfig.InsecureSkipVerify, "InsecureSkipVerify after")
	assert.Nil(t, reparsed.TLSConfig.RootCAs,
		"RootCAs after: this is the widening the comment above describes, asserted so that it is visible")
}

// TestConfigURLNamesACertificateFileRatherThanItsContents records that a DSN is
// a reference and not a snapshot.
//
// setupTLSServerCertificateOnly reads the file at Parse time and captures the
// bytes in its callback. URL() can only write the path back, and Parse reads the
// file again, so if the file changed in between, the pin that comes back is the
// certificate in the file now rather than the one this Config holds. URL()
// cannot see the difference: the pinned bytes live in a closure, and nothing in
// the grammar carries a certificate inline.
//
// Following the file is the narrower of the two things it could do. Dropping the
// parameter would leave the reader verifying against system roots, which accepts
// far more than either certificate; and on the ordinary reason for a file to
// change - the certificate was rotated - the file holds what the operator now
// means. certificate differs only because its pool can be read back out of the
// tls.Config and compared, so a rotation there is indistinguishable from a
// caller swapping the pool and the parameter is dropped instead.
func TestConfigURLNamesACertificateFileRatherThanItsContents(t *testing.T) {
	pinFile, originalDER := newSelfSignedCert(t)
	_, rotatedDER := newSelfSignedCert(t)

	config, err := Parse("server=host.example.com;encrypt=true;servercertificate=" + pinFile)
	require.NoError(t, err, "parsing")
	pinned := config.TLSConfig.VerifyPeerCertificate
	require.NotNil(t, pinned, "the pin")
	require.NoError(t, pinned([][]byte{originalDER}, nil), "it pins what the file held")

	require.NoError(t,
		os.WriteFile(pinFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rotatedDER}), 0o600),
		"rotating the certificate behind the parameter")

	u := config.URL()
	assert.Contains(t, u.Query(), ServerCertificate, "the parameter still names the file")

	reparsed, err := Parse(u.String())
	require.NoError(t, err, "reparsing %q", u.String())
	rebuilt := reparsed.TLSConfig.VerifyPeerCertificate
	require.NotNil(t, rebuilt, "the pin came back")
	assert.NoError(t, rebuilt([][]byte{rotatedDER}, nil), "it pins what the file holds now")
	assert.Error(t, rebuilt([][]byte{originalDER}, nil), "and no longer what it held at Parse time")
}
