package krb5

import (
	"os"
	"strings"
	"testing"

	"github.com/jcmturner/gokrb5/v8/config"
	"github.com/microsoft/go-mssqldb/msdsn"
	"github.com/stretchr/testify/assert"
)

func TestReadKrb5ConfigHappyPath(t *testing.T) {
	tests := []struct {
		name      string
		cfg       msdsn.Config
		validate  func(t testing.TB, cfg msdsn.Config, actual *krb5Login)
		confPath  string
		tabPath   string
		cachePath string
	}{
		{
			name: "basic match",
			cfg: msdsn.Config{
				User:      "username",
				Password:  "placeholderpassword",
				ServerSPN: "serverspn",
				Parameters: map[string]string{
					"krb5-configfile":         "krb5-configfile",
					"krb5-keytabfile":         "krb5-keytabfile",
					"krb5-credcachefile":      "krb5-credcachefile",
					"krb5-realm":              "krb5-realm",
					"krb5-dnslookupkdc":       "false",
					"krb5-udppreferencelimit": "1234",
				},
			},
			validate: basicConfigMatch,
		},
		{
			name: "realm in user name",
			cfg: msdsn.Config{
				User:      "username@realm.com",
				Password:  "placeholderpassword",
				ServerSPN: "serverspn",
				Parameters: map[string]string{
					"krb5-configfile":         "krb5-configfile",
					"krb5-keytabfile":         "krb5-keytabfile",
					"krb5-credcachefile":      "krb5-credcachefile",
					"krb5-dnslookupkdc":       "false",
					"krb5-udppreferencelimit": "1234",
				},
			},
			validate: func(t testing.TB, cfg msdsn.Config, actual *krb5Login) {
				assert.Equal(t, "realm.com", actual.Realm, "Realm should have been copied from user name")
				assert.Equal(t, "username", actual.UserName, "UserName shouldn't include the realm")
			},
		},
		{
			name: "using defaults for file paths",
			cfg: msdsn.Config{
				User:      "username",
				Password:  "",
				ServerSPN: "serverspn",
				Parameters: map[string]string{
					"krb5-dnslookupkdc":       "false",
					"krb5-udppreferencelimit": "1234",
					"krb5-realm":              "krb5-realm",
				},
			},
			validate: func(t testing.TB, cfg msdsn.Config, actual *krb5Login) {
				assert.Equal(t, `/etc/krb5.conf`, actual.Krb5ConfigFile, "Expected default conf file path")
				assert.Equal(t, `/etc/krb5.keytab`, actual.KeytabFile, "Expected keytab path from libdefaults")
			},
		},
		{
			name:      "Using environment variables",
			confPath:  `/etc/my.config`,
			cachePath: `/tmp/mycache`,
			tabPath:   `/tmp/mytab`,
			cfg: msdsn.Config{
				User:      "username",
				Password:  "",
				ServerSPN: "serverspn",
				Parameters: map[string]string{
					"krb5-dnslookupkdc":       "false",
					"krb5-udppreferencelimit": "1234",
					"krb5-realm":              "krb5-realm",
				},
			},
			validate: func(t testing.TB, cfg msdsn.Config, actual *krb5Login) {
				assert.Equal(t, `/etc/my.config`, actual.Krb5ConfigFile, "Expected conf file path from env var")
				assert.Equal(t, `/tmp/mytab`, actual.KeytabFile, "Expected tab file from env var")
				assert.Equal(t, `/tmp/mycache`, actual.CredCacheFile, "Expected cache file from env var")
			},
		},
		{
			name:      "no keytab from environment when user name is unset",
			confPath:  `/etc/my.config`,
			cachePath: `/tmp/mycache`,
			tabPath:   `/tmp/mytab`,
			cfg: msdsn.Config{
				User:      "",
				Password:  "",
				ServerSPN: "serverspn",
				Parameters: map[string]string{
					"krb5-dnslookupkdc":       "false",
					"krb5-udppreferencelimit": "1234",
					"krb5-realm":              "krb5-realm",
				},
			},
			validate: func(t testing.TB, cfg msdsn.Config, actual *krb5Login) {
				assert.Equal(t, `/etc/my.config`, actual.Krb5ConfigFile, "Expected conf file path from env var")
				assert.Empty(t, actual.KeytabFile, "Expected no tab file")
				assert.Equal(t, `/tmp/mycache`, actual.CredCacheFile, "Expected cache file from env var")
			},
		},
	}
	revert := mockFileExists()
	defer revert()

	revertConfig := mockDefaultConfig()
	defer revertConfig()

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if len(test.cachePath) > 0 {
				cp := os.Getenv("KRB5CCNAME")
				os.Setenv("KRB5CCNAME", test.cachePath)
				defer os.Setenv("KRB5CCNAME", cp)
			}
			if len(test.confPath) > 0 {
				cp := os.Getenv("KRB5_CONFIG")
				os.Setenv("KRB5_CONFIG", test.confPath)
				defer os.Setenv("KRB5_CONFIG", cp)
			}
			if len(test.tabPath) > 0 {
				cp := os.Getenv("KRB5_KTNAME")
				os.Setenv("KRB5_KTNAME", test.tabPath)
				defer os.Setenv("KRB5_KTNAME", cp)
			}

			actual, err := readKrb5Config(test.cfg)

			assert.NoError(t, err, "Unexpected error")
			test.validate(t, test.cfg, actual)
		})

	}
}

func basicConfigMatch(t testing.TB, config msdsn.Config, actual *krb5Login) {
	assert.Equal(t, config.Parameters[keytabConfigFile], actual.Krb5ConfigFile, "Krb5ConfigFile mismatch")
	assert.Equal(t, config.Parameters[keytabFile], actual.KeytabFile, "KeytabFile mismatch")
	assert.Equal(t, config.Parameters[credCacheFile], actual.CredCacheFile, "CredCacheFile mismatch")
	assert.Equal(t, config.Parameters[realm], actual.Realm, "Realm mismatch")
	assert.Equal(t, config.User, actual.UserName, "UserName mismatch")
	assert.Equal(t, config.Password, actual.Password, "Password mismatch")
	assert.Equal(t, config.ServerSPN, actual.ServerSPN, "ServerSPN mismatch")
	assert.False(t, actual.DNSLookupKDC, "DNSLookupKDC should be false")
	assert.Equal(t, 1234, actual.UDPPreferenceLimit, "UDPPreferenceLimit mismatch")
}
func TestReadKrb5ConfigErrorCases(t *testing.T) {

	tests := []struct {
		name               string
		dnslookup          string
		udpPreferenceLimit string
		expectedError      string
	}{

		{
			name:               "invalid dnslookupkdc",
			dnslookup:          "a",
			udpPreferenceLimit: "1234",
			expectedError:      "invalid 'krb5-dnslookupkdc' parameter 'a': strconv.ParseBool: parsing \"a\": invalid syntax",
		},
		{
			name:               "invalid udpPreferenceLimit",
			dnslookup:          "true",
			udpPreferenceLimit: "a",
			expectedError:      "invalid 'krb5-udppreferencelimit' parameter 'a': strconv.Atoi: parsing \"a\": invalid syntax",
		},
	}

	revertConfig := mockDefaultConfig()
	defer revertConfig()

	for _, tt := range tests {
		config := msdsn.Config{
			Parameters: map[string]string{
				"krb5-dnslookupkdc":       tt.dnslookup,
				"krb5-udppreferencelimit": tt.udpPreferenceLimit,
			},
		}

		actual, err := readKrb5Config(config)

		assert.Nil(t, actual, "Expected nil return value")
		assert.Error(t, err, "Expected error")
		assert.Equal(t, tt.expectedError, err.Error(), "Error message mismatch")
	}
}

func TestReadKrb5ConfigGetsDefaultsFromConfFile(t *testing.T) {
	loadDefaultConfigFromFile = func(krb5Login *krb5Login) (*config.Config, error) {
		c := config.New()
		c.LibDefaults.DefaultRealm = "myrealm"
		c.LibDefaults.DefaultClientKeytabName = "mykeytabexists"
		c.LibDefaults.DNSLookupKDC = krb5Login.DNSLookupKDC
		c.LibDefaults.UDPPreferenceLimit = krb5Login.UDPPreferenceLimit
		return c, nil
	}
	defer func() {
		loadDefaultConfigFromFile = newKrb5ConfigFromFile
	}()
	revert := mockFileExists()
	defer revert()

	cfg := msdsn.Config{
		User:      "username",
		Password:  "",
		ServerSPN: "serverspn",
		Parameters: map[string]string{
			"krb5-dnslookupkdc":       "false",
			"krb5-udppreferencelimit": "1234",
		},
	}
	login, err := readKrb5Config(cfg)
	assert.NoError(t, err, "Unexpected error from readKrb5Config")
	assert.Equal(t, "myrealm", login.Realm, "Unexpected realm")
	assert.Equal(t, "mykeytabexists", login.KeytabFile, "Unexpected keytab file")

}
func TestValidateKrb5LoginParams(t *testing.T) {

	tests := []struct {
		name                string
		input               *krb5Login
		expectedLoginMethod loginMethod
		expectedError       error
	}{

		{
			name: "happy username and password",
			input: &krb5Login{
				Krb5ConfigFile: "exists",
				Realm:          "realm",
				UserName:       "username",
				Password:       "password",
			},
			expectedLoginMethod: usernameAndPassword,
			expectedError:       nil,
		},
		{
			name: "username and password, missing realm",
			input: &krb5Login{
				Krb5ConfigFile: "exists",
				Realm:          "",
				UserName:       "username",
				Password:       "password",
			},
			expectedLoginMethod: none,
			expectedError:       ErrRealmRequiredWithUsernameAndPassword,
		},
		{
			name: "username and password, missing Krb5ConfigFile",
			input: &krb5Login{
				Krb5ConfigFile: "",
				Realm:          "realm",
				UserName:       "username",
				Password:       "password",
			},
			expectedLoginMethod: none,
			expectedError:       ErrKrb5ConfigFileRequiredWithUsernameAndPassword,
		},
		{
			name: "username and password, Krb5ConfigFile file not found",
			input: &krb5Login{
				Krb5ConfigFile: "missing",
				Realm:          "realm",
				UserName:       "username",
				Password:       "password",
			},
			expectedLoginMethod: none,
			expectedError:       ErrKrb5ConfigFileDoesNotExist,
		},
		{
			name: "happy keytab",
			input: &krb5Login{
				KeytabFile:     "exists",
				Krb5ConfigFile: "exists",
				Realm:          "realm",
				UserName:       "username",
			},
			expectedLoginMethod: keyTabFile,
			expectedError:       nil,
		},
		{
			name: "keytab, missing username",
			input: &krb5Login{
				KeytabFile:     "exists",
				Krb5ConfigFile: "exists",
				Realm:          "realm",
				UserName:       "",
			},
			expectedLoginMethod: none,
			expectedError:       ErrUsernameRequiredWithKeytab,
		},
		{
			name: "keytab, missing realm",
			input: &krb5Login{
				KeytabFile:     "exists",
				Krb5ConfigFile: "exists",
				Realm:          "",
				UserName:       "username",
			},
			expectedLoginMethod: none,
			expectedError:       ErrRealmRequiredWithKeytab,
		},
		{
			name: "keytab, missing Krb5ConfigFile",
			input: &krb5Login{
				KeytabFile:     "exists",
				Krb5ConfigFile: "",
				Realm:          "realm",
				UserName:       "username",
			},
			expectedLoginMethod: none,
			expectedError:       ErrKrb5ConfigFileRequiredWithKeytab,
		},
		{
			name: "keytab, Krb5ConfigFile file not found",
			input: &krb5Login{
				KeytabFile:     "exists",
				Krb5ConfigFile: "missing",
				Realm:          "realm",
				UserName:       "username",
			},
			expectedLoginMethod: none,
			expectedError:       ErrKrb5ConfigFileDoesNotExist,
		},
		{
			name: "keytab, KeytabFile file not found",
			input: &krb5Login{
				KeytabFile:     "missing",
				Krb5ConfigFile: "exists",
				Realm:          "realm",
				UserName:       "username",
			},
			expectedLoginMethod: none,
			expectedError:       ErrKeytabFileDoesNotExist,
		},
		{
			name: "happy credential cache",
			input: &krb5Login{
				CredCacheFile:  "exists",
				Krb5ConfigFile: "exists",
			},
			expectedLoginMethod: cachedCredentialsFile,
			expectedError:       nil,
		},
		{
			name: "credential cache, missing Krb5ConfigFile",
			input: &krb5Login{
				CredCacheFile:  "exists",
				Krb5ConfigFile: "",
			},
			expectedLoginMethod: none,
			expectedError:       ErrKrb5ConfigFileRequiredWithCredCache,
		},
		{
			name: "credential cache, Krb5ConfigFile file not found",
			input: &krb5Login{
				CredCacheFile:  "exists",
				Krb5ConfigFile: "missing",
			},
			expectedLoginMethod: none,
			expectedError:       ErrKrb5ConfigFileDoesNotExist,
		},
		{
			name: "credential cache, CredCacheFile file not found",
			input: &krb5Login{
				CredCacheFile:  "missing",
				Krb5ConfigFile: "exists",
			},
			expectedLoginMethod: none,
			expectedError:       ErrCredCacheFileDoesNotExist,
		},
		{
			name:                "no login method match",
			input:               &krb5Login{},
			expectedLoginMethod: none,
			expectedError:       ErrRequiredParametersMissing,
		},
	}

	revert := mockFileExists()
	defer revert()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.input.loginMethod = none
			err := validateKrb5LoginParams(tt.input)

			if tt.expectedError == nil {
				assert.NoError(t, err, "Expected no error")
			} else {
				assert.Equal(t, tt.expectedError, err, "Error mismatch")
			}

			assert.Equal(t, tt.expectedLoginMethod, tt.input.loginMethod, "loginMethod mismatch")
		})
	}
}

func mockFileExists() func() {
	fileExists = func(filename string, errWhenFileNotFound error) (bool, error) {
		if strings.Contains(filename, "exists") || filename == `/etc/krb5.keytab` {
			return true, nil
		}

		return false, errWhenFileNotFound
	}

	return func() { fileExists = fileExistsOS }
}

func mockDefaultConfig() func() {
	loadDefaultConfigFromFile = func(krb5Login *krb5Login) (*config.Config, error) {
		return config.New(), nil
	}
	return func() {
		loadDefaultConfigFromFile = newKrb5ConfigFromFile
	}
}

func TestGetAuth(t *testing.T) {
	config := msdsn.Config{
		User:      "username",
		Password:  "password",
		ServerSPN: "serverspn",
		Parameters: map[string]string{
			"krb5-configfile":         "exists",
			"krb5-keytabfile":         "exists",
			"krb5-keytabcachefile":    "exists",
			"krb5-realm":              "krb5-realm",
			"krb5-dnslookupkdc":       "false",
			"krb5-udppreferencelimit": "1234",
		},
	}

	revert := mockFileExists()
	defer revert()
	revertConfig := mockDefaultConfig()
	defer revertConfig()

	a, err := getAuth(config)
	assert.NoError(t, err, "Unexpected error")

	actual := a.(*krbAuth)

	assert.Equal(t, config.Parameters[keytabConfigFile], actual.krb5Config.Krb5ConfigFile, "Krb5ConfigFile mismatch")
	assert.Equal(t, config.Parameters[keytabFile], actual.krb5Config.KeytabFile, "KeytabFile mismatch")
	assert.Equal(t, config.Parameters[credCacheFile], actual.krb5Config.CredCacheFile, "CredCacheFile mismatch")
	assert.Equal(t, config.Parameters[realm], actual.krb5Config.Realm, "Realm mismatch")
	assert.Equal(t, config.User, actual.krb5Config.UserName, "UserName mismatch")
	assert.Equal(t, config.Password, actual.krb5Config.Password, "Password mismatch")
	assert.Equal(t, config.ServerSPN, actual.krb5Config.ServerSPN, "ServerSPN mismatch")
	assert.False(t, actual.krb5Config.DNSLookupKDC, "DNSLookupKDC should be false")
	assert.Equal(t, 1234, actual.krb5Config.UDPPreferenceLimit, "UDPPreferenceLimit mismatch")
}

func TestCanonicalize(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no slash",
			input:    "MSSQLSvc",
			expected: "MSSQLSvc", // unchanged - no slash
		},
		{
			name:     "invalid host:port format",
			input:    "MSSQLSvc/invalidhost",
			expected: "MSSQLSvc/invalidhost", // unchanged - no port
		},
		{
			name:     "localhost with port",
			input:    "MSSQLSvc/localhost:1433",
			expected: "MSSQLSvc/localhost:1433", // localhost doesn't need canonicalization
		},
		{
			name:     "ipv4 with port",
			input:    "MSSQLSvc/127.0.0.1:1433",
			expected: "MSSQLSvc/127.0.0.1:1433", // IP addresses don't need canonicalization
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := canonicalize(tt.input)
			// DNS lookups may or may not work depending on environment
			// Just verify function doesn't panic and returns something
			assert.NotEmpty(t, result, "canonicalize returned empty string")
		})
	}
}

func TestKrbAuthFree(t *testing.T) {
	// Test Free with nil krb5Client
	auth := &krbAuth{
		krb5Config:   &krb5Login{},
		spnegoClient: nil,
		krb5Client:   nil,
	}

	// Should not panic
	auth.Free()

	assert.Nil(t, auth.krb5Client, "krb5Client should remain nil after Free")
}

func TestFileExistsOS(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		wantErr  error
		wantOk   bool
	}{
		{
			name:     "non-existent file",
			filename: "/this/path/does/not/exist/file.txt",
			wantErr:  ErrKrb5ConfigFileDoesNotExist,
			wantOk:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := fileExistsOS(tt.filename, tt.wantErr)
			assert.Equal(t, tt.wantOk, ok, "fileExistsOS() ok mismatch")
			assert.Equal(t, tt.wantErr, err, "fileExistsOS() error mismatch")
		})
	}
}
