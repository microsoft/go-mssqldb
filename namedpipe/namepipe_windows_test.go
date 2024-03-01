package namedpipe

import (
	"testing"

	"github.com/microsoft/go-mssqldb/msdsn"
	"github.com/stretchr/testify/assert"
)

func TestParseServer(t *testing.T) {
	c := &msdsn.Config{
		Port: 1000,
	}
	n := &namedPipeDialer{}
	err := n.ParseServer("server", c)
	assert.Errorf(t, err, "ParseServer with a Port")

	c = &msdsn.Config{
		Parameters:         make(map[string]string),
		ProtocolParameters: make(map[string]interface{}),
	}
	err = n.ParseServer(`\\.\pipe\MSSQL$Instance\sql\query`, c)
	assert.NoError(t, err, "ParseServer with a full pipe name")
	assert.Equal(t, "", c.Host, "Config Host with a full pipe name")
	data, ok := c.ProtocolParameters[n.Protocol()]
	assert.True(t, ok, "Should have added ProtocolParameters when server is pipe name")
	switch d := data.(type) {
	case namedPipeData:
		assert.Equal(t, `\\.\pipe\MSSQL$Instance\sql\query`, d.PipeName, "Pipe name in ProtocolParameters when server is pipe name")
	default:
		assert.Fail(t, "Incorrect protocol parameters type:", d)
	}

	c = &msdsn.Config{
		Parameters:         make(map[string]string),
		ProtocolParameters: make(map[string]interface{}),
	}
	err = n.ParseServer(`.\instance`, c)
	assert.NoError(t, err, "ParseServer .")
	assert.Equal(t, "localhost", c.Host, `Config Host with server == .\instance`)
	assert.Equal(t, "instance", c.Instance, `Config Instance with server == .\instance`)
	_, ok = c.ProtocolParameters[n.Protocol()]
	assert.Equal(t, ok, false, "Should have no namedPipeData when pipe name omitted")

	c = &msdsn.Config{
		Host:               "server",
		Parameters:         make(map[string]string),
		ProtocolParameters: make(map[string]interface{}),
	}
	c.Parameters["pipe"] = `myinstance\sql\query`
	err = n.ParseServer(`anything`, c)
	assert.NoError(t, err, "ParseServer anything")
	data, ok = c.ProtocolParameters[n.Protocol()]
	assert.True(t, ok, "Should have added ProtocolParameters when pipe name is provided")
	switch d := data.(type) {
	case namedPipeData:
		assert.Equal(t, `\\server\pipe\myinstance\sql\query`, d.PipeName, "Pipe name in ProtocolParameters")
	default:
		assert.Fail(t, "Incorrect protocol parameters type:", d)
	}

}
