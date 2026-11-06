package mssql

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestBulkCopyOutOfRangeValues(t *testing.T) {
	b := &Bulk{cn: &Conn{sess: &tdsSession{encoding: benchmarkEncoding()}}}
	intCol := func(size int) columnStruct {
		return columnStruct{ColName: "v", ti: typeInfo{TypeId: typeIntN, Size: size}}
	}
	moneyCol := func(size int) columnStruct {
		return columnStruct{ColName: "v", ti: typeInfo{TypeId: typeMoneyN, Size: size}}
	}
	tests := []struct {
		name string
		col  columnStruct
		ok   []interface{}
		bad  []interface{}
	}{
		{"tinyint", intCol(1), []interface{}{int64(0), int64(255), float64(255.9)}, []interface{}{int64(-1), int64(256), float64(300)}},
		{"smallint", intCol(2), []interface{}{int64(math.MinInt16), int64(math.MaxInt16)}, []interface{}{int64(math.MinInt16 - 1), int64(math.MaxInt16 + 1)}},
		{"int", intCol(4), []interface{}{int64(math.MinInt32), int64(math.MaxInt32), float32(1.5)}, []interface{}{int64(math.MaxInt32 + 1), int64(1<<32 + 1)}},
		{"bigint", intCol(8), []interface{}{int64(math.MinInt64), float64(math.MinInt64), float64(9.2e18)}, []interface{}{float64(1e19), float64(-1e19), math.NaN(), math.Inf(1), float32(1e19)}},
		{"smallmoney", moneyCol(4), []interface{}{"214748.3647", "-214748.3648"}, []interface{}{"214748.3648", "-214748.3649", "429496.7296"}},
		{"money", moneyCol(8), []interface{}{"922337203685477.5807", "-922337203685477.5808"}, []interface{}{"922337203685477.5808", "-922337203685477.5809", "1844674407370955.1616", "123456789012345678901234"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, v := range tt.ok {
				if _, err := b.makeParam(v, tt.col); err != nil {
					t.Errorf("%v: unexpected error: %v", v, err)
				}
			}
			for _, v := range tt.bad {
				p, err := b.makeParam(v, tt.col)
				if err == nil || !strings.Contains(err.Error(), "out of range") {
					t.Errorf("%v: expected out of range error, got %v (bytes % x)", v, err, p.buffer)
				}
			}
		})
	}
}

func TestBulkcopyOutOfRangeRowIsSkipped(t *testing.T) {
	pool, logger := open(t)
	defer pool.Close()
	defer logger.StopLogging()

	ctx := context.Background()
	conn, err := pool.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "CREATE TABLE #outofrange (i int NOT NULL, m smallmoney NULL)"); err != nil {
		t.Fatal(err)
	}
	stmt, err := conn.PrepareContext(ctx, CopyIn("#outofrange", BulkOptions{}, "i", "m"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = stmt.ExecContext(ctx, int64(math.MaxInt32+1), nil)
	assert.ErrorContains(t, err, "out of range")
	_, err = stmt.ExecContext(ctx, 1, decimal.RequireFromString("214748.3648"))
	assert.ErrorContains(t, err, "out of range")

	_, err = stmt.ExecContext(ctx, 2, decimal.RequireFromString("-214748.3648"))
	assert.NoError(t, err)
	_, err = stmt.ExecContext(ctx)
	assert.NoError(t, err)
	assert.NoError(t, stmt.Close())

	var n, i int
	var m string
	err = conn.QueryRowContext(ctx, "SELECT COUNT(*), MIN(i), CONVERT(varchar(20), MIN(m), 2) FROM #outofrange").Scan(&n, &i, &m)
	assert.NoError(t, err)
	assert.Equal(t, []interface{}{1, 2, "-214748.3648"}, []interface{}{n, i, m})
}
