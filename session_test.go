package mssql

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/microsoft/go-mssqldb/msdsn"
	"github.com/stretchr/testify/assert"
)

func TestNewSession(t *testing.T) {
	p := msdsn.Config{
		LogFlags: 32,
	}
	id, _ := uuid.Parse("5ac439f7-d5de-484c-8e0a-cbe27e7e9d72")
	p.ActivityID = id[:]
	buf := makeBuf(9, []byte{0x01 /*id*/, 0xFF /*status*/, 0x0, 0x9 /*size*/, 0xff, 0xff, 0xff, 0xff, 0x02 /*test byte*/})
	sess := newSession(buf, nil, p)
	assert.Equal(t, uint64(32), sess.logFlags, "logFlags")
	activityid, err := sess.activityid.Value()
	if assert.NoError(t, err, "activityid.Value()") {
		assert.Equal(t, p.ActivityID, activityid.([]byte), "activityid")
	}
	connidStr := sess.connid.String()
	_, err = uuid.Parse(connidStr)
	if assert.NoErrorf(t, err, "Invalid connid '%s'", connidStr) {
		assert.NotEqual(t, "00000000-0000-0000-0000-000000000000", connidStr)
	}
}

func TestPreparePreloginFields(t *testing.T) {
	p := msdsn.Config{
		LogFlags:   32,
		Encryption: msdsn.EncryptionStrict,
		Instance:   "i",
	}
	fe := &featureExtFedAuth{FedAuthLibrary: FedAuthLibraryADAL}
	// any 16 bytes would do
	id, _ := uuid.Parse("5ac439f7-d5de-484c-8e0a-cbe27e7e9d72")
	p.ActivityID = id[:]
	buf := makeBuf(9, []byte{0x01 /*id*/, 0xFF /*status*/, 0x0, 0x9 /*size*/, 0xff, 0xff, 0xff, 0xff, 0x02 /*test byte*/})
	sess := newSession(buf, nil, p)
	fields := sess.preparePreloginFields(context.Background(), p, fe)
	assert.Equal(t, []byte{encryptStrict}, fields[preloginENCRYPTION], "preloginENCRYPTION")
	assert.Equal(t, []byte{'i', 0}, fields[preloginINSTOPT], "preloginINSTOPT")
	traceid := fields[preloginTRACEID]
	assert.Equal(t, id[:], traceid[16:32], "activity id portion of preloginTRACEID")
	var connid UniqueIdentifier
	err := connid.Scan(traceid[:16])
	if assert.NoError(t, err, "invalid connection id portion of preloginTRACEID") {
		assert.Equal(t, sess.connid.String(), connid.String(), "connection id portion of preloginTRACEID")
	}

	assert.Equal(t, []byte{1}, fields[preloginFEDAUTHREQUIRED], "preloginFEDAUTHREQUIRED")
}
