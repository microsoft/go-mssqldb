package mssql

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestDatetimeAccuracy validates that DATETIME type values
// are properly created from time.Time giving the same rounding
// logic as SQL Server itself implements.
//
// Datetime is rounded to increments of .000, .003, or .007 seconds (accuracy is 1/300 of a second)
// (see: https://learn.microsoft.com/en-us/sql/t-sql/data-types/datetime-transact-sql?view=sql-server-ver16).
//
// This test creates 3 schematically identical tables filled in 3 different ways:
//
//   - datetime_test_insert_time_as_str (filled via regular INSERT with time as str params)
//   - datetime_test_insert_time_as_time (filled via regular INSERT with time as go time.Time params)
//   - datetime_test_insert_bulk (filled via Bulk Copy)
//
// After creating these 3 tables the test will compare the data read from all of the tables
// expecting the data to be identical.
func TestDatetimeAccuracy(t *testing.T) {
	ctx := context.Background()
	conn, logger := open(t)
	t.Cleanup(func() {
		conn.Close()
		logger.StopLogging()
	})

	createTable := func(tableName string) {
		_, err := conn.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", tableName))
		if err != nil {
			t.Fatal("Failed to drop table: ", err)
		}
		_, err = conn.Exec(fmt.Sprintf(`CREATE TABLE %s (
			id INT NOT NULL PRIMARY KEY,
			dt DATETIME
		)`, tableName))
		if err != nil {
			t.Fatal("Failed to create table: ", err)
		}
		t.Cleanup(func() { _, _ = conn.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", tableName)) })
	}

	fillTable := func(tableName string, dts []any) {
		for i, dt := range dts {
			_, err := conn.Exec(fmt.Sprintf("INSERT INTO %s (id, dt) VALUES (@p1, @p2)", tableName), i, dt)
			if err != nil {
				t.Fatal(err)
			}
		}
	}

	fillTableBulkCopy := func(tableName string, dts []any) {
		conn, err := conn.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = conn.Close() }()
		stmt, err := conn.PrepareContext(ctx, CopyIn(tableName, BulkOptions{}, "id", "dt"))
		if err != nil {
			t.Fatal(err)
		}
		for i, dt := range dts {
			if _, err = stmt.Exec(i, dt); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = stmt.Exec(); err != nil {
			t.Fatal(err)
		}
	}

	readTable := func(tableName string) (res []time.Time) {
		rows, err := conn.QueryContext(ctx, fmt.Sprintf("SELECT dt FROM %s ORDER BY id", tableName))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var dt time.Time
			if err = rows.Scan(&dt); err != nil {
				t.Fatal(err)
			}
			res = append(res, dt)
		}
		if rows.Err() != nil {
			t.Fatal(rows.Err())
		}
		return res
	}

	// generate data to be inserted into the tables:
	// times with fraction of a second from .000 to .999.
	var dtsTime []any
	var dtsStrs []any
	for i := 0; i < 1000; i++ {
		ns := int(time.Duration(i) * (time.Second / 1000) * time.Nanosecond)
		dt := time.Date(2025, 4, 11, 10, 30, 42, ns, time.UTC)
		str := dt.Format("2006-01-02T15:04:05.999Z")
		dtsTime = append(dtsTime, dt)
		dtsStrs = append(dtsStrs, str)
	}

	createTable("datetime_test_insert_time_as_str")
	fillTable("datetime_test_insert_time_as_str", dtsStrs)

	createTable("datetime_test_insert_time_as_time")
	fillTable("datetime_test_insert_time_as_time", dtsTime)

	createTable("datetime_test_insert_bulk")
	fillTableBulkCopy("datetime_test_insert_bulk", dtsTime)

	as := readTable("datetime_test_insert_time_as_str")
	bs := readTable("datetime_test_insert_time_as_time")
	cs := readTable("datetime_test_insert_bulk")

	if len(dtsTime) != len(as) || len(dtsTime) != len(bs) || len(dtsTime) != len(cs) {
		t.Fatalf("Not all data inserted into tables: want = %d, got = %d %d %d", len(dtsTime), len(as), len(bs), len(cs))
	}

	for i := 0; i < len(dtsTime); i++ {
		if !as[i].Equal(bs[i]) || !as[i].Equal(cs[i]) {
			t.Fatalf(`Rows not equal at #%d:
			| %-36s | %-36s | %-36s |
			| %36s | %36s | %36s |`,
				i,
				"datetime_test_insert_time_as_str",
				"datetime_test_insert_time_as_time",
				"datetime_test_insert_bulk",
				as[i].Format(time.RFC3339Nano),
				bs[i].Format(time.RFC3339Nano),
				cs[i].Format(time.RFC3339Nano),
			)
		}
	}
}

func TestThreeHundredthsToNanosConverter(t *testing.T) {
	for want := 0; want < 300; want++ {
		ns := threeHundredthsOfASecondToNanos(want)
		got := nanosToThreeHundredthsOfASecond(ns)
		if want != got {
			t.Fatalf("want = %d, got = %d", want, got)
		}
	}
}
