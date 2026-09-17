package msdsn

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

type (
	Encryption int
	Log        uint64
	BrowserMsg byte
)

const (
	DsnTypeURL  = 1
	DsnTypeOdbc = 2
	DsnTypeAdo  = 3
)

const (
	EncryptionOff      = 0
	EncryptionRequired = 1
	EncryptionDisabled = 3
	EncryptionStrict   = 4
)

const (
	LogErrors      Log = 1
	LogMessages    Log = 2
	LogRows        Log = 4
	LogSQL         Log = 8
	LogParams      Log = 16
	LogTransaction Log = 32
	LogDebug       Log = 64
	LogRetries     Log = 128
	// LogSessionIDs tells the session logger to include activity id and connection id
	LogSessionIDs Log = 0x8000
)

const (
	BrowserDefault      BrowserMsg = 0
	BrowserAllInstances BrowserMsg = 0x03
	BrowserDAC          BrowserMsg = 0x0f
)

const (
	Database               = "database"
	Encrypt                = "encrypt"
	Password               = "password"
	ChangePassword         = "change password"
	UserID                 = "user id"
	Port                   = "port"
	TrustServerCertificate = "trustservercertificate"
	Certificate            = "certificate"
	ServerCertificate      = "servercertificate"
	TLSMin                 = "tlsmin"
	PacketSize             = "packet size"
	LogParam               = "log"
	ConnectionTimeout      = "connection timeout"
	HostNameInCertificate  = "hostnameincertificate"
	KeepAlive              = "keepalive"
	ServerSpn              = "serverspn"
	WorkstationID          = "workstation id"
	AppName                = "app name"
	ApplicationIntent      = "applicationintent"
	FailoverPartner        = "failoverpartner"
	FailOverPort           = "failoverport"
	FailoverPartnerSpn     = "failoverpartnerspn"
	DisableRetry           = "disableretry"
	Server                 = "server"
	Protocol               = "protocol"
	DialTimeout            = "dial timeout"
	Pipe                   = "pipe"
	MultiSubnetFailover    = "multisubnetfailover"
	NoTraceID              = "notraceid"
	GuidConversion         = "guid conversion"
	Timezone               = "timezone"
	EpaEnabled             = "epa enabled"
)

// Defaults Parse applies when a connection string does not name the setting.
// URL() compares against the same values, so that it emits a parameter exactly
// when reparsing without it would produce something else.
const (
	defaultAppName   = "go-mssqldb"
	defaultKeepAlive = 30 * time.Second

	// The TDS packet size range Parse clamps to.
	minPacketSize = 512
	maxPacketSize = 32767
)

// defaultWorkstation is the workstation id Parse uses when the connection string
// does not carry one. It is the local host name, so it is not stable between
// machines; that is why a Config whose Workstation still matches it does not
// serialize the setting.
func defaultWorkstation() string {
	name, err := os.Hostname()
	if err != nil {
		return ""
	}
	return name
}

// epaEnabledFromEnvironment reports whether MSSQL_USE_EPA turns Extended
// Protection on in this process, and whether it says anything a reader could
// act on. Parse consults it only when the connection string does not carry an
// epa enabled parameter, which is why a serialized URL that leaves the
// parameter out does not mean a fixed value - and why a value Parse would
// reject is not a default to compare against either.
func epaEnabledFromEnvironment() (enabled bool, usable bool) {
	value := os.Getenv("MSSQL_USE_EPA")
	if value == "" {
		return false, true
	}
	enabled, err := strconv.ParseBool(value)
	return enabled, err == nil
}

type EncodeParameters struct {
	// Properly convert GUIDs, using correct byte endianness
	GuidConversion bool
	// Timezone is the timezone to use for encoding and decoding datetime values.
	Timezone *time.Location
}

func (e EncodeParameters) GetTimezone() *time.Location {
	if e.Timezone == nil {
		return time.UTC
	}
	return e.Timezone
}

type Config struct {
	Port       uint64
	Host       string
	Instance   string
	Database   string
	User       string
	Password   string
	Encryption Encryption
	TLSConfig  *tls.Config

	FailOverPartner    string
	FailOverPort       uint64
	FailOverPartnerSPN string

	// If true the TLSConfig servername should use the routed server.
	HostInCertificateProvided bool

	// Read Only intent for application database.
	// NOTE: This does not make queries to most databases read-only.
	ReadOnlyIntent bool

	LogFlags Log

	ServerSPN   string
	Workstation string
	AppName     string

	// If true disables database/sql's automatic retry of queries
	// that start on bad connections.
	DisableRetry bool

	// Do not use the following.

	DialTimeout time.Duration // DialTimeout defaults to 15s per protocol. Set negative to disable.
	ConnTimeout time.Duration // Use context for timeouts.
	KeepAlive   time.Duration // Leave at default.
	PacketSize  uint16

	Parameters map[string]string
	// Protocols is an ordered list of protocols to dial
	Protocols []string
	// ProtocolParameters are written by non-tcp ProtocolParser implementations
	ProtocolParameters map[string]interface{}
	// BrowserMsg is the message identifier to fetch instance data from SQL browser
	BrowserMessage BrowserMsg
	// ChangePassword is used to set the login's password during login. Ignored for non-SQL authentication.
	ChangePassword string
	//ColumnEncryption is true if the application needs to decrypt or encrypt Always Encrypted values
	ColumnEncryption bool
	// Attempt to connect to all IPs in parallel when MultiSubnetFailover is true
	MultiSubnetFailover bool
	// guid to set as Activity Id in the prelogin packet. Defaults to a new value for each Config.
	ActivityID []byte
	// When true, no connection id or trace id value is sent in the prelogin packet.
	// Some cloud servers may block connections that lack such values.
	NoTraceID bool
	// TrustServerCertificate controls whether the client verifies the server certificate.
	// When true, the server certificate is accepted without validation.
	TrustServerCertificate bool
	// Parameters related to type encoding
	Encoding EncodeParameters
	// EPA mode determines how the Channel Bindings are calculated.
	EpaEnabled bool
}

func readDERFile(filename string) ([]byte, error) {
	derBytes, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	cert, err := x509.ParseCertificate(derBytes)
	if err != nil {
		return nil, err
	}

	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	})
	return pemBytes, nil
}

func readCertificate(certificate string) ([]byte, error) {
	certType := strings.ToLower(filepath.Ext(certificate))

	switch certType {
	case ".pem":
		return os.ReadFile(certificate)
	case ".der":
		return readDERFile(certificate)
	default:
		return nil, fmt.Errorf("certificate type %s is not supported", certType)
	}
}

// Build a tls.Config object from the supplied certificate.
// serverCertificate is used for byte-comparison validation (skips chain validation and hostname validation)
// certificate is used for traditional chain validation
func SetupTLS(certificate string, serverCertificate string, insecureSkipVerify bool, hostInCertificate string, minTLSVersion string) (*tls.Config, error) {
	config := tls.Config{
		ServerName:         hostInCertificate,
		InsecureSkipVerify: insecureSkipVerify,

		// fix for https://github.com/microsoft/go-mssqldb/issues/166
		// Go implementation of TLS payload size heuristic algorithm splits single TDS package to multiple TCP segments,
		// while SQL Server seems to expect one TCP segment per encrypted TDS package.
		// Setting DynamicRecordSizingDisabled to true disables that algorithm and uses 16384 bytes per TLS package
		DynamicRecordSizingDisabled: true,
		MinVersion:                  TLSVersionFromString(minTLSVersion),
	}

	// Handle serverCertificate parameter (byte-comparison validation)
	if len(serverCertificate) > 0 {
		pem, err := readCertificate(serverCertificate)
		if err != nil {
			return nil, fmt.Errorf("cannot read server certificate %q: %w", serverCertificate, err)
		}
		if err := setupTLSServerCertificateOnly(&config, pem); err != nil {
			return nil, err
		}
		return &config, nil
	}

	// Handle certificate parameter (traditional chain validation)
	if len(certificate) == 0 {
		return &config, nil
	}
	pem, err := readCertificate(certificate)
	if err != nil {
		return nil, fmt.Errorf("cannot read certificate %q: %w", certificate, err)
	}

	if strings.Contains(config.ServerName, ":") && !insecureSkipVerify {
		err := setupTLSCommonName(&config, pem)
		if err != skipSetup {
			return &config, err
		}
	}
	certs := x509.NewCertPool()
	certs.AppendCertsFromPEM(pem)
	config.RootCAs = certs
	return &config, nil
}

// setupTLSServerCertificateOnly validates that the server certificate matches the provided certificate via byte comparison
// This matches the behavior of Microsoft.Data.SqlClient
func setupTLSServerCertificateOnly(config *tls.Config, pemData []byte) error {
	// To match the behavior of Microsoft.Data.SqlClient, we simply compare the raw bytes
	// of the server's certificate with the provided certificate file. This approach:
	// - Does not validate certificate chain, expiry, or subject
	// - Only checks that the server's certificate exactly matches the provided certificate
	// - Skips hostname validation (which is the intended behavior)
	//
	// We use InsecureSkipVerify=true with VerifyPeerCertificate callback because
	// VerifyConnection runs AFTER standard verification (including hostname check).

	// Parse the expected certificate from the PEM data
	block, _ := pem.Decode(pemData)
	if block == nil {
		return errors.New("failed to decode PEM certificate")
	}
	// Store the raw certificate bytes (DER format) for comparison
	expectedCertBytes := block.Bytes

	config.InsecureSkipVerify = true
	config.VerifyPeerCertificate = func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
		if len(rawCerts) == 0 {
			return errors.New("no peer certificates provided")
		}

		// Compare the server's certificate bytes with the expected certificate bytes
		// This matches the Microsoft.Data.SqlClient behavior: just compare raw bytes
		serverCertBytes := rawCerts[0]

		if !bytes.Equal(serverCertBytes, expectedCertBytes) {
			return errors.New("server certificate doesn't match the provided certificate")
		}

		return nil
	}
	return nil
}

// parseTLS parses encryption parameters and returns the TLS configuration and trustServerCertificate value.
func parseTLS(params map[string]string, host string) (Encryption, *tls.Config, bool, error) {
	trustServerCert := false

	var encryption Encryption = EncryptionOff
	encrypt, ok := params[Encrypt]
	if ok {
		encrypt = strings.ToLower(encrypt)
		switch encrypt {
		case "mandatory", "yes", "1", "t", "true":
			encryption = EncryptionRequired
		case "disable":
			encryption = EncryptionDisabled
		case "strict":
			encryption = EncryptionStrict
		case "optional", "no", "0", "f", "false":
			encryption = EncryptionOff
		default:
			f := "invalid encrypt '%s'"
			return encryption, nil, false, fmt.Errorf(f, encrypt)
		}
	} else {
		trustServerCert = true
	}
	trust, ok := params[TrustServerCertificate]
	if ok {
		var err error
		trustServerCert, err = strconv.ParseBool(trust)
		if err != nil {
			f := "invalid trust server certificate '%s': %s"
			return encryption, nil, false, fmt.Errorf(f, trust, err.Error())
		}
	}
	certificate := params[Certificate]
	serverCertificate := params[ServerCertificate]
	hostInCertificate := params[HostNameInCertificate]

	// Validate parameter combinations
	if len(serverCertificate) > 0 {
		if len(certificate) > 0 {
			return encryption, nil, false, errors.New("cannot specify both 'certificate' and 'serverCertificate' parameters")
		}
		if len(hostInCertificate) > 0 {
			return encryption, nil, false, errors.New("cannot specify both 'serverCertificate' and 'hostnameincertificate' parameters")
		}
	}

	if encryption != EncryptionDisabled {
		tlsMin := params[TLSMin]
		if encrypt == "strict" {
			trustServerCert = false
		}
		tlsConfig, err := SetupTLS(certificate, serverCertificate, trustServerCert, host, tlsMin)
		if err != nil {
			return encryption, nil, trustServerCert, fmt.Errorf("failed to setup TLS: %w", err)
		}
		return encryption, tlsConfig, trustServerCert, nil
	}
	return encryption, nil, trustServerCert, nil
}

var skipSetup = errors.New("skip setting up TLS")

func getDsnType(dsn string) int {
	if strings.HasPrefix(dsn, "sqlserver://") {
		return DsnTypeURL
	}
	if strings.HasPrefix(dsn, "odbc:") {
		return DsnTypeOdbc
	}
	return DsnTypeAdo
}

func getDsnParams(dsn string) (map[string]string, error) {

	var params map[string]string
	var err error

	switch getDsnType(dsn) {
	case DsnTypeOdbc:
		params, err = splitConnectionStringOdbc(dsn[len("odbc:"):])
		if err != nil {
			return params, err
		}
	case DsnTypeURL:
		params, err = splitConnectionStringURL(dsn)
		if err != nil {
			return params, err
		}
	default:
		params = splitConnectionString(dsn)
	}
	return params, nil
}

func Parse(dsn string) (Config, error) {
	p := Config{
		ProtocolParameters: map[string]interface{}{},
		Protocols:          []string{},
		Encoding: EncodeParameters{
			Timezone: time.UTC,
		},
	}

	activityid, uerr := uuid.NewRandom()
	if uerr == nil {
		p.ActivityID = activityid[:]
	}
	var params map[string]string
	var err error

	params, err = getDsnParams(dsn)
	if err != nil {
		return p, err
	}
	p.Parameters = params

	strlog, ok := params[LogParam]
	if ok {
		flags, err := strconv.ParseUint(strlog, 10, 64)
		if err != nil {
			return p, fmt.Errorf("invalid log parameter '%s': %s", strlog, err.Error())
		}
		p.LogFlags = Log(flags)
	}

	tz, ok := params[Timezone]
	if ok {
		location, err := time.LoadLocation(tz)
		if err != nil {
			return p, fmt.Errorf("invalid timezone '%s': %s", tz, err.Error())
		}
		p.Encoding.Timezone = location
	}

	p.Database = params[Database]
	p.User = params[UserID]
	p.Password = params[Password]
	p.ChangePassword = params[ChangePassword]
	p.Port = 0
	strport, ok := params[Port]
	if ok {
		var err error
		p.Port, err = strconv.ParseUint(strport, 10, 16)
		if err != nil {
			f := "invalid tcp port '%v': %v"
			return p, fmt.Errorf(f, strport, err.Error())
		}
	}

	// https://docs.microsoft.com/sql/database-engine/configure-windows/configure-the-network-packet-size-server-configuration-option
	strpsize, ok := params[PacketSize]
	if ok {
		var err error
		psize, err := strconv.ParseUint(strpsize, 0, 16)
		if err != nil {
			f := "invalid packet size '%v': %v"
			return p, fmt.Errorf(f, strpsize, err.Error())
		}

		// Ensure packet size falls within the TDS protocol range of 512 to 32767 bytes
		// NOTE: Encrypted connections have a maximum size of 16383 bytes.  If you request
		// a higher packet size, the server will respond with an ENVCHANGE request to
		// alter the packet size to 16383 bytes.
		p.PacketSize = uint16(psize)
		if p.PacketSize < minPacketSize {
			p.PacketSize = minPacketSize
		} else if p.PacketSize > maxPacketSize {
			p.PacketSize = maxPacketSize
		}
	}

	// https://msdn.microsoft.com/library/dd341108.aspx
	//
	// Do not set a connection timeout. Use Context to manage such things.
	// Default to zero, but still allow it to be set.
	if strconntimeout, ok := params[ConnectionTimeout]; ok {
		timeout, err := strconv.ParseUint(strconntimeout, 10, 64)
		if err != nil {
			f := "invalid connection timeout '%v': %v"
			return p, fmt.Errorf(f, strconntimeout, err.Error())
		}
		p.ConnTimeout = time.Duration(timeout) * time.Second
	}

	// default keep alive should be 30 seconds according to spec:
	// https://msdn.microsoft.com/library/dd341108.aspx
	p.KeepAlive = defaultKeepAlive
	if keepAlive, ok := params[KeepAlive]; ok {
		timeout, err := strconv.ParseUint(keepAlive, 10, 64)
		if err != nil {
			f := "invalid keepAlive value '%s': %s"
			return p, fmt.Errorf(f, keepAlive, err.Error())
		}
		p.KeepAlive = time.Duration(timeout) * time.Second
	}

	serverSPN, ok := params[ServerSpn]
	if ok {
		p.ServerSPN = serverSPN
	} // If not set by the app, ServerSPN will be set by the successful dialer.

	workstation, ok := params[WorkstationID]
	if ok {
		p.Workstation = workstation
	} else {
		p.Workstation = defaultWorkstation()
	}

	appname, ok := params[AppName]
	if !ok {
		appname = defaultAppName
	}
	p.AppName = appname

	appintent, ok := params[ApplicationIntent]
	if ok {
		if appintent == "ReadOnly" {
			if p.Database == "" {
				return p, fmt.Errorf("database must be specified when ApplicationIntent is ReadOnly")
			}
			p.ReadOnlyIntent = true
		}
	}

	failOverPartner, ok := params[FailoverPartner]
	if ok {
		p.FailOverPartner = failOverPartner
	}

	failOverPort, ok := params[FailOverPort]
	if ok {
		var err error
		p.FailOverPort, err = strconv.ParseUint(failOverPort, 0, 16)
		if err != nil {
			f := "invalid failover port '%v': %v"
			return p, fmt.Errorf(f, failOverPort, err.Error())
		}
	}

	failOverPartnerSPN, ok := params[FailoverPartnerSpn]
	if ok {
		p.FailOverPartnerSPN = failOverPartnerSPN
	}

	disableRetry, ok := params[DisableRetry]
	if ok {
		var err error
		p.DisableRetry, err = strconv.ParseBool(disableRetry)
		if err != nil {
			f := "invalid disableRetry '%s': %s"
			return p, fmt.Errorf(f, disableRetry, err.Error())
		}
	} else {
		p.DisableRetry = disableRetryDefault
	}

	server := params[Server]
	protocol, ok := params[Protocol]

	for _, parser := range ProtocolParsers {
		if (!ok && !parser.Hidden()) || parser.Protocol() == protocol {
			err = parser.ParseServer(server, &p)
			if err != nil {
				// if the caller only wants this protocol , fail right away
				if ok {
					return p, err
				}
			} else {
				// Only enable a protocol if it can handle the server name
				p.Protocols = append(p.Protocols, parser.Protocol())
			}

		}
	}
	if ok && len(p.Protocols) == 0 {
		return p, fmt.Errorf("No protocol handler is available for protocol: '%s'", protocol)
	}

	f := len(p.Protocols)
	if f == 0 {
		f = 1
	}
	p.DialTimeout = time.Duration(15*f) * time.Second
	if strdialtimeout, ok := params[DialTimeout]; ok {
		timeout, err := strconv.ParseUint(strdialtimeout, 10, 64)
		if err != nil {
			f := "invalid dial timeout '%v': %v"
			return p, fmt.Errorf(f, strdialtimeout, err.Error())
		}

		p.DialTimeout = time.Duration(timeout) * time.Second
	}

	hostInCertificate, ok := params[HostNameInCertificate]
	if ok {
		p.HostInCertificateProvided = true
	} else {
		hostInCertificate = p.Host
		p.HostInCertificateProvided = false
	}

	p.Encryption, p.TLSConfig, p.TrustServerCertificate, err = parseTLS(params, hostInCertificate)
	if err != nil {
		return p, err
	}

	if c, ok := params["columnencryption"]; ok {
		columnEncryption, err := strconv.ParseBool(c)
		if err != nil {
			if strings.EqualFold(c, "Enabled") {
				columnEncryption = true
			} else if strings.EqualFold(c, "Disabled") {
				columnEncryption = false
			} else {
				return p, fmt.Errorf("invalid columnencryption '%v' : %v", columnEncryption, err.Error())
			}
		}
		p.ColumnEncryption = columnEncryption
	}

	msf, ok := params[MultiSubnetFailover]
	if ok {
		multiSubnetFailover, err := strconv.ParseBool(msf)
		if err != nil {
			if strings.EqualFold(msf, "Enabled") {
				multiSubnetFailover = true
			} else if strings.EqualFold(msf, "Disabled") {
				multiSubnetFailover = false
			} else {
				return p, fmt.Errorf("invalid multiSubnetFailover value '%v': %v", multiSubnetFailover, err.Error())
			}
		}
		p.MultiSubnetFailover = multiSubnetFailover
	} else {
		// Defaulting to true to prevent breaking change although other client libraries default to false
		p.MultiSubnetFailover = true
	}
	nti, ok := params[NoTraceID]
	if ok {
		notraceid, err := strconv.ParseBool(nti)
		if err == nil {
			p.NoTraceID = notraceid
		}
	}

	guidConversion, ok := params[GuidConversion]
	if ok {
		var err error
		p.Encoding.GuidConversion, err = strconv.ParseBool(guidConversion)
		if err != nil {
			f := "invalid guid conversion '%s': %s"
			return p, fmt.Errorf(f, guidConversion, err.Error())
		}
	} else {
		// set to false for backward compatibility
		p.Encoding.GuidConversion = false
	}

	p.EpaEnabled = false
	epaString, ok := params[EpaEnabled]
	if !ok {
		epaString = os.Getenv("MSSQL_USE_EPA")
	}
	if epaString != "" {
		epaEnabled, err := strconv.ParseBool(epaString)
		if err != nil {
			return p, fmt.Errorf("invalid epa enabled value '%s': %v", epaString, err)
		}
		p.EpaEnabled = epaEnabled
	}

	return p, nil
}

// retainedCertificateParameterApplies reports whether a certificate parameter
// Parse retained still names something this Config enforces.
//
// It answers one question: is the parameter still a true statement about what
// the connection does with the certificate? Not whether it is the whole truth.
// A caller can add a check no connection string can spell, and the parameter is
// then incomplete rather than wrong; dropping it for that costs more than it
// saves, because nothing else in the grammar names a pin or a private CA and a
// URL without the parameter verifies against system roots - which accepts every
// certificate a public CA has issued for the host. Trading an incomplete truth
// for that is a wider connection, not a safer one.
//
// What does make it false is the anchor changing underneath it: a caller who
// cleared TLSConfig has dropped back to what getTLSConn builds, and one who
// swapped the root pool has left the file describing certificates the connection
// no longer trusts. Writing it then would hand whoever reads the URL an anchor
// this Config does not use, with nothing to give it away.
//
// It applies to certificate and servercertificate only. Every other name
// carriedVerbatim holds - fedauth, authenticator, the krb5- settings - has
// nothing to do with TLS, and dropping one of those because the tls.Config
// changed would silently pick a different authentication method.
//
// With encryption disabled no certificate is exchanged, so nothing here is in
// play and the retained text is kept as the only record the settings have.
func (p Config) retainedCertificateParameterApplies(name string) bool {
	if name != Certificate && name != ServerCertificate {
		return true
	}
	if p.Encryption == EncryptionDisabled {
		return true
	}
	if p.TLSConfig == nil {
		return false
	}

	if name == ServerCertificate {
		// crypto/tls calls VerifyPeerCertificate on every handshake, after any
		// chain check and whether or not verification is on, so while one is
		// there the file still names a certificate the connection compares
		// against. Nothing else on the config takes that away: a pool is read
		// only where a chain is built, a VerifyConnection is a second check
		// rather than a replacement, and verification turned back on adds the
		// chain rather than removing the pin.
		//
		// Requiring the rest of the shape setupTLSServerCertificateOnly builds
		// would drop the pin in each of those cases, and this parameter is the
		// only thing in the grammar that can name one. What it would be traded
		// for is a chain to system roots - the pin path sets no RootCAs - so the
		// URL would accept every certificate a public CA has issued for the host
		// in place of the single certificate named here.
		//
		// What a non-nil callback cannot say is whose it is. A caller who swapped
		// their own in over the pin and left the parameter behind is
		// indistinguishable from the pin, because Go function values cannot be
		// compared; the file travels and reparsing rebuilds the pin from it. That
		// is the limit URL() records for every callback, and dropping the
		// parameter instead would land on system roots, the wider answer.
		return p.TLSConfig.VerifyPeerCertificate != nil
	}

	// Unlike a pin, a root pool is read only where a chain is built against it:
	// with InsecureSkipVerify on, crypto/tls builds none, so the file names
	// nothing the connection enforces.
	//
	// The exception is the common name path. SetupTLS forks on the name it
	// checks against: a colon in it, with verification still on, routes the file
	// through setupTLSCommonName, which builds its callback out of that same
	// file and turns verification off so that the callback can do the checking.
	// There the file names the check even though no chain is built. A callback
	// the caller swapped in over that shape is indistinguishable from the one
	// SetupTLS built, because Go function values cannot be compared; that limit
	// is recorded on URL().
	if p.TLSConfig.VerifyConnection != nil &&
		p.TLSConfig.InsecureSkipVerify &&
		strings.Contains(p.TLSConfig.ServerName, ":") {
		return true
	}
	if p.TLSConfig.InsecureSkipVerify || p.TLSConfig.RootCAs == nil {
		return false
	}
	// A non-nil pool is not enough: a caller can swap in their own, and the file
	// name says nothing about what is in it. Read the file back and compare, so
	// that the parameter travels only while it still describes the roots in use.
	// This is the question a callback of the caller's own used to answer instead:
	// an extra check beside the pool is not a reason to stop believing the pool,
	// and the pool is what this file can still name.
	//
	// A file that cannot be read, or that holds nothing a pool will take, is
	// treated as no longer describing them. That is the honest direction rather
	// than the safe one; see the note on URL().
	pemBytes, err := readCertificate(p.Parameters[Certificate])
	if err != nil {
		return false
	}
	fromFile := x509.NewCertPool()
	if !fromFile.AppendCertsFromPEM(pemBytes) {
		return false
	}
	return p.TLSConfig.RootCAs.Equal(fromFile)
}

// trustsAnyCertificate reports whether this Config would accept whatever
// certificate the server presents. It mirrors what the connection actually
// does, because that is the only thing URL() may weaken.
//
// A nil TLSConfig is not "no opinion": getTLSConn replaces it with
// SetupTLS("", "", false, ...), so such a Config verifies. A verification
// callback counts as verification even alongside InsecureSkipVerify, which is
// the combination SetupTLS itself uses for a servercertificate pin and for a
// certificate whose common name contains a colon - and the combination a caller
// uses to install their own check. URL() cannot serialize a callback, so a
// Config carrying one must not be described as trusting: reparsing will verify
// against system roots instead, which may fail the handshake but will not
// accept anything.
//
// The field is read as well, though getTLSConn never reads it, and only as a
// veto: a Config is described as trusting only if the tls.Config trusts and the
// field agrees. A field set to false by hand over a trusting tls.Config is a
// request to verify that the connection itself would ignore, and honouring it
// here can only narrow what the reparsed Config accepts. The field can never
// grant trust on its own, because the tls.Config is what the handshake uses.
func (p Config) trustsAnyCertificate() bool {
	if p.Encryption == EncryptionDisabled {
		// No TLS is negotiated, so no certificate is seen either way.
		return p.TrustServerCertificate
	}
	return p.TrustServerCertificate &&
		p.TLSConfig != nil &&
		p.TLSConfig.InsecureSkipVerify &&
		p.TLSConfig.VerifyPeerCertificate == nil &&
		p.TLSConfig.VerifyConnection == nil
}

// tlsMinParameter renders a MinVersion the way tlsmin spells one. Zero is the
// tls package default and has no spelling. TLS 1.0 to 1.3 are written by name.
// A version above the highest name is written as its protocol number - 0x0305
// and so on - which tlsVersionFromNumber reads back, so that such a floor
// round-trips: on today's crypto/tls it is a Config no handshake can satisfy,
// and dropping it or rounding it down to a name would both come back as a
// Config that connects. A version below TLS 1.0 has no name and no number
// either, and is dropped; the default that comes back is stricter, not looser.
func tlsMinParameter(minVersion uint16) string {
	if name := tlsVersionToString(minVersion); name != "" {
		return name
	}
	if minVersion > highestNamedTLSVersion {
		return fmt.Sprintf("0x%04x", minVersion)
	}
	return ""
}

// tlsVersionFromNumber reads the spelling tlsMinParameter writes for a version
// above the highest one tlsmin has a name for: 0x followed by exactly four hex
// digits. It is the only spelling added to tlsmin, and it is that narrow on
// purpose. Below the bound a number is not read, because it would give a string
// that meant nothing before - and so the TLS 1.2 default - a meaning, and a
// looser one: crypto/tls keeps TLS 1.0 and 1.1 out only while MinVersion is
// zero, so 0x0300 read as a floor would let them back in on upgrade. Anything
// that is not exactly this spelling, or that names a version at or below the
// bound, reads as zero, which is what an unknown tlsmin has always meant.
func tlsVersionFromNumber(s string) uint16 {
	const prefix = "0x"
	if len(s) != len(prefix)+4 || !strings.HasPrefix(s, prefix) {
		return 0
	}
	v, err := strconv.ParseUint(s[len(prefix):], 16, 16)
	if err != nil || v <= highestNamedTLSVersion {
		return 0
	}
	return uint16(v)
}

// wholeSeconds renders a duration the way the connection string grammar spells
// one: a whole number of seconds, not negative. Parse reads these fields with
// strconv.ParseUint, so a negative or sub-second value has no spelling there and
// is reported as unrepresentable rather than rounded into a different setting.
func wholeSeconds(d time.Duration) (string, bool) {
	if d < 0 || d%time.Second != 0 {
		return "", false
	}
	return strconv.FormatInt(int64(d/time.Second), 10), true
}

// carriedVerbatim lists the parameters URL() has no Config field for but must
// still carry, so that a rebuilt connection string reaches the same certificate
// trust and selects the same authentication workflow. Most are read out of
// Config.Parameters by azuread and integratedauth rather than by this package.
//
// It is a list rather than a copy of everything Parse retained, because URL()
// cannot tell whether a parameter it does not recognise holds a secret.
// azuread reads systemtoken, clientassertion and userassertion out of the same
// map, and url.URL.Redacted() masks only the userinfo password, so copying
// every retained parameter would put those credentials into the string Go
// offers as the safe one to log. A parameter this list has not been told about
// is dropped instead, which loses a setting rather than publishing a secret.
// change password is left out for the same reason, and because a password
// change is a login-time operation that replaying a serialized DSN should not
// reissue.
var carriedVerbatim = map[string]bool{
	Certificate:       true,
	ServerCertificate: true,

	// azuread/configuration.go
	"additionallyallowedtenants": true,
	"applicationclientid":        true,
	"clientcertpath":             true,
	"disableinstancediscovery":   true,
	"fedauth":                    true,
	"resource id":                true,
	"sendcertificatechain":       true,
	"serviceconnectionid":        true,
	"tokenfilepath":              true,

	// integratedauth/auth.go and integratedauth/krb5/krb5.go
	"authenticator":           true,
	"krb5-configfile":         true,
	"krb5-credcachefile":      true,
	"krb5-dnslookupkdc":       true,
	"krb5-keytabfile":         true,
	"krb5-realm":              true,
	"krb5-udppreferencelimit": true,
}

// URL converts a Config back into a sqlserver:// connection string.
//
// A setting that has a field on Config is written from that field, whenever the
// field disagrees with the value reparsing the URL without it would produce. An
// edit to a Config therefore survives the round trip whether or not the
// connection string named the setting, and a setting left at its default adds
// nothing to the URL. Where the field holds something the connection string
// grammar cannot spell - a negative or sub-second timeout, a packet size
// outside the TDS range, read-only intent with no database - the parameter is
// dropped, because Parse would otherwise reject the URL or read back a
// different setting. disableretry predates the rule and is still written
// unconditionally; dial timeout is written for every value the grammar can
// spell, which is the same thing except for the negative and sub-second ones the
// rule drops.
//
// Five settings do not follow that rule, each for its own reason.
//
// encrypt: EncryptionOff is both the zero value and the result of an explicit
// encrypt=false or encrypt=optional, so writing it for every Config that never
// set encryption would turn a default into a choice. It is written only when
// the connection string supplied it, which costs nothing because EncryptionOff
// is also what a URL without the parameter reparses to.
//
// trustservercertificate: a verifying value follows the rule, but a trusting
// one is written only when the connection string asked for it, so that
// serializing a Config can never be the thing that turns verification off. A
// Config with no tls.Config, the field at its zero value and no parameter has
// no view on trust and gets nothing written, so it reparses to the parser's
// default as it always has. See the comment on its emission.
//
// hostnameincertificate: written from HostInCertificateProvided, not from
// ServerName differing from Host. A ServerName set straight onto the tls.Config
// does not travel unless HostInCertificateProvided is set as well.
//
// workstation id and epa enabled: their defaults are ambient rather than
// constant - the local host name and MSSQL_USE_EPA - so the parameter's
// presence counts as evidence alongside the field. For epa enabled that means a
// value the environment supplied is left for the reading environment to supply
// again; only an explicit choice, or one a caller set that the environment
// disagrees with, travels.
//
// change password has a field and is still never written, because the field
// holds a credential. See carriedVerbatim.
//
// A setting that has no field is carried only if carriedVerbatim names it. That
// covers certificate and servercertificate, which name a file, and the
// authentication settings azuread and integratedauth read out of Parameters for
// themselves. A parameter this method has not been told about is dropped rather
// than copied, because it cannot tell whether one holds a secret.
//
// One Config is written so that Parse refuses it: a servercertificate pin
// with ordinary verification left on, which accepts only a certificate that
// passes both the chain check and the byte comparison. No connection string
// produces that, and either half on its own accepts certificates the Config
// rejects, so the URL names both and parseTLS rejects the pair. See the
// hostnameincertificate emission.
//
// Server, port, user id and password travel in the URL's host and userinfo
// rather than its query. Anything held only in a programmatic tls.Config has no
// connection string spelling and does not survive: a verification callback, a
// cipher suite list, a MaxVersion. A Config restricted by one of those reparses
// as one that is not. A callback beside a pin or a pool leaves the pin or the
// pool in place, since the parameter is still true about what the connection
// checks; a callback a caller installs in place of the one SetupTLS built,
// leaving the rest of the tls.Config as SetupTLS left it, is indistinguishable
// from the original, because Go function values cannot be compared, and the
// parameter behind it travels. A MinVersion above the highest version tlsmin
// names is written as its protocol number, which Parse reads back, so such a
// floor round-trips; see tlsMinParameter for the bound and why it is there.
//
// Two things about certificate and servercertificate follow from their naming a
// file rather than holding one, and neither has an answer inside the grammar.
//
// A root pool with no file behind it cannot be written at all. Where a caller
// has replaced RootCAs with one they built, the URL says nothing and the reader
// chains to system roots, which is honest - it states nothing untrue about this
// Config - but not safe: the system pool accepts more than a private one, so the
// round trip can end up accepting a certificate this Config would reject. The
// alternative, writing the file name anyway, describes a trust anchor the
// connection has stopped using, which a reader has no way to notice.
//
// And a DSN is a reference, not a snapshot: Parse reads the file again, so what
// comes back is whatever the file holds then. A servercertificate pin therefore
// follows the file rather than the bytes captured when this Config was built.
//
// URL() reads the certificate file itself for the same reason, on every call, so
// a file that is locked, unreadable or on a network share that is briefly away
// takes the same path as one whose roots no longer match: the parameter is
// dropped and the reader chains to system roots. There is no error to return
// from here to say so.
//
// The output also depends on this process: it reads os.Hostname() and
// MSSQL_USE_EPA to decide what counts as a default.
func (p Config) URL() *url.URL {
	q := url.Values{}
	if p.Database != "" {
		q.Add(Database, p.Database)
	}
	if p.LogFlags != 0 {
		q.Add(LogParam, strconv.FormatUint(uint64(p.LogFlags), 10))
	}
	host := p.Host
	protocol := ""
	// Can't just check for a : because of IPv6 host names
	if strings.HasPrefix(host, "admin") || strings.HasPrefix(host, "np") || strings.HasPrefix(host, "sm") || strings.HasPrefix(host, "tcp") {
		hostParts := strings.SplitN(p.Host, ":", 2)
		if len(hostParts) > 1 {
			host = hostParts[1]
			protocol = hostParts[0]
		}
	}
	if p.Port > 0 {
		// Use net.JoinHostPort to properly handle IPv6 addresses (e.g., [::1]:1433)
		host = net.JoinHostPort(host, strconv.Itoa(int(p.Port)))
	}
	q.Add(DisableRetry, fmt.Sprintf("%t", p.DisableRetry))
	protocolParam, ok := p.Parameters[Protocol]
	if ok {
		if protocol != "" && protocolParam != protocol {
			panic("Mismatched protocol parameters!")
		}
		protocol = protocolParam
	}
	if protocol != "" {
		q.Add(Protocol, protocol)
	}
	pipe, ok := p.Parameters[Pipe]
	if ok {
		q.Add(Pipe, pipe)
	}
	res := url.URL{
		Scheme: "sqlserver",
		Host:   host,
		User:   url.UserPassword(p.User, p.Password),
	}
	if p.Instance != "" {
		res.Path = p.Instance
	}
	// DialTimeout is documented as negative to disable, and Parse reads it with
	// strconv.ParseUint, so a negative or sub-second value has no spelling here
	// either.
	if seconds, ok := wholeSeconds(p.DialTimeout); ok {
		q.Add(DialTimeout, seconds)
	}

	switch p.Encryption {
	case EncryptionDisabled:
		q.Add(Encrypt, "DISABLE")
	case EncryptionRequired:
		q.Add(Encrypt, "true")
	case EncryptionStrict:
		q.Add(Encrypt, "strict")
	case EncryptionOff:
		// EncryptionOff is both the zero value and the result of an explicit
		// encrypt=false or encrypt=optional. Emitting it for every Config that
		// never set encryption would turn a default into a choice, so it is
		// written only when the connection string supplied it. The half of that
		// distinction that matters for security is carried by
		// trustservercertificate below, which does not depend on the parameter
		// having been supplied.
		if _, ok := p.Parameters[Encrypt]; ok {
			q.Add(Encrypt, "false")
		}
	}
	// parseTLS starts from a trusting default when a connection string carries no
	// encrypt parameter and from a verifying one otherwise.
	//
	// What gets compared against that is trustsAnyCertificate below, which is
	// the runtime's own notion rather than the field's: Config.TrustServerCertificate
	// is not read at connect time, getTLSConn is, so serializing the field is
	// the only way setting it can reach a connection at all.
	//
	// A verifying value is written whenever it differs from what reparsing
	// would produce, so that turning verification on survives. A trusting one
	// is written only when the connection string asked for it, so that
	// serializing a Config can never be the thing that turns verification off.
	//
	// Strict encryption is left out: parseTLS forces the value to false there
	// whatever the parameter said, so writing one would advertise a trust
	// setting this driver ignores and another client might not.
	if p.Encryption != EncryptionStrict {
		_, encryptEmitted := q[Encrypt]
		trustedWhenReparsed := !encryptEmitted
		trusted := p.trustsAnyCertificate()
		_, trustSupplied := p.Parameters[TrustServerCertificate]
		// A Config with no tls.Config, the field at its zero value and no
		// parameter behind it has no view on trust at all. Parse never produces
		// that shape, since it builds a tls.Config whenever encryption is on, so
		// it only comes from a Config built by hand, and for those the zero value
		// has always meant the parser's default rather than a choice. Writing
		// trustservercertificate=false for it turned this driver's own
		// integration harness, which builds its Config that way from HOST and
		// DATABASE and round-trips it through here, into one that verified a
		// self-signed server. Say nothing and let the reader apply its default.
		hasView := p.TLSConfig != nil || p.TrustServerCertificate || trustSupplied
		if hasView && trusted != trustedWhenReparsed && (!trusted || trustSupplied) {
			q.Add(TrustServerCertificate, strconv.FormatBool(trusted))
		}
	}
	// certificate and servercertificate name a file rather than anything the
	// built tls.Config retains, so carriedVerbatim writes them instead.
	if p.TLSConfig != nil {
		// A zero MinVersion means the tls package default, which is what an
		// absent tlsmin produces, so a non-zero value is itself the evidence
		// that a floor was asked for.
		if tlsMin := tlsMinParameter(p.TLSConfig.MinVersion); tlsMin != "" {
			q.Add(TLSMin, tlsMin)
		}
		// ServerName falls back to the host when no name was supplied, so
		// HostInCertificateProvided is what tells a supplied name from that
		// fallback; an explicitly empty value counts as supplied.
		//
		// A ServerName that simply differs from Host is deliberately not taken
		// as evidence. The two also diverge when the caller moves Host and
		// leaves the tls.Config alone - failoverPartnerParams in the root
		// package does exactly that - and writing the old name there would pin
		// the certificate of a server we are no longer connecting to. It would
		// also set HostInCertificateProvided on the way back in, which is the
		// flag connect() reads to decide whether to retarget ServerName after a
		// routing redirect. A ServerName set straight onto the tls.Config is
		// therefore in the same bucket as a verification callback: set
		// HostInCertificateProvided as well if it should travel.
		//
		// A servercertificate pin that is written is the exception: parseTLS
		// rejects the two together, and the pin compares raw bytes rather than
		// checking a name, so there is nothing for a certificate name to say. It
		// has to be a pin that is actually written, not one the connection
		// string once named: a caller who removed the pin and chose a name has
		// a config that checks that name, and the stale parameter says nothing
		// about it.
		//
		// A pin beside ordinary verification is the one case that is written
		// so that Parse refuses it. crypto/tls runs the chain and host name
		// checks first and the pin after, so such a config accepts only a
		// certificate that passes both, and no connection string produces that:
		// the pin path turns verification off. Writing the pin alone would
		// accept a certificate the chain rejects, such as the self-signed one a
		// pin usually names; dropping the pin would accept every certificate a
		// public CA has issued for the host. Each half is a true statement
		// about this Config, so both are written and parseTLS rejects the pair,
		// which is the only answer that accepts nothing the Config would not.
		pinWritten := p.Parameters[ServerCertificate] != "" &&
			p.retainedCertificateParameterApplies(ServerCertificate)
		switch {
		case pinWritten && !p.TLSConfig.InsecureSkipVerify:
			name := p.TLSConfig.ServerName
			if name == "" {
				// crypto/tls refuses to verify against an empty name, so this
				// Config could not have connected; the host is what is left
				// to write, and Parse refuses the pair whatever the name is.
				name = p.Host
			}
			q.Add(HostNameInCertificate, name)
		case pinWritten:
		case p.HostInCertificateProvided:
			q.Add(HostNameInCertificate, p.TLSConfig.ServerName)
		}
	} else if p.Encryption == EncryptionDisabled {
		// No TLS is negotiated, so there is no tls.Config to read these out of
		// and the parsed text is the only record they have.
		if tlsMin, ok := p.Parameters[TLSMin]; ok {
			q.Add(TLSMin, tlsMin)
		}
		if hostInCertificate, ok := p.Parameters[HostNameInCertificate]; ok {
			q.Add(HostNameInCertificate, hostInCertificate)
		}
	}
	// Otherwise the caller cleared the tls.Config and these no longer describe
	// it; see retainedCertificateParameterApplies.
	if p.ColumnEncryption {
		q.Add("columnencryption", "true")
	}

	if p.Encoding.GuidConversion {
		q.Add(GuidConversion, strconv.FormatBool(p.Encoding.GuidConversion))
	}

	if tz := p.Encoding.Timezone; tz != nil && tz != time.UTC {
		q.Add(Timezone, tz.String())
	}

	if p.FailOverPartner != "" {
		q.Add(FailoverPartner, p.FailOverPartner)
	}
	if p.FailOverPort != 0 {
		q.Add(FailOverPort, strconv.FormatUint(p.FailOverPort, 10))
	}
	if p.ServerSPN != "" {
		q.Add(ServerSpn, p.ServerSPN)
	}
	if p.FailOverPartnerSPN != "" {
		q.Add(FailoverPartnerSpn, p.FailOverPartnerSPN)
	}

	// Login and connection settings, each written when the field disagrees with
	// the value reparsing this URL without it would produce. That is what makes
	// an edit to a Config win whether or not the connection string named the
	// setting, and it keeps a setting left at its default out of the URL.
	if p.ReadOnlyIntent && p.Database != "" {
		// Parse rejects ReadOnly without a database, so emitting it without one
		// would build a URL this package cannot read back.
		q.Add(ApplicationIntent, "ReadOnly")
	}
	if p.NoTraceID {
		q.Add(NoTraceID, "true")
	}
	if !p.MultiSubnetFailover {
		q.Add(MultiSubnetFailover, "false")
	}
	// epa enabled is the one setting whose absence does not mean a fixed value:
	// Parse falls back to MSSQL_USE_EPA. A connection string that never named it
	// was already asking whichever process reads it, so the round trip keeps
	// asking rather than freezing this process's answer into the URL. What has
	// to survive is the explicit choice - the thing the issue reports as lost -
	// and a value a caller set that this process's environment disagrees with,
	// which is why the comparison is against the environment rather than
	// against false. A value a caller set that happens to match the
	// environment is indistinguishable from the ambient default and is left to
	// the reader as well.
	_, epaSupplied := p.Parameters[EpaEnabled]
	epaAmbient, epaAmbientUsable := epaEnabledFromEnvironment()
	if epaSupplied || !epaAmbientUsable || p.EpaEnabled != epaAmbient {
		q.Add(EpaEnabled, strconv.FormatBool(p.EpaEnabled))
	}
	// Zero is the sentinel for the driver's own default rather than a size, so
	// it stays out. Anything else is written as the size the connection would
	// actually use: both Parse and the login path clamp to the TDS range, so
	// writing the raw value would come back as the clamped one anyway, and
	// dropping it would come back as the default instead.
	switch {
	case p.PacketSize == 0:
	case p.PacketSize < minPacketSize:
		q.Add(PacketSize, strconv.FormatUint(minPacketSize, 10))
	case p.PacketSize > maxPacketSize:
		q.Add(PacketSize, strconv.FormatUint(maxPacketSize, 10))
	default:
		q.Add(PacketSize, strconv.FormatUint(uint64(p.PacketSize), 10))
	}
	if p.ConnTimeout != 0 {
		if seconds, ok := wholeSeconds(p.ConnTimeout); ok {
			q.Add(ConnectionTimeout, seconds)
		}
	}
	if p.KeepAlive != defaultKeepAlive {
		if seconds, ok := wholeSeconds(p.KeepAlive); ok {
			q.Add(KeepAlive, seconds)
		}
	}
	if p.AppName != defaultAppName {
		q.Add(AppName, p.AppName)
	}
	// The workstation default is this machine's host name rather than a
	// constant, so a field that happens to match it is not evidence that nobody
	// asked for it. Without the presence test an explicit workstation id equal
	// to the local host name would become the reading machine's name instead.
	_, workstationSupplied := p.Parameters[WorkstationID]
	if workstationSupplied || p.Workstation != defaultWorkstation() {
		q.Add(WorkstationID, p.Workstation)
	}

	// The settings other packages read for themselves. See carriedVerbatim for
	// why this is a list and not a copy of Config.Parameters.
	for name := range carriedVerbatim {
		// Nothing above writes one of these names today. If something ever
		// does, splitConnectionStringURL rejects a duplicate key outright, so
		// skipping is the difference between a redundant parameter and a DSN
		// that cannot be read back.
		if _, written := q[name]; written {
			continue
		}
		if !p.retainedCertificateParameterApplies(name) {
			continue
		}
		if value, ok := p.Parameters[name]; ok {
			q.Add(name, value)
		}
	}

	if len(q) > 0 {
		res.RawQuery = q.Encode()
	}

	return &res
}

// adoSynonyms maps ADO.Net alternate keyword forms to this driver's canonical keys.
// See https://learn.microsoft.com/dotnet/api/microsoft.data.sqlclient.sqlconnection.connectionstring
var adoSynonyms = map[string]string{
	"app":                       AppName,
	"application name":          AppName,
	"data source":               Server,
	"address":                   Server,
	"network address":           Server,
	"addr":                      Server,
	"user":                      UserID,
	"uid":                       UserID,
	"pwd":                       Password,
	"initial catalog":           Database,
	"connect timeout":           ConnectionTimeout,
	"timeout":                   ConnectionTimeout,
	"failover partner":          FailoverPartner,
	"failover partner spn":      FailoverPartnerSpn,
	"application intent":        ApplicationIntent,
	"trust server certificate":  TrustServerCertificate,
	"multi subnet failover":     MultiSubnetFailover,
	"host name in certificate":  HostNameInCertificate,
	"server spn":                ServerSpn,
	"server certificate":        ServerCertificate,
	"wsid":                      WorkstationID,
	"column encryption setting": "columnencryption",
}

func splitConnectionString(dsn string) (res map[string]string) {
	res = map[string]string{}
	parts := splitAdoConnectionStringParts(dsn)
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		lst := strings.SplitN(part, "=", 2)
		name := strings.TrimSpace(strings.ToLower(lst[0]))
		if len(name) == 0 {
			continue
		}
		var value string = ""
		if len(lst) > 1 {
			value = strings.TrimSpace(lst[1])
			// Remove surrounding double quotes if present
			if len(value) >= 2 && strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") {
				value = value[1 : len(value)-1]
				// Unescape double quotes
				value = strings.ReplaceAll(value, "\"\"", "\"")
			}
		}
		synonym, hasSynonym := adoSynonyms[name]
		if hasSynonym {
			name = synonym
		}
		// "server" in ADO can include a protocol and a port.
		if name == Server {
			for _, parser := range ProtocolParsers {
				prot := parser.Protocol() + ":"
				if strings.HasPrefix(value, prot) {
					res[Protocol] = parser.Protocol()
				}
				value = strings.TrimPrefix(value, prot)
			}
			serverParts := strings.Split(value, ",")
			if len(serverParts) == 2 && len(serverParts[1]) > 0 {
				value = serverParts[0]
				res[Port] = serverParts[1]
			}
		}
		res[name] = value
	}
	return res
}

// splitAdoConnectionStringParts splits an ADO connection string into parts,
// properly handling double-quoted values that may contain semicolons
func splitAdoConnectionStringParts(dsn string) []string {
	var parts []string
	var current strings.Builder
	inQuotes := false

	runes := []rune(dsn)
	for i := 0; i < len(runes); i++ {
		char := runes[i]

		if char == '"' {
			if inQuotes && i+1 < len(runes) && runes[i+1] == '"' {
				// Double quote escape sequence - add both quotes to current part
				current.WriteRune(char)
				current.WriteRune(runes[i+1])
				i++ // Skip the next quote
			} else {
				// Start or end of quoted section
				inQuotes = !inQuotes
				current.WriteRune(char)
			}
		} else if char == ';' && !inQuotes {
			// Semicolon outside of quotes - end current part
			parts = append(parts, current.String())
			current.Reset()
		} else {
			current.WriteRune(char)
		}
	}

	// Add the last part if it's not empty
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
}

// Splits a URL of the form sqlserver://username:password@host/instance?param1=value&param2=value
func splitConnectionStringURL(dsn string) (map[string]string, error) {
	res := map[string]string{}

	u, err := url.Parse(dsn)
	if err != nil {
		// Do not include the original error which may contain credentials
		return res, fmt.Errorf("unable to parse connection string: invalid URL format")
	}

	if u.Scheme != "sqlserver" {
		return res, fmt.Errorf("scheme %s is not recognized", u.Scheme)
	}

	if u.User != nil {
		res[UserID] = u.User.Username()
		p, exists := u.User.Password()
		if exists {
			res[Password] = p
		}
	}

	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		host = u.Host
	}

	if len(u.Path) > 0 {
		res[Server] = host + "\\" + u.Path[1:]
	} else {
		res[Server] = host
	}

	if len(port) > 0 {
		res[Port] = port
	}

	query := u.Query()
	for k, v := range query {
		if len(v) > 1 {
			return res, fmt.Errorf("key %s provided more than once", k)
		}
		lk := strings.ToLower(k)
		if _, exists := res[lk]; exists {
			return res, fmt.Errorf("key %q provided more than once (connection string keys are case-insensitive; remove the duplicate)", k)
		}
		res[lk] = v[0]
	}

	return res, nil
}

// Splits a URL in the ODBC format
func splitConnectionStringOdbc(dsn string) (map[string]string, error) {
	res := map[string]string{}

	type parserState int
	const (
		// Before the start of a key
		parserStateBeforeKey parserState = iota

		// Inside a key
		parserStateKey

		// Beginning of a value. May be bare or braced
		parserStateBeginValue

		// Inside a bare value
		parserStateBareValue

		// Inside a braced value
		parserStateBracedValue

		// A closing brace inside a braced value.
		// May be the end of the value or an escaped closing brace, depending on the next character
		parserStateBracedValueClosingBrace

		// After a value. Next character should be a semicolon or whitespace.
		parserStateEndValue
	)

	var state = parserStateBeforeKey

	var key string
	var value string

	for i, c := range dsn {
		switch state {
		case parserStateBeforeKey:
			switch {
			case c == '=':
				return res, fmt.Errorf("unexpected character = at index %d. Expected start of key or semi-colon or whitespace", i)
			case !unicode.IsSpace(c) && c != ';':
				state = parserStateKey
				key += string(c)
			}

		case parserStateKey:
			switch c {
			case '=':
				key = normalizeOdbcKey(key)
				state = parserStateBeginValue

			case ';':
				// Key without value
				key = normalizeOdbcKey(key)
				res[key] = value
				key = ""
				value = ""
				state = parserStateBeforeKey

			default:
				key += string(c)
			}

		case parserStateBeginValue:
			switch {
			case c == '{':
				state = parserStateBracedValue
			case c == ';':
				// Empty value
				res[key] = value
				key = ""
				state = parserStateBeforeKey
			case unicode.IsSpace(c):
				// Ignore whitespace
			default:
				state = parserStateBareValue
				value += string(c)
			}

		case parserStateBareValue:
			if c == ';' {
				res[key] = strings.TrimRightFunc(value, unicode.IsSpace)
				key = ""
				value = ""
				state = parserStateBeforeKey
			} else {
				value += string(c)
			}

		case parserStateBracedValue:
			if c == '}' {
				state = parserStateBracedValueClosingBrace
			} else {
				value += string(c)
			}

		case parserStateBracedValueClosingBrace:
			if c == '}' {
				// Escaped closing brace
				value += string(c)
				state = parserStateBracedValue
				continue
			}

			// End of braced value
			res[key] = value
			key = ""
			value = ""

			// This character is the first character past the end,
			// so it needs to be parsed like the parserStateEndValue state.
			state = parserStateEndValue
			switch {
			case c == ';':
				state = parserStateBeforeKey
			case unicode.IsSpace(c):
				// Ignore whitespace
			default:
				return res, fmt.Errorf("unexpected character %c at index %d. Expected semi-colon or whitespace", c, i)
			}

		case parserStateEndValue:
			switch {
			case c == ';':
				state = parserStateBeforeKey
			case unicode.IsSpace(c):
				// Ignore whitespace
			default:
				return res, fmt.Errorf("unexpected character %c at index %d. Expected semi-colon or whitespace", c, i)
			}
		}
	}

	switch state {
	case parserStateBeforeKey: // Okay
	case parserStateKey: // Unfinished key. Treat as key without value.
		key = normalizeOdbcKey(key)
		res[key] = value
	case parserStateBeginValue: // Empty value
		res[key] = value
	case parserStateBareValue:
		res[key] = strings.TrimRightFunc(value, unicode.IsSpace)
	case parserStateBracedValue:
		return res, fmt.Errorf("unexpected end of braced value at index %d", len(dsn))
	case parserStateBracedValueClosingBrace: // End of braced value
		res[key] = value
	case parserStateEndValue: // Okay
	}

	return res, nil
}

// Normalizes the given string as an ODBC-format key
func normalizeOdbcKey(s string) string {
	return strings.ToLower(strings.TrimRightFunc(s, unicode.IsSpace))
}

// ProtocolParser can populate Config with parameters to dial using its protocol
type ProtocolParser interface {
	// ParseServer updates the Config with protocol properties from the server. Returns an error if the server isn't compatible.
	ParseServer(server string, p *Config) error
	// Protocol returns the name of the protocol dialer
	Protocol() string
	// Hidden returns true if this protocol must be explicitly chosen by the application
	Hidden() bool
}

// ProtocolParsers is an ordered list of protocols that can be dialed. Each parser must have a corresponding Dialer in mssql.ProtocolDialers
var ProtocolParsers []ProtocolParser = []ProtocolParser{
	tcpParser{Prefix: "tcp"},
	tcpParser{Prefix: "admin"},
}

type tcpParser struct {
	Prefix string
}

func (t tcpParser) Hidden() bool {
	return t.Prefix == "admin"
}

func (t tcpParser) ParseServer(server string, p *Config) error {
	// a server name can have different forms
	parts := strings.SplitN(server, `\`, 2)
	p.Host = parts[0]
	if p.Host == "." || strings.ToUpper(p.Host) == "(LOCAL)" || p.Host == "" {
		p.Host = "localhost"
	}
	if len(parts) > 1 {
		p.Instance = parts[1]
	}
	if t.Prefix == "admin" {
		if p.Instance == "" {
			p.Port = 1434
		}
		p.BrowserMessage = BrowserDAC
	} else {
		p.BrowserMessage = BrowserAllInstances
	}
	return nil
}

func (t tcpParser) Protocol() string {
	return t.Prefix
}
