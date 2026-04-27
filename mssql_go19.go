//go:build go1.9
// +build go1.9

package mssql

import (
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/golang-sql/sqlexp"
	"github.com/shopspring/decimal"

	// "github.com/cockroachdb/apd"
	"github.com/golang-sql/civil"
)

// Type alias provided for compatibility.

type MssqlDriver = Driver           // Deprecated: users should transition to the new name when possible.
type MssqlBulk = Bulk               // Deprecated: users should transition to the new name when possible.
type MssqlBulkOptions = BulkOptions // Deprecated: users should transition to the new name when possible.
type MssqlConn = Conn               // Deprecated: users should transition to the new name when possible.
type MssqlResult = Result           // Deprecated: users should transition to the new name when possible.
type MssqlRows = Rows               // Deprecated: users should transition to the new name when possible.
type MssqlStmt = Stmt               // Deprecated: users should transition to the new name when possible.

var _ driver.NamedValueChecker = &Conn{}

// VarChar is used to encode a string parameter as VarChar instead of a sized NVarChar
type VarChar string

// NVarCharMax is used to encode a string parameter as NVarChar(max) instead of a sized NVarChar
type NVarCharMax string

// VarCharMax is used to encode a string parameter as VarChar(max) instead of a sized NVarChar
type VarCharMax string

// NChar is used to encode a string parameter as NChar instead of a sized NVarChar
type NChar string

// DateTime1 encodes parameters to original DateTime SQL types.
type DateTime1 time.Time

// DateTimeOffset encodes parameters to DateTimeOffset, preserving the UTC offset.
type DateTimeOffset time.Time

// JSON represents a SQL Server JSON value using Go's json.RawMessage.
// json.RawMessage is a raw encoded JSON value that can be used to delay
// JSON decoding or precompute a JSON encoding.
type JSON json.RawMessage

// MarshalJSON returns j as the JSON encoding of j.
// This ensures JSON is marshaled as raw JSON, not as a base64-encoded byte slice.
func (j JSON) MarshalJSON() ([]byte, error) {
	if j == nil {
		return []byte("null"), nil
	}
	return json.RawMessage(j).MarshalJSON()
}

// UnmarshalJSON sets *j to a copy of data.
// This ensures JSON can be unmarshaled from raw JSON.
func (j *JSON) UnmarshalJSON(data []byte) error {
	if j == nil {
		return fmt.Errorf("mssql.JSON: UnmarshalJSON on nil pointer")
	}
	*j = append((*j)[0:0], data...)
	return nil
}

// Scan implements the Scanner interface so JSON can be used as a scan destination.
func (j *JSON) Scan(value interface{}) error {
	if j == nil {
		return fmt.Errorf("mssql.JSON: Scan on nil pointer")
	}
	if value == nil {
		return fmt.Errorf("cannot scan NULL into *mssql.JSON (use mssql.NullJSON)")
	}
	switch v := value.(type) {
	case string:
		*j = append((*j)[:0], v...)
	case []byte:
		*j = append((*j)[:0], v...)
	case json.RawMessage:
		*j = append((*j)[:0], v...)
	default:
		return fmt.Errorf("unsupported Scan, storing driver.Value type %T into type *JSON", value)
	}
	return nil
}

// NullJSON represents a JSON value that may be null.
// NullJSON implements the Scanner interface so it can be used as a scan destination.
type NullJSON struct {
	JSON  json.RawMessage
	Valid bool // Valid is true if JSON is not NULL
}

// Scan implements the Scanner interface.
func (nj *NullJSON) Scan(value interface{}) error {
	if value == nil {
		nj.JSON, nj.Valid = nil, false
		return nil
	}
	switch v := value.(type) {
	case string:
		nj.JSON = json.RawMessage(v)
	case []byte:
		// Make a copy to avoid retaining references to driver buffers
		nj.JSON = make(json.RawMessage, len(v))
		copy(nj.JSON, v)
	case json.RawMessage:
		// Make a copy to avoid retaining references to driver buffers
		nj.JSON = make(json.RawMessage, len(v))
		copy(nj.JSON, v)
	default:
		nj.Valid = false
		return fmt.Errorf("unsupported Scan, storing driver.Value type %T into type *NullJSON", value)
	}
	nj.Valid = true
	return nil
}

// Value implements the driver Valuer interface.
func (nj NullJSON) Value() (driver.Value, error) {
	if !nj.Valid {
		return nil, nil
	}
	return string(nj.JSON), nil
}

func convertInputParameter(val interface{}) (interface{}, error) {
	switch v := val.(type) {
	case int, int16, int32, int64, int8:
		return val, nil
	case byte:
		return val, nil
	case VarChar:
		return val, nil
	case NVarCharMax:
		return val, nil
	case VarCharMax:
		return val, nil
	case NChar:
		return val, nil
	case DateTime1:
		return val, nil
	case DateTimeOffset:
		return val, nil
	case JSON:
		return val, nil
	case NullJSON:
		return val, nil
	case *JSON:
		if v == nil {
			return NullJSON{}, nil
		}
		return *v, nil
	case *NullJSON:
		if v == nil {
			return NullJSON{}, nil
		}
		return *v, nil
	case civil.Date:
		return val, nil
	case civil.DateTime:
		return val, nil
	case civil.Time:
		return val, nil
	case NullDate:
		if v.Valid {
			return v.Date, nil
		}
		return val, nil
	case NullDateTime:
		if v.Valid {
			return v.DateTime, nil
		}
		return val, nil
	case NullTime:
		if v.Valid {
			return v.Time, nil
		}
		return val, nil
	// case *apd.Decimal:
	// 	return nil
	case float32:
		return val, nil
	case driver.Valuer:
		return val, nil
	default:
		return driver.DefaultParameterConverter.ConvertValue(v)
	}
}

func (c *Conn) CheckNamedValue(nv *driver.NamedValue) error {
	switch v := nv.Value.(type) {
	case sql.Out:
		if c.outs.params == nil {
			c.outs.params = make(map[string]interface{})
		}
		c.outs.params[nv.Name] = v.Dest

		if v.Dest == nil {
			return errors.New("destination is a nil pointer")
		}

		dest_info := reflect.ValueOf(v.Dest)
		if dest_info.Kind() != reflect.Ptr {
			return errors.New("destination not a pointer")
		}

		if dest_info.IsNil() {
			return errors.New("destination is a nil pointer")
		}

		pointed_value := reflect.Indirect(dest_info)

		// don't allow pointer to a pointer, only pointer to a value can be handled
		// correctly
		if pointed_value.Kind() == reflect.Ptr {
			return errors.New("destination is a pointer to a pointer")
		}

		// Unwrap the Out value and check the inner value.
		val := pointed_value.Interface()
		if val == nil {
			return errors.New("MSSQL does not allow NULL value without type for OUTPUT parameters")
		}
		conv, err := convertInputParameter(val)
		if err != nil {
			return err
		}
		if conv == nil {
			// if we replace with nil we would lose type information
			nv.Value = sql.Out{Dest: val}
		} else {
			nv.Value = sql.Out{Dest: conv}
		}
		return nil
	case *ReturnStatus:
		*v = 0 // By default the return value should be zero.
		c.outs.returnStatus = v
		return driver.ErrRemoveArgument
	case TVP:
		return nil
	case *sqlexp.ReturnMessage:
		sqlexp.ReturnMessageInit(v)
		c.outs.msgq = v
		return driver.ErrRemoveArgument
	default:
		var err error
		nv.Value, err = convertInputParameter(nv.Value)
		return err
	}
}

func makeMoneyParam(val decimal.Decimal) (res param) {
	res.ti.TypeId = typeMoneyN

	coeff := val.Mul(decimal.New(1, 4)).IntPart()

	res.buffer = make([]byte, 8)
	res.ti.Size = 8
	binary.LittleEndian.PutUint32(res.buffer, uint32(coeff>>32))
	binary.LittleEndian.PutUint32(res.buffer[4:], uint32(coeff))

	return
}

// makeJsonParam creates a parameter for JSON/NullJSON types.
//
// Encoding asymmetry (confirmed by SqlClient source and wire captures):
//   - Client→server (RPC parameters): UTF-8 bytes via PLP, type 0xF4.
//     SqlClient: Encoding.UTF8.GetBytes(value.ToString()) in WriteParameterVarLen.
//   - Server→client (result set columns): UTF-16LE bytes via PLP, type 0xF4.
//     Decoded by decodeUcs2 in readPLPType, same as XML and nvarchar.
//
// When the server does not support native JSON (pre-2025), falls back to
// nvarchar(max) with UTF-16LE encoding in both directions.
//
// No client-side JSON validation is performed; the server validates JSON content
// on insertion into JSON columns. This is consistent with SqlClient behavior.
//
// Note: valid=true with empty data sends a non-NULL empty payload rather than
// SQL NULL so callers do not silently insert NULL when they intended to send a
// value. The server remains responsible for validating JSON content.
func (s *Stmt) makeJsonParam(data []byte, valid bool) param {
	res := param{}
	if s.c != nil && s.c.sess != nil && s.c.sess.jsonSupported {
		res.ti.TypeId = typeJson
		if valid {
			if len(data) == 0 {
				res.buffer = []byte{}
			} else {
				res.buffer = data
			}
		}
	} else {
		res.ti.TypeId = typeNVarChar
		res.ti.Size = 0 // Forces nvarchar(max) PLP format
		if valid {
			if len(data) == 0 {
				res.buffer = []byte{}
			} else {
				res.buffer = str2ucs2(string(data))
			}
		}
	}
	return res
}

func (s *Stmt) makeParamExtra(val driver.Value) (res param, err error) {
	loc := getTimezone(s.c)

	switch val := val.(type) {
	case VarChar:
		res.ti.TypeId = typeBigVarChar
		res.buffer = []byte(val)
		res.ti.Size = len(res.buffer)
	case VarCharMax:
		res.ti.TypeId = typeBigVarChar
		res.buffer = []byte(val)
		res.ti.Size = 0 // currently zero forces varchar(max)
	case NVarCharMax:
		res.ti.TypeId = typeNVarChar
		res.buffer = str2ucs2(string(val))
		res.ti.Size = 0 // currently zero forces nvarchar(max)
	case NChar:
		res.ti.TypeId = typeNChar
		res.buffer = str2ucs2(string(val))
		res.ti.Size = len(res.buffer)
	case DateTime1:
		t := time.Time(val)
		res.ti.TypeId = typeDateTimeN
		res.buffer = encodeDateTime(t)
		res.ti.Size = len(res.buffer)
	case DateTimeOffset:
		res.ti.TypeId = typeDateTimeOffsetN
		res.ti.Scale = 7
		res.buffer = encodeDateTimeOffset(time.Time(val), int(res.ti.Scale))
		res.ti.Size = len(res.buffer)
	case civil.Date:
		res.ti.TypeId = typeDateN
		res.buffer = encodeDate(val.In(loc))
		res.ti.Size = len(res.buffer)
	case civil.DateTime:
		res.ti.TypeId = typeDateTime2N
		res.ti.Scale = 7
		res.buffer = encodeDateTime2(val.In(loc), int(res.ti.Scale))
		res.ti.Size = len(res.buffer)
	case civil.Time:
		res.ti.TypeId = typeTimeN
		res.ti.Scale = 7
		res.buffer = encodeTime(val.Hour, val.Minute, val.Second, val.Nanosecond, int(res.ti.Scale))
		res.ti.Size = len(res.buffer)
	case JSON:
		res = s.makeJsonParam([]byte(val), val != nil)
	case NullJSON:
		res = s.makeJsonParam(val.JSON, val.Valid)
	case sql.Out:
		switch dest := val.Dest.(type) {
		case Money[decimal.Decimal]:
			res = makeMoneyParam(dest.Decimal)
		case Money[decimal.NullDecimal]:
			if dest.Decimal.Valid {
				res = makeMoneyParam(dest.Decimal.Decimal)
			} else {
				res.ti.TypeId = typeMoneyN
				res.buffer = []byte{}
				res.ti.Size = 8
			}
		default:
			res, err = s.makeParam(dest)
		}
		res.Flags = fByRevValue
	case TVP:
		err = val.check()
		if err != nil {
			return
		}
		schema, name, errGetName := getSchemeAndName(val.TypeName)
		if errGetName != nil {
			return
		}
		res.ti.UdtInfo.TypeName = name
		res.ti.UdtInfo.SchemaName = schema
		res.ti.TypeId = typeTvp
		columnStr, tvpFieldIndexes, errCalTypes := val.columnTypes()
		if errCalTypes != nil {
			err = errCalTypes
			return
		}
		res.buffer, err = val.encode(schema, name, columnStr, tvpFieldIndexes, s.c.sess.encoding)
		if err != nil {
			return
		}
		res.ti.Size = len(res.buffer)

	default:
		err = fmt.Errorf("mssql: unknown type for %T", val)
	}
	return
}

func scanIntoOut(name string, fromServer, scanInto interface{}) error {
	return convertAssign(scanInto, fromServer)
}

func isOutputValue(val driver.Value) bool {
	_, out := val.(sql.Out)
	return out
}
