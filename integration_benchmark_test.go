package mssql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Integration benchmarks exercise full end-to-end paths through the driver.
// They require a SQL Server connection (SQLSERVER_DSN or HOST/DATABASE env vars).
// Without a connection, they are skipped gracefully.

func benchmarkDB(b *testing.B) *sql.DB {
	b.Helper()
	connector, err := NewConnector(makeConnStr(b).String())
	if err != nil {
		b.Fatal("Open connection failed:", err.Error())
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(0)
	b.Cleanup(func() { db.Close() })
	// Warm up the connection
	if err := db.Ping(); err != nil {
		b.Fatal("Ping failed:", err)
	}
	return db
}

// benchmarkConn returns a pinned single connection for benchmarks that use
// session-scoped state like temp tables.
func benchmarkConn(b *testing.B) (*sql.Conn, context.Context) {
	b.Helper()
	connector, err := NewConnector(makeConnStr(b).String())
	if err != nil {
		b.Fatal("Open connection failed:", err.Error())
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(0)
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		b.Fatal("Conn failed:", err)
	}
	b.Cleanup(func() {
		conn.Close()
		db.Close()
	})
	return conn, ctx
}

func BenchmarkRoundTrip_ConnectDisconnect(b *testing.B) {
	// Measures the full connection establishment cost: TCP + TLS + TDS login + auth
	connStr := makeConnStr(b).String()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		connector, err := NewConnector(connStr)
		if err != nil {
			b.Fatal(err)
		}
		db := sql.OpenDB(connector)
		db.SetMaxOpenConns(1)
		if err := db.Ping(); err != nil {
			b.Fatal(err)
		}
		db.Close()
	}
}

func BenchmarkRoundTrip_Select1(b *testing.B) {
	db := benchmarkDB(b)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var n int
		if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&n); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRoundTrip_ParamQuery(b *testing.B) {
	db := benchmarkDB(b)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var s string
		err := db.QueryRowContext(ctx, "SELECT @p1 + @p2",
			sql.Named("p1", "hello"),
			sql.Named("p2", "world"),
		).Scan(&s)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRoundTrip_MultiRow(b *testing.B) {
	db := benchmarkDB(b)
	ctx := context.Background()
	// Returns ~100 rows from system catalog
	query := "SELECT TOP 100 object_id, name, type FROM sys.all_objects"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			b.Fatal(err)
		}
		count := 0
		for rows.Next() {
			var id int
			var name, typ string
			if err := rows.Scan(&id, &name, &typ); err != nil {
				b.Fatal(err)
			}
			count++
		}
		rows.Close()
		if count == 0 {
			b.Fatal("expected rows")
		}
	}
}

func BenchmarkRoundTrip_LargeResultSet(b *testing.B) {
	db := benchmarkDB(b)
	ctx := context.Background()
	// Cross join to get ~1000 rows
	query := `SELECT TOP 1000 a.object_id, a.name
		FROM sys.all_objects a CROSS JOIN sys.all_objects b`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			b.Fatal(err)
		}
		count := 0
		for rows.Next() {
			var id int
			var name string
			if err := rows.Scan(&id, &name); err != nil {
				b.Fatal(err)
			}
			count++
		}
		rows.Close()
		if count != 1000 {
			b.Fatalf("expected 1000 rows, got %d", count)
		}
	}
}

func BenchmarkRoundTrip_ExecInsert(b *testing.B) {
	conn, ctx := benchmarkConn(b)
	_, err := conn.ExecContext(ctx, `CREATE TABLE #bench_insert (
		id INT IDENTITY PRIMARY KEY,
		val NVARCHAR(100),
		num INT,
		ts DATETIME2
	)`)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := conn.ExecContext(ctx,
			"INSERT INTO #bench_insert (val, num, ts) VALUES (@p1, @p2, @p3)",
			fmt.Sprintf("row-%d", i), i, time.Now(),
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRoundTrip_StoredProc(b *testing.B) {
	db := benchmarkDB(b)
	ctx := context.Background()
	// Use built-in sp_executesql as a stored procedure benchmark
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var n int
		err := db.QueryRowContext(ctx,
			"EXEC sp_executesql N'SELECT @val', N'@val INT', @val = @p1",
			sql.Named("p1", 42),
		).Scan(&n)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRoundTrip_Transaction(b *testing.B) {
	conn, ctx := benchmarkConn(b)
	_, err := conn.ExecContext(ctx, `CREATE TABLE #bench_tx (id INT, val NVARCHAR(50))`)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			b.Fatal(err)
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO #bench_tx VALUES (@p1, @p2)", i, "txn-test")
		if err != nil {
			tx.Rollback()
			b.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRoundTrip_BulkInsert(b *testing.B) {
	conn, ctx := benchmarkConn(b)

	for _, rowCount := range []int{100, 1000} {
		b.Run(fmt.Sprintf("Rows_%d", rowCount), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				// Re-create table each iteration
				_, err := conn.ExecContext(ctx, `
					IF OBJECT_ID('tempdb..#bench_bulk') IS NOT NULL DROP TABLE #bench_bulk;
					CREATE TABLE #bench_bulk (
						id INT,
						name NVARCHAR(100),
						amount FLOAT,
						created DATETIME2
					)`)
				if err != nil {
					b.Fatal(err)
				}

				stmt, err := conn.PrepareContext(ctx, CopyIn("#bench_bulk", BulkOptions{}, "id", "name", "amount", "created"))
				if err != nil {
					b.Fatal(err)
				}
				now := time.Now()
				for r := 0; r < rowCount; r++ {
					_, err = stmt.Exec(r, fmt.Sprintf("name-%d", r), float64(r)*1.5, now)
					if err != nil {
						b.Fatal(err)
					}
				}
				_, err = stmt.Exec()
				if err != nil {
					b.Fatal(err)
				}
				stmt.Close()
			}
		})
	}
}

func BenchmarkRoundTrip_ConcurrentQueries(b *testing.B) {
	connector, err := NewConnector(makeConnStr(b).String())
	if err != nil {
		b.Fatal("Open connection failed:", err.Error())
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(10)
	b.Cleanup(func() { db.Close() })
	if err := db.Ping(); err != nil {
		b.Fatal("Ping failed:", err)
	}
	ctx := context.Background()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			var n int
			if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&n); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkRoundTrip_MixedTypes(b *testing.B) {
	db := benchmarkDB(b)
	ctx := context.Background()
	query := `SELECT
		CAST(12345 AS INT),
		CAST(9876543210 AS BIGINT),
		CAST(3.14159 AS FLOAT),
		CAST('hello world' AS NVARCHAR(100)),
		CAST(1 AS BIT),
		CAST('2024-01-15T10:30:00' AS DATETIME2),
		CAST(NULL AS NVARCHAR(50))`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var (
			intVal    int
			bigintVal int64
			floatVal  float64
			strVal    string
			boolVal   bool
			timeVal   time.Time
			nullVal   sql.NullString
		)
		err := db.QueryRowContext(ctx, query).Scan(
			&intVal, &bigintVal, &floatVal, &strVal, &boolVal, &timeVal, &nullVal,
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRoundTrip_LargePayload(b *testing.B) {
	db := benchmarkDB(b)
	ctx := context.Background()
	// 8KB string to test larger NVarChar handling
	largeStr := strings.Repeat("abcdefgh", 1024)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var result string
		err := db.QueryRowContext(ctx, "SELECT @p1", sql.Named("p1", largeStr)).Scan(&result)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRoundTrip_PreparedStmt(b *testing.B) {
	db := benchmarkDB(b)
	ctx := context.Background()
	stmt, err := db.PrepareContext(ctx, "SELECT @p1 + @p2")
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { stmt.Close() })
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var n int
		if err := stmt.QueryRowContext(ctx, 1, 2).Scan(&n); err != nil {
			b.Fatal(err)
		}
	}
}
