package mssql

import (
	"errors"
	"testing"

	"github.com/microsoft/go-mssqldb/msdsn"
	"github.com/stretchr/testify/assert"
)

func TestStringForInstanceNameComparison(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple ASCII instance name",
			input:    "MSSQLSERVER",
			expected: `"MSSQLSERVER"`,
		},
		{
			name:     "lowercase instance name",
			input:    "myinstance",
			expected: `"myinstance"`,
		},
		{
			name:     "empty string",
			input:    "",
			expected: `""`,
		},
		{
			name:     "instance with numbers",
			input:    "SQLEXPRESS2019",
			expected: `"SQLEXPRESS2019"`,
		},
		{
			name:     "instance with underscore",
			input:    "MY_INSTANCE",
			expected: `"MY_INSTANCE"`,
		},
		{
			name:     "unicode character Å",
			input:    "TJUTVÅ",
			expected: `"TJUTV\xc5"`,
		},
		{
			name:     "unicode character outside BMP",
			input:    "TEST\u1234",
			expected: `"TEST\x1234"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stringForInstanceNameComparison(tt.input)
			assert.Equal(t, tt.expected, result, "stringForInstanceNameComparison(%q)", tt.input)
		})
	}
}

func TestResolveServerPort(t *testing.T) {
	tests := []struct {
		name     string
		port     uint64
		expected uint64
	}{
		{
			name:     "zero port returns default",
			port:     0,
			expected: 1433,
		},
		{
			name:     "explicit port 1433",
			port:     1433,
			expected: 1433,
		},
		{
			name:     "custom port",
			port:     5000,
			expected: 5000,
		},
		{
			name:     "high port number",
			port:     65535,
			expected: 65535,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveServerPort(tt.port)
			assert.Equal(t, tt.expected, result, "resolveServerPort(%d)", tt.port)
		})
	}
}

func TestWrapConnErr(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		port     uint64
		innerErr error
		wantHost string
		wantPort string
	}{
		{
			name:     "basic error wrapping",
			host:     "localhost",
			port:     1433,
			innerErr: errors.New("connection refused"),
			wantHost: "localhost",
			wantPort: "1433",
		},
		{
			name:     "zero port uses default",
			host:     "myserver",
			port:     0,
			innerErr: errors.New("timeout"),
			wantHost: "myserver",
			wantPort: "1433",
		},
		{
			name:     "custom port",
			host:     "10.0.0.1",
			port:     5000,
			innerErr: errors.New("network unreachable"),
			wantHost: "10.0.0.1",
			wantPort: "5000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &msdsn.Config{
				Host: tt.host,
				Port: tt.port,
			}
			result := wrapConnErr(config, tt.innerErr)

			assert.NotNil(t, result, "wrapConnErr() returned nil")

			errMsg := result.Error()
			assert.Contains(t, errMsg, tt.wantHost, "error should contain host")
			assert.Contains(t, errMsg, tt.wantPort, "error should contain port")
			assert.Contains(t, errMsg, tt.innerErr.Error(), "error should contain inner error")

			// Verify unwrapping works
			assert.ErrorIs(t, result, tt.innerErr, "wrapConnErr() error should unwrap to inner error")
		})
	}
}
