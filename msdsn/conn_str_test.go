package msdsn

import (
	"crypto/tls"
	"encoding/hex"
	"io"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestInvalidConnectionString(t *testing.T) {
	connStrings := []string{
		"log=invalid",
		"port=invalid",
		"packet size=invalid",
		"connection timeout=invalid",
		"dial timeout=invalid",
		"keepalive=invalid",
		"encrypt=invalid",
		"trustservercertificate=invalid",
		"failoverport=invalid",
		"applicationintent=ReadOnly",
		"disableretry=invalid",
		"multisubnetfailover=invalid",
		"timezone=invalid",
		"vectortypesupport=invalid",
		"vectortypesupport=v2", // not yet supported
		"epa enabled=invalid",

		// ODBC mode
		"odbc:password={",
		"odbc:password={somepass",
		"odbc:password={somepass}}",
		"odbc:password={some}pass",
		"odbc:=", // unexpected =
		"odbc: =",
		"odbc:password={some} a",
		"odbc:timezone=invalid",

		// URL mode
		"sqlserver://\x00",
		"sqlserver://host?key=value1&key=value2", // duplicate keys
	}
	for _, connStr := range connStrings {
		_, err := Parse(connStr)
		if err == nil {
			t.Errorf("Connection expected to fail for connection string %s but it didn't", connStr)
			continue
		} else {
			t.Logf("Connection failed for %s as expected with error %v", connStr, err)
		}
	}
}

func TestValidConnectionString(t *testing.T) {
	type testStruct struct {
		connStr string
		check   func(Config) bool
	}
	connStrings := []testStruct{
		{"server=server\\instance;database=testdb;user id=tester;password=pwd", func(p Config) bool {
			return p.Host == "server" && p.Instance == "instance" && p.User == "tester" && p.Password == "pwd"
		}},
		{"server=.", func(p Config) bool { return p.Host == "localhost" && !p.ColumnEncryption }},
		{"server=(local)", func(p Config) bool { return p.Host == "localhost" }},
		{"ServerSPN=serverspn;Workstation ID=workstid", func(p Config) bool { return p.ServerSPN == "serverspn" && p.Workstation == "workstid" }},
		{"failoverpartner=fopartner;failoverport=2000", func(p Config) bool { return p.FailOverPartner == "fopartner" && p.FailOverPort == 2000 }},
		{"app name=appname;applicationintent=ReadOnly;database=testdb", func(p Config) bool { return p.AppName == "appname" && p.ReadOnlyIntent }},
		{"encrypt=disable", func(p Config) bool { return p.Encryption == EncryptionDisabled }},
		{"encrypt=disable;tlsmin=1.1", func(p Config) bool { return p.Encryption == EncryptionDisabled && p.TLSConfig == nil }},
		{"encrypt=true", func(p Config) bool { return p.Encryption == EncryptionRequired && p.TLSConfig.MinVersion == 0 }},
		{"encrypt=mandatory", func(p Config) bool { return p.Encryption == EncryptionRequired && p.TLSConfig.MinVersion == 0 }},
		{"encrypt=true;tlsmin=1.0", func(p Config) bool {
			return p.Encryption == EncryptionRequired && p.TLSConfig.MinVersion == tls.VersionTLS10
		}},
		{"encrypt=false;tlsmin=1.0", func(p Config) bool {
			return p.Encryption == EncryptionOff && p.TLSConfig.MinVersion == tls.VersionTLS10
		}},
		{"encrypt=true;tlsmin=1.1;column encryption setting=enabled", func(p Config) bool {
			return p.Encryption == EncryptionRequired && p.TLSConfig.MinVersion == tls.VersionTLS11 && p.ColumnEncryption
		}},
		{"encrypt=true;tlsmin=1.2", func(p Config) bool {
			return p.Encryption == EncryptionRequired && p.TLSConfig.MinVersion == tls.VersionTLS12
		}},
		{"encrypt=true;tlsmin=1.3", func(p Config) bool {
			return p.Encryption == EncryptionRequired && p.TLSConfig.MinVersion == tls.VersionTLS13
		}},
		{"encrypt=true;tlsmin=1.4", func(p Config) bool {
			return p.Encryption == EncryptionRequired && p.TLSConfig.MinVersion == 0
		}},
		{"encrypt=false", func(p Config) bool { return p.Encryption == EncryptionOff }},
		{"encrypt=optional", func(p Config) bool { return p.Encryption == EncryptionOff }},
		{"encrypt=strict", func(p Config) bool { return p.Encryption == EncryptionStrict }},
		{"connection timeout=3;dial timeout=4;keepalive=5", func(p Config) bool {
			return p.ConnTimeout == 3*time.Second && p.DialTimeout == 4*time.Second && p.KeepAlive == 5*time.Second
		}},
		{"log=63", func(p Config) bool { return p.LogFlags == 63 && p.Port == 0 }},
		{"log=63;port=1000", func(p Config) bool { return p.LogFlags == 63 && p.Port == 1000 }},
		{"log=64", func(p Config) bool { return p.LogFlags == 64 }},
		{"log=64;packet size=0", func(p Config) bool { return p.LogFlags == 64 && p.PacketSize == 512 }},
		{"log=64;packet size=300", func(p Config) bool { return p.LogFlags == 64 && p.PacketSize == 512 }},
		{"log=64;packet size=8192", func(p Config) bool { return p.LogFlags == 64 && p.PacketSize == 8192 }},
		{"log=64;packet size=48000", func(p Config) bool { return p.LogFlags == 64 && p.PacketSize == 32767 }},
		{"disableretry=true", func(p Config) bool { return p.DisableRetry }},
		{"disableretry=false", func(p Config) bool { return !p.DisableRetry }},
		{"disableretry=1", func(p Config) bool { return p.DisableRetry }},
		{"disableretry=0", func(p Config) bool { return !p.DisableRetry }},
		{"", func(p Config) bool { return p.DisableRetry == disableRetryDefault }},
		{"MultiSubnetFailover=true;NoTraceID=true", func(p Config) bool { return p.MultiSubnetFailover && p.NoTraceID }},
		{"MultiSubnetFailover=false", func(p Config) bool { return !p.MultiSubnetFailover }},
		{"timezone=Asia/Shanghai", func(p Config) bool { return p.Encoding.Timezone.String() == "Asia/Shanghai" }},
		{"Pwd=placeholder", func(p Config) bool { return p.Password == "placeholder" }},
		{"epa enabled=true", func(p Config) bool { return p.EpaEnabled }},
		{"epa enabled=false", func(p Config) bool { return !p.EpaEnabled }},
		{"epa enabled=1", func(p Config) bool { return p.EpaEnabled }},
		{"epa enabled=0", func(p Config) bool { return !p.EpaEnabled }},
		{"server=test;epa enabled=true", func(p Config) bool { return p.Host == "test" && p.EpaEnabled }},
		{"server=test;epa enabled=false", func(p Config) bool { return p.Host == "test" && !p.EpaEnabled }},

		// vectortypesupport tests (matches JDBC/ODBC pattern)
		{"vectortypesupport=off", func(p Config) bool { return p.VectorTypeSupport == VectorTypeSupportOff }},
		{"vectortypesupport=v1", func(p Config) bool { return p.VectorTypeSupport == VectorTypeSupportV1 }},
		{"vectortypesupport=OFF", func(p Config) bool { return p.VectorTypeSupport == VectorTypeSupportOff }},
		{"vectortypesupport=V1", func(p Config) bool { return p.VectorTypeSupport == VectorTypeSupportV1 }},
		{"vectortypesupport=0", func(p Config) bool { return p.VectorTypeSupport == VectorTypeSupportOff }},
		{"vectortypesupport=1", func(p Config) bool { return p.VectorTypeSupport == VectorTypeSupportV1 }},
		{"", func(p Config) bool { return p.VectorTypeSupport == VectorTypeSupportOff }}, // default is off
		{"server=test;vectortypesupport=v1", func(p Config) bool { return p.Host == "test" && p.VectorTypeSupport == VectorTypeSupportV1 }},

		// ADO connection string tests with double-quoted values containing semicolons
		{"server=test;password=\"pass;word\"", func(p Config) bool { return p.Host == "test" && p.Password == "pass;word" }},
		{"password=\"[2+R2B6O:fF/[;]cJsr\"", func(p Config) bool { return p.Password == "[2+R2B6O:fF/[;]cJsr" }},
		{"server=host;user id=user;password=\"complex;pass=word\"", func(p Config) bool {
			return p.Host == "host" && p.User == "user" && p.Password == "complex;pass=word"
		}},
		{"password=\"value with \"\"quotes\"\" inside\"", func(p Config) bool { return p.Password == "value with \"quotes\" inside" }},
		{"server=test;password=\"simple\"", func(p Config) bool { return p.Host == "test" && p.Password == "simple" }},
		// Test case specifically for the reported issue #282
		{"Server=tcp:sql.database.windows.net,1433;Initial Catalog=MyDatabase;User Id=testadmin@sql.database.windows.net;Password=\"[2+R2B6O:fF/[;]cJsr\"", func(p Config) bool {
			return p.Host == "sql.database.windows.net" && p.Database == "MyDatabase" && p.User == "testadmin@sql.database.windows.net" && p.Password == "[2+R2B6O:fF/[;]cJsr"
		}},
		// Additional edge cases for double-quoted values
		{"password=\"\"", func(p Config) bool { return p.Password == "" }},                                                                 // Empty quoted password
		{"password=\";\"", func(p Config) bool { return p.Password == ";" }},                                                               // Just a semicolon
		{"password=\";;\"", func(p Config) bool { return p.Password == ";;" }},                                                             // Multiple semicolons
		{"server=\"host;name\";password=\"pass;word\"", func(p Config) bool { return p.Host == "host;name" && p.Password == "pass;word" }}, // Multiple quoted values

		// Test cases with multibyte UTF-8 characters
		{"password=\"пароль;test\"", func(p Config) bool { return p.Password == "пароль;test" }},                                     // Cyrillic characters with semicolon
		{"server=\"服务器;name\";password=\"密码;word\"", func(p Config) bool { return p.Host == "服务器;name" && p.Password == "密码;word" }}, // Chinese characters
		{"password=\"🔐;secret;🗝️\"", func(p Config) bool { return p.Password == "🔐;secret;🗝️" }},                                     // Emoji characters with semicolons
		{"user id=\"用户名\";password=\"пароль\"", func(p Config) bool { return p.User == "用户名" && p.Password == "пароль" }},            // Mixed multibyte chars
		{"password=\"测试\"\"密码\"\"\"", func(p Config) bool { return p.Password == "测试\"密码\"" }},                                       // Chinese chars with escaped quotes
		{"password=\"café;naïve;résumé\"", func(p Config) bool { return p.Password == "café;naïve;résumé" }},                         // Accented characters

		// those are supported currently, but maybe should not be
		{"someparam", func(p Config) bool { return true }},
		{";;=;", func(p Config) bool { return true }},

		// ODBC mode
		{"odbc:server=somehost;user id=someuser;password=somepass", func(p Config) bool {
			return p.Host == "somehost" && p.User == "someuser" && p.Password == "somepass"
		}},
		{"odbc:server=somehost;user id=someuser;password=some{pass", func(p Config) bool {
			return p.Host == "somehost" && p.User == "someuser" && p.Password == "some{pass"
		}},
		{"odbc:server={somehost};user id={someuser};password={somepass}", func(p Config) bool {
			return p.Host == "somehost" && p.User == "someuser" && p.Password == "somepass"
		}},
		{"odbc:server={somehost};user id={someuser};password={some=pass}", func(p Config) bool {
			return p.Host == "somehost" && p.User == "someuser" && p.Password == "some=pass"
		}},
		{"odbc:server={somehost};user id={someuser};password={some;pass}", func(p Config) bool {
			return p.Host == "somehost" && p.User == "someuser" && p.Password == "some;pass"
		}},
		{"odbc:server={somehost};user id={someuser};password={some{pass}", func(p Config) bool {
			return p.Host == "somehost" && p.User == "someuser" && p.Password == "some{pass"
		}},
		{"odbc:server={somehost};user id={someuser};password={some}}pass}", func(p Config) bool {
			return p.Host == "somehost" && p.User == "someuser" && p.Password == "some}pass"
		}},
		{"odbc:server={somehost};user id={someuser};password={some{}}p=a;ss}", func(p Config) bool {
			return p.Host == "somehost" && p.User == "someuser" && p.Password == "some{}p=a;ss"
		}},
		{"odbc: server = somehost; user id =  someuser ; password = {some pass } ;", func(p Config) bool {
			return p.Host == "somehost" && p.User == "someuser" && p.Password == "some pass "
		}},
		{"odbc:password", func(p Config) bool {
			return p.Password == ""
		}},
		{"odbc:", func(p Config) bool {
			return true
		}},
		{"odbc:password=", func(p Config) bool {
			return p.Password == ""
		}},
		{"odbc:password;", func(p Config) bool {
			return p.Password == ""
		}},
		{"odbc:password=;", func(p Config) bool {
			return p.Password == ""
		}},
		{"odbc:password={value}  ", func(p Config) bool {
			return p.Password == "value"
		}},
		{"odbc:server=somehost;user id=someuser;password=somepass;disableretry=true", func(p Config) bool {
			return p.Host == "somehost" && p.User == "someuser" && p.Password == "somepass" && p.DisableRetry
		}},
		{"odbc:server=somehost;user id=someuser;password=somepass; disableretry =  1 ", func(p Config) bool {
			return p.Host == "somehost" && p.User == "someuser" && p.Password == "somepass" && p.DisableRetry
		}},
		{"odbc:timezone={Asia/Shanghai}", func(p Config) bool { return p.Encoding.Timezone.String() == "Asia/Shanghai" }},
		{"odbc:vectortypesupport=v1", func(p Config) bool { return p.VectorTypeSupport == VectorTypeSupportV1 }},
		{"odbc:vectortypesupport=off", func(p Config) bool { return p.VectorTypeSupport == VectorTypeSupportOff }},
		{"odbc:vectortypesupport={v1}", func(p Config) bool { return p.VectorTypeSupport == VectorTypeSupportV1 }},
		{"odbc:epa enabled=true", func(p Config) bool { return p.EpaEnabled }},
		{"odbc:epa enabled=false", func(p Config) bool { return !p.EpaEnabled }},
		{"odbc:server=somehost;epa enabled=1", func(p Config) bool { return p.Host == "somehost" && p.EpaEnabled }},
		{"odbc:server=somehost;epa enabled=0", func(p Config) bool { return p.Host == "somehost" && !p.EpaEnabled }},
		{"odbc:epa enabled={true}", func(p Config) bool { return p.EpaEnabled }},
		{"odbc:epa enabled={false}", func(p Config) bool { return !p.EpaEnabled }},

		// URL mode
		{"sqlserver://somehost?connection+timeout=30", func(p Config) bool {
			return p.Host == "somehost" && p.Port == 0 && p.Instance == "" && p.ConnTimeout == 30*time.Second
		}},
		{"sqlserver://someuser@somehost?connection+timeout=30", func(p Config) bool {
			return p.Host == "somehost" && p.Port == 0 && p.Instance == "" && p.User == "someuser" && p.Password == "" && p.ConnTimeout == 30*time.Second
		}},
		{"sqlserver://someuser:@somehost?connection+timeout=30", func(p Config) bool {
			return p.Host == "somehost" && p.Port == 0 && p.Instance == "" && p.User == "someuser" && p.Password == "" && p.ConnTimeout == 30*time.Second
		}},
		{"sqlserver://someuser:foo%3A%2F%5C%21~%40;bar@somehost?connection+timeout=30", func(p Config) bool {
			return p.Host == "somehost" && p.Port == 0 && p.Instance == "" && p.User == "someuser" && p.Password == "foo:/\\!~@;bar" && p.ConnTimeout == 30*time.Second
		}},
		{"sqlserver://someuser:foo%3A%2F%5C%21~%40;bar@somehost:1434?connection+timeout=30", func(p Config) bool {
			return p.Host == "somehost" && p.Port == 1434 && p.Instance == "" && p.User == "someuser" && p.Password == "foo:/\\!~@;bar" && p.ConnTimeout == 30*time.Second
		}},
		{"sqlserver://someuser:foo%3A%2F%5C%21~%40;bar@somehost:1434/someinstance?connection+timeout=30", func(p Config) bool {
			return p.Host == "somehost" && p.Port == 1434 && p.Instance == "someinstance" && p.User == "someuser" && p.Password == "foo:/\\!~@;bar" && p.ConnTimeout == 30*time.Second
		}},
		{"sqlserver://someuser@somehost?disableretry=true", func(p Config) bool {
			return p.Host == "somehost" && p.Port == 0 && p.Instance == "" && p.User == "someuser" && p.Password == "" && p.DisableRetry
		}},
		{"sqlserver://someuser@somehost?connection+timeout=30&disableretry=1", func(p Config) bool {
			return p.Host == "somehost" && p.Port == 0 && p.Instance == "" && p.User == "someuser" && p.Password == "" && p.ConnTimeout == 30*time.Second && p.DisableRetry && !p.ColumnEncryption
		}},
		{"sqlserver://somehost?encrypt=true&tlsmin=1.1&columnencryption=1", func(p Config) bool {
			return p.Host == "somehost" && p.Encryption == EncryptionRequired && p.TLSConfig.MinVersion == tls.VersionTLS11 && p.ColumnEncryption && !p.Encoding.GuidConversion
		}},
		{"sqlserver://somehost?encrypt=true&tlsmin=1.1&columnencryption=1&guid+conversion=true", func(p Config) bool {
			return p.Host == "somehost" && p.Encryption == EncryptionRequired && p.TLSConfig.MinVersion == tls.VersionTLS11 && p.ColumnEncryption && p.Encoding.GuidConversion
		}},
		{"sqlserver://someuser@somehost?timezone=Asia%2FShanghai", func(p Config) bool { return p.Encoding.Timezone.String() == "Asia/Shanghai" }},
		{"sqlserver://somehost?vectortypesupport=v1", func(p Config) bool { return p.Host == "somehost" && p.VectorTypeSupport == VectorTypeSupportV1 }},
		{"sqlserver://somehost?vectortypesupport=off", func(p Config) bool { return p.Host == "somehost" && p.VectorTypeSupport == VectorTypeSupportOff }},
		{"sqlserver://somehost?epa+enabled=true", func(p Config) bool { return p.Host == "somehost" && p.EpaEnabled }},
		{"sqlserver://somehost?epa+enabled=false", func(p Config) bool { return p.Host == "somehost" && !p.EpaEnabled }},
		{"sqlserver://somehost?epa+enabled=1", func(p Config) bool { return p.Host == "somehost" && p.EpaEnabled }},
		{"sqlserver://somehost?epa+enabled=0", func(p Config) bool { return p.Host == "somehost" && !p.EpaEnabled }},
		{"sqlserver://somehost?epa+enabled=true&encrypt=true", func(p Config) bool { return p.Host == "somehost" && p.EpaEnabled && p.Encryption == EncryptionRequired }},
	}
	for _, ts := range connStrings {
		p, err := Parse(ts.connStr)
		if err == nil {
			t.Logf("Connection string was parsed successfully %s", ts.connStr)
		} else {
			t.Errorf("Connection string %s failed to parse with error %s", ts.connStr, err)
			continue
		}

		if !ts.check(p) {
			t.Errorf("Check failed on conn str %s", ts.connStr)
		}
	}
}

func TestSplitConnectionStringURL(t *testing.T) {
	_, err := splitConnectionStringURL("http://bad")
	if err == nil {
		t.Error("Connection string with invalid scheme should fail to parse but it didn't")
	}
}

func TestConnParseRoundTripFixed(t *testing.T) {
	connStr := "sqlserver://sa:sa@localhost/sqlexpress?database=master&log=127&disableretry=true&dial+timeout=30"
	params, err := Parse(connStr)
	if err != nil {
		t.Fatal("Test URL is not valid", err)
	}
	rtParams, err := Parse(params.URL().String())
	if err != nil {
		t.Fatal("Params after roundtrip are not valid", err)
	}
	t.Log("params.URL " + params.URL().String())
	params.ActivityID = nil
	rtParams.ActivityID = nil
	if !reflect.DeepEqual(params, rtParams) {
		t.Fatal("Parameters do not match after roundtrip", params, rtParams)
	}
}

func TestServerNameInTLSConfig(t *testing.T) {
	var tests = []struct {
		dsn          string
		host         string
		hasTLSConfig bool
	}{
		{"sqlserver://someuser:somepass@somehost?TrustServerCertificate=false&encrypt=true", "somehost", true},
		{"sqlserver://someuser:somepass@somehost?TrustServerCertificate=false&encrypt=false", "somehost", true},
		{"sqlserver://someuser:somepass@somehost?TrustServerCertificate=false&encrypt=true&hostnameincertificate=someotherhost", "someotherhost", true},
		{"sqlserver://someuser:somepass@somehost?TrustServerCertificate=false", "somehost", true},
		{"sqlserver://someuser:somepass@somehost?TrustServerCertificate=false&encrypt=DISABLE", "", false},
		{"sqlserver://someuser:somepass@somehost?TrustServerCertificate=false&encrypt=DISABLE&hostnameincertificate=someotherhost", "", false},
		{"sqlserver://someuser:somepass@somehost?TrustServerCertificate=false&encrypt=false", "somehost", true},
	}
	for _, test := range tests {
		cfg, err := Parse(test.dsn)
		if err != nil {
			t.Errorf("Could not parse valid connection string %s: %v", test.dsn, err)
		}
		if !test.hasTLSConfig && cfg.TLSConfig != nil {
			t.Errorf("Expected empty TLS config, but got %v (cfg.Host was %s)", cfg.TLSConfig, cfg.Host)
		}
		if test.hasTLSConfig && cfg.TLSConfig.ServerName != test.host {
			t.Errorf("Expected somehost as TLS server, but got %s (cfg.Host was %s)", cfg.TLSConfig.ServerName, cfg.Host)
		}
	}
}
func TestAllKeysAreAvailableInParametersMap(t *testing.T) {
	keys := map[string]string{
		"user id":            "1",
		"testparam":          "testvalue",
		"password":           "test",
		"thisisanunknownkey": "thisisthevalue",
		"server":             "name",
	}

	connString := ""
	for key, val := range keys {
		connString += key + "=" + val + ";"
	}

	params, err := Parse(connString)
	if err != nil {
		t.Errorf("unexpected error while parsing, %v", err)
	}

	if params.Parameters == nil {
		t.Error("Expected parameters map to be instanciated, found nil")
		return
	}

	if len(params.Parameters) != len(keys) {
		t.Errorf("Expected parameters map to be same length as input map length, expected %v, found %v", len(keys), len(params.Parameters))
		return
	}

	for key, val := range keys {
		if params.Parameters[key] != val {
			t.Errorf("Expected parameters map to contain key %v and value %v, found %v", key, val, params.Parameters[key])
		}
	}
}

func TestReadCertificate(t *testing.T) {

	//Setup dummy certificate
	hexCertificate := "3082031830820200a00302010202103608db21691eccba415f8624d34b66fe300d06092a864886f70d01010b050030143112301006035504030c096c6f63616c686f7374301e170d3233303830383133343233375a170d3234303830383134303233375a30143112301006035504030c096c6f63616c686f737430820122300d06092a864886f70d01010105000382010f003082010a0282010100e18cd4d2923c548ac6e4fd731de116716a09fd2447feb28213810a1b508c22c108928f61531d31439b7252808d6bc6a71d50e5bb00596bbc1633d65389b80bb36f22d1546cbff570881331285cb458b3a2ad1ad0fa83081bd000f2793d29460a6adc0128a2d979d34f5cd91d60d4fef5932f393e04fcb3730a33693f3c44b882384c529f7489e58e296b0c17ca391b02f2488c38f8fc3c3afa0c1be0d22329287f93cf57ee46836a12f74de82eb54b18a5ae0134266db52633c0e33177f8ac4532045f053ddc920f0659cafa84c54c2b3cc92f4010c8af93ae0fc92e461d47c0cf2da46421189b2ddcf2f6ae17cb5ef6f1eda94452af6f714d583dcb7bcd43e90203010001a3663064300e0603551d0f0101ff0404030205a0301d0603551d250416301406082b0601050507030206082b0601050507030130140603551d11040d300b82096c6f63616c686f7374301d0603551d0e0416041443e3d9f187e9474d73794c641d54ecb810342ec6300d06092a864886f70d01010b05000382010100a227e721ac80838e66ef75d8ba080185dd8f4a5c84d7373e8ed50534100a490b577e3c1af593597303bdad8bb900e32b5d6f69941c19cc87fd426f9e4a4134f34f2ade02748d64031bc4e9c7617206a45c1d9556bb0488994cd27126adb029216f7c57852c1663983b7be638f1bc5411ba2221ce3fde29bf4818e36bec8ac25e9a37bfc41c5a3812829a6358a66c467818448346be140639957077b924b22567b75c7dab4d9d6794b4d79596d17446641684cbd193ec20a6faa85fb6b72f5f30dc57e8cd662b22152429e5b43ccb450c6840ba006e1c8e38b002aa97d8dd07e100ef76eebd9c523d8710636f060865e6198da620fedbf1ae6ed75df997641621"
	derfile, _ := os.CreateTemp("", "*.der")
	defer os.Remove(derfile.Name())
	certInBytes, _ := hex.DecodeString(hexCertificate)
	_, _ = derfile.Write(certInBytes)

	// Test with a valid certificate
	cert, err := readCertificate(derfile.Name())
	assert.Nil(t, err, "Expected no error while reading certificate, found %v", err)
	assert.NotNil(t, cert, "Expected certificate to be read, found nil")

	pemfile, _ := os.CreateTemp("", "*.pem")
	_, _ = io.Copy(derfile, pemfile)
	defer os.Remove(pemfile.Name())
	cert, err = readCertificate(pemfile.Name())
	assert.Nil(t, err, "Expected no error while reading certificate, found %v", err)
	assert.NotNil(t, cert, "Expected certificate to be read, found nil")

	// Test with an invalid certificate
	bakfile, _ := os.CreateTemp("", "*.bak")
	_, _ = io.Copy(derfile, bakfile)
	defer os.Remove(bakfile.Name())
	cert, err = readCertificate(bakfile.Name())
	assert.NotNil(t, err, "Expected error while reading certificate, found nil")
	assert.Nil(t, cert, "Expected certificate to be nil, found %v", cert)
}

// TestStrictEncryptionWithCertificate tests that hostname validation is skipped
// when a certificate is provided with encrypt=strict
func TestStrictEncryptionWithCertificate(t *testing.T) {
	// Create a temporary certificate file for testing
	// This is a minimal self-signed certificate for testing purposes
	pemCert := `-----BEGIN CERTIFICATE-----
MIIBkTCB+wIJAKHHCgVZU1tZMA0GCSqGSIb3DQEBBQUAMBExDzANBgNVBAMMBnNl
cnZlcjAeFw0yMjA0MDQxMTIxNTNaFw0zMjA0MDExMTIxNTNaMBExDzANBgNVBAMM
BnNlcnZlcjCBnzANBgkqhkiG9w0BAQEFAAOBjQAwgYkCgYEAuTU1euiQCmLQG0z8
b/5pXNlWM6gGAMJklwO9jN8vGiWQGbQXPOMPqK8xDQqLOQnVEXrKJSfF2blHRneC
qVmMNL7YSUEMxWdVaW3mQ4MzC6JgmWsxVrJeQEDZLdYVbQPXMGh5YtH5Ih8qTqJy
e4MJwPMXEKlYVPJ3LE3E8pD6vXkCAwEAATANBgkqhkiG9w0BAQUFAAOBgQBHCqVT
tZhWYXPHQFQgbKh6yvmhZfF8ZXHgZMhQQQwvqc0i5mvFpJpCQUQXAOkPGNPJANcV
QSkVdAJg8mHKYGNZ6pIYMFr7RoBLGqMnKLPMYn3VqFvMccPx7A0hKQFJBR/qV8lh
f0kGHKQEAFYGJLqJdK4KsGQDKLfZr9fqvXCCAA==
-----END CERTIFICATE-----`

	pemfile, err := os.CreateTemp("", "*.pem")
	if err != nil {
		t.Fatalf("failed to create temporary certificate file: %v", err)
	}
	defer os.Remove(pemfile.Name())
	if _, err := pemfile.WriteString(pemCert); err != nil {
		t.Fatalf("failed to write certificate to file: %v", err)
	}
	if err := pemfile.Close(); err != nil {
		t.Fatalf("failed to close certificate file: %v", err)
	}

	// Test 1: serverCertificate parameter with byte-comparison validation
	connStr := "server=differenthostname;encrypt=strict;serverCertificate=" + pemfile.Name()
	config, err := Parse(connStr)
	assert.Nil(t, err, "Expected no error parsing connection string")
	assert.Equal(t, Encryption(EncryptionStrict), config.Encryption, "Expected EncryptionStrict")
	assert.NotNil(t, config.TLSConfig, "Expected TLSConfig to be set")
	// serverCertificate uses InsecureSkipVerify with VerifyPeerCertificate for byte comparison
	assert.True(t, config.TLSConfig.InsecureSkipVerify, "Expected InsecureSkipVerify to be true when serverCertificate is provided")
	assert.NotNil(t, config.TLSConfig.VerifyPeerCertificate, "Expected VerifyPeerCertificate callback to be set")

	// Test 2: certificate parameter with traditional chain validation (backward compatible)
	connStr2 := "server=somehost;encrypt=true;certificate=" + pemfile.Name()
	config2, err := Parse(connStr2)
	assert.Nil(t, err, "Expected no error parsing connection string")
	assert.Equal(t, Encryption(EncryptionRequired), config2.Encryption, "Expected EncryptionRequired")
	assert.NotNil(t, config2.TLSConfig, "Expected TLSConfig to be set")
	assert.NotNil(t, config2.TLSConfig.RootCAs, "Expected RootCAs to be set for certificate parameter")
	// certificate parameter uses traditional chain validation, does NOT skip hostname by default
	assert.Nil(t, config2.TLSConfig.VerifyPeerCertificate, "Expected no VerifyPeerCertificate callback for traditional certificate validation")

	// Test 3: encrypt=strict without certificate should NOT skip hostname validation
	connStr3 := "server=somehost;encrypt=strict"
	config3, err := Parse(connStr3)
	assert.Nil(t, err, "Expected no error parsing connection string")
	assert.Equal(t, Encryption(EncryptionStrict), config3.Encryption, "Expected EncryptionStrict")
	assert.NotNil(t, config3.TLSConfig, "Expected TLSConfig to be set")
	assert.False(t, config3.TLSConfig.InsecureSkipVerify, "Expected InsecureSkipVerify to be false when no certificate is provided")

	// Test 4: Cannot specify both certificate and serverCertificate
	connStr4 := "server=somehost;encrypt=true;certificate=" + pemfile.Name() + ";serverCertificate=" + pemfile.Name()
	_, err = Parse(connStr4)
	assert.NotNil(t, err, "Expected error when both certificate and serverCertificate are specified")
	assert.Contains(t, err.Error(), "cannot specify both", "Error should mention conflicting parameters")

	// Test 5: Cannot specify serverCertificate with hostnameincertificate
	connStr5 := "server=somehost;encrypt=true;serverCertificate=" + pemfile.Name() + ";hostnameincertificate=othername"
	_, err = Parse(connStr5)
	assert.NotNil(t, err, "Expected error when both serverCertificate and hostnameincertificate are specified")
	assert.Contains(t, err.Error(), "cannot specify both", "Error should mention conflicting parameters")
}

// TestEpaEnabledFromEnvironment tests parsing EPA enabled from the MSSQL_USE_EPA environment variable
func TestEpaEnabledFromEnvironment(t *testing.T) {
	// Save the original environment variable value
	originalValue := os.Getenv("MSSQL_USE_EPA")
	defer func() {
		// Restore the original value
		if originalValue != "" {
			os.Setenv("MSSQL_USE_EPA", originalValue)
		} else {
			os.Unsetenv("MSSQL_USE_EPA")
		}
	}()

	// Test 1: Environment variable set to "true"
	os.Setenv("MSSQL_USE_EPA", "true")
	config, err := Parse("server=testhost")
	assert.Nil(t, err, "Expected no error parsing connection string")
	assert.True(t, config.EpaEnabled, "Expected EpaEnabled to be true when MSSQL_USE_EPA=true")

	// Test 2: Environment variable set to "false"
	os.Setenv("MSSQL_USE_EPA", "false")
	config, err = Parse("server=testhost")
	assert.Nil(t, err, "Expected no error parsing connection string")
	assert.False(t, config.EpaEnabled, "Expected EpaEnabled to be false when MSSQL_USE_EPA=false")

	// Test 3: Environment variable set to "1"
	os.Setenv("MSSQL_USE_EPA", "1")
	config, err = Parse("server=testhost")
	assert.Nil(t, err, "Expected no error parsing connection string")
	assert.True(t, config.EpaEnabled, "Expected EpaEnabled to be true when MSSQL_USE_EPA=1")

	// Test 4: Environment variable set to "0"
	os.Setenv("MSSQL_USE_EPA", "0")
	config, err = Parse("server=testhost")
	assert.Nil(t, err, "Expected no error parsing connection string")
	assert.False(t, config.EpaEnabled, "Expected EpaEnabled to be false when MSSQL_USE_EPA=0")

	// Test 5: Environment variable not set (should default to false)
	os.Unsetenv("MSSQL_USE_EPA")
	config, err = Parse("server=testhost")
	assert.Nil(t, err, "Expected no error parsing connection string")
	assert.False(t, config.EpaEnabled, "Expected EpaEnabled to be false when MSSQL_USE_EPA is not set")

	// Test 6: Connection string parameter overrides environment variable
	os.Setenv("MSSQL_USE_EPA", "true")
	config, err = Parse("server=testhost;epa enabled=false")
	assert.Nil(t, err, "Expected no error parsing connection string")
	assert.False(t, config.EpaEnabled, "Expected EpaEnabled to be false when explicitly set in connection string, overriding environment variable")

	// Test 7: Connection string parameter overrides environment variable (reverse)
	os.Setenv("MSSQL_USE_EPA", "false")
	config, err = Parse("server=testhost;epa enabled=true")
	assert.Nil(t, err, "Expected no error parsing connection string")
	assert.True(t, config.EpaEnabled, "Expected EpaEnabled to be true when explicitly set in connection string, overriding environment variable")

	// Test 8: Invalid environment variable value should cause error
	os.Setenv("MSSQL_USE_EPA", "invalid")
	_, err = Parse("server=testhost")
	assert.NotNil(t, err, "Expected error when MSSQL_USE_EPA has invalid value")
	assert.Contains(t, err.Error(), "invalid epa enabled value", "Error should mention invalid epa enabled value")

	// Test 9: Empty environment variable should default to false
	os.Setenv("MSSQL_USE_EPA", "")
	config, err = Parse("server=testhost")
	assert.Nil(t, err, "Expected no error parsing connection string")
	assert.False(t, config.EpaEnabled, "Expected EpaEnabled to be false when MSSQL_USE_EPA is empty")
}
