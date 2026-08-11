package mssql

import (
	"strings"
)

// TSQLQuoter implements sqlexp.Quoter
type TSQLQuoter struct {
}

// ID quotes identifiers such as schema, table, or column names.
// This implementation handles multi-part names.
func (TSQLQuoter) ID(name string) string {
	return "[" + strings.Replace(name, "]", "]]", -1) + "]"
}

// splitMultiPartID splits a possibly multi-part name such as "db.schema.table"
// into its individual parts. Dots inside a part delimited with [] or "" are not
// treated as separators.
func splitMultiPartID(name string) []string {
	parts := make([]string, 0, 4)
	var part strings.Builder
	// closer is the byte that ends the delimited part currently being read,
	// or 0 when the reader is not inside a delimited part.
	var closer byte
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case closer != 0:
			part.WriteByte(c)
			if c != closer {
				continue
			}
			// A doubled delimiter is an escaped literal rather than the end
			// of the part.
			if i+1 < len(name) && name[i+1] == closer {
				i++
				part.WriteByte(c)
				continue
			}
			closer = 0
		case c == '[':
			closer = ']'
			part.WriteByte(c)
		case c == '"':
			closer = '"'
			part.WriteByte(c)
		case c == '.':
			parts = append(parts, part.String())
			part.Reset()
		default:
			part.WriteByte(c)
		}
	}
	return append(parts, part.String())
}

// unquoteID reports whether part is a single well-formed delimited identifier
// and, if so, returns the identifier without its delimiters. A part such as
// `[a] SELECT 1 --[b]` starts and ends with brackets but is not a single
// identifier, so it is rejected.
func unquoteID(part string) (string, bool) {
	if len(part) < 2 {
		return "", false
	}
	var closer byte
	switch part[0] {
	case '[':
		closer = ']'
	case '"':
		closer = '"'
	default:
		return "", false
	}
	if part[len(part)-1] != closer {
		return "", false
	}

	body := part[1 : len(part)-1]
	var name strings.Builder
	for i := 0; i < len(body); i++ {
		if body[i] == closer {
			// A lone delimiter would close the identifier early, so part is
			// not a single delimited identifier.
			if i+1 >= len(body) || body[i+1] != closer {
				return "", false
			}
			i++
		}
		name.WriteByte(body[i])
	}
	return name.String(), true
}

// quoteIDPart quotes one part of a multi-part name. A part the caller already
// delimited with [] or "" keeps its identifier, everything else is escaped.
// An empty part is preserved so a name such as "db..table" keeps deferring to
// the server's default schema.
func quoteIDPart(part string) string {
	part = strings.TrimSpace(part)
	if part == "" {
		return ""
	}
	if name, ok := unquoteID(part); ok {
		return TSQLQuoter{}.ID(name)
	}
	return TSQLQuoter{}.ID(part)
}

// quoteMultiPartID quotes a possibly multi-part object name such as "table",
// "schema.table" or "db.schema.table" so it is safe to embed in query text.
func quoteMultiPartID(name string) string {
	parts := splitMultiPartID(name)
	for i, part := range parts {
		parts[i] = quoteIDPart(part)
	}
	return strings.Join(parts, ".")
}

// quoteBulkOrder quotes the column name in a BulkOptions.Order entry so it is
// safe to embed in the ORDER hint of an INSERT BULK statement. An optional
// trailing ASC or DESC sort direction is preserved. A column name that itself
// ends in "asc" or "desc" has to be delimited by the caller to be told apart
// from a sort direction.
func quoteBulkOrder(entry string) string {
	name := strings.TrimSpace(entry)
	direction := ""
	if i := strings.LastIndexAny(name, " \t\r\n"); i >= 0 {
		switch suffix := strings.ToUpper(strings.TrimSpace(name[i:])); suffix {
		case "ASC", "DESC":
			direction = " " + suffix
			name = name[:i]
		}
	}
	return quoteIDPart(name) + direction
}

// Value quotes database values such as string or []byte types as strings
// that are suitable and safe to embed in SQL text. The returned value
// of a string will include all surrounding quotes.
//
// If a value type is not supported it must panic.
func (TSQLQuoter) Value(v interface{}) string {
	switch v := v.(type) {
	default:
		panic("unsupported value")

	case string:
		return sqlString(v)
	case VarChar:
		return sqlString(string(v))
	case VarCharMax:
		return sqlString(string(v))
	case NVarCharMax:
		return sqlString(string(v))
	}
}

func sqlString(v string) string {
	return "'" + strings.Replace(string(v), "'", "''", -1) + "'"
}
