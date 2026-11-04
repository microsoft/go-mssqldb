package mssql

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
)

// TestRowsAffectedWithTrigger pins RowsAffected against SqlClient. Trigger
// rows count toward the total when the trigger does not SET NOCOUNT ON, and
// stop counting when it does. Verified by running the same schema through
// SqlClient.ExecuteNonQuery on SQL Server 2025. See #204.
func TestRowsAffectedWithTrigger(t *testing.T) {
	conn, logger := open(t)
	defer conn.Close()
	defer logger.StopLogging()

	ctx := context.Background()

	suffix := fmt.Sprintf("%d", rand.Intn(999999))
	tbl := "test_ra204_" + suffix
	audit := "test_ra204_audit_" + suffix
	trg := "tr_ra204_" + suffix

	cleanup := func() {
		conn.ExecContext(ctx, "drop trigger if exists "+trg)
		conn.ExecContext(ctx, "drop table if exists "+audit)
		conn.ExecContext(ctx, "drop table if exists "+tbl)
	}
	cleanup()
	defer cleanup()

	_, err := conn.ExecContext(ctx, "create table "+tbl+" (id int primary key, value nvarchar(100))")
	if err != nil {
		t.Fatal("create table failed:", err)
	}

	_, err = conn.ExecContext(ctx, "insert into "+tbl+" values (1, 'old'), (2, 'old'), (3, 'old')")
	if err != nil {
		t.Fatal("insert failed:", err)
	}

	// Scenario 1: Basic update without trigger
	result, err := conn.ExecContext(ctx, "update "+tbl+" set value = 'test' where id = 1")
	if err != nil {
		t.Fatal("update failed:", err)
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected != 1 {
		t.Errorf("basic update: expected RowsAffected=1, got %d", rowsAffected)
	}

	// Create audit table and trigger without NOCOUNT
	_, err = conn.ExecContext(ctx, "create table "+audit+" (id int identity, action nvarchar(50))")
	if err != nil {
		t.Fatal("create audit table failed:", err)
	}
	_, err = conn.ExecContext(ctx, "create trigger "+trg+" on "+tbl+" after update as begin insert into "+audit+" (action) select 'updated' from inserted end")
	if err != nil {
		t.Fatal("create trigger failed:", err)
	}

	// Scenario 2: Update with trigger (no NOCOUNT). The trigger's rows count
	// toward the total, matching ExecuteNonQuery.
	result, err = conn.ExecContext(ctx, "update "+tbl+" set value = 'triggered' where id = 1")
	if err != nil {
		t.Fatal("triggered update failed:", err)
	}
	rowsAffected, _ = result.RowsAffected()
	if rowsAffected != 2 {
		t.Errorf("trigger without NOCOUNT: expected RowsAffected=2 (1 updated + 1 audit), got %d", rowsAffected)
	}

	// Scenario 2b: Bulk update all rows with a no-NOCOUNT trigger active:
	// 3 updated rows plus 3 audit rows.
	result, err = conn.ExecContext(ctx, "update "+tbl+" set value = 'bulk'")
	if err != nil {
		t.Fatal("bulk update without NOCOUNT failed:", err)
	}
	rowsAffected, _ = result.RowsAffected()
	if rowsAffected != 6 {
		t.Errorf("bulk update without NOCOUNT: expected RowsAffected=6 (3 updated + 3 audit), got %d", rowsAffected)
	}

	// Scenario 3: Recreate trigger with NOCOUNT
	conn.ExecContext(ctx, "drop trigger "+trg)
	_, err = conn.ExecContext(ctx, "create trigger "+trg+" on "+tbl+" after update as begin set nocount on; insert into "+audit+" (action) select 'updated' from inserted end")
	if err != nil {
		t.Fatal("create nocount trigger failed:", err)
	}

	result, err = conn.ExecContext(ctx, "update "+tbl+" set value = 'nocount' where id = 1")
	if err != nil {
		t.Fatal("nocount triggered update failed:", err)
	}
	rowsAffected, _ = result.RowsAffected()
	if rowsAffected != 1 {
		t.Errorf("trigger with NOCOUNT: expected RowsAffected=1, got %d", rowsAffected)
	}

	// Scenario 4: Update all rows with trigger
	result, err = conn.ExecContext(ctx, "update "+tbl+" set value = 'all'")
	if err != nil {
		t.Fatal("bulk update failed:", err)
	}
	rowsAffected, _ = result.RowsAffected()
	if rowsAffected != 3 {
		t.Errorf("bulk update with trigger: expected RowsAffected=3, got %d", rowsAffected)
	}
}

// TestMultiStatementBatchRowsAffected pins the sum of every counted statement
// in a batch, which is what SqlClient.ExecuteNonQuery returns for the same
// batch on SQL Server 2025.
func TestMultiStatementBatchRowsAffected(t *testing.T) {
	conn, logger := open(t)
	defer conn.Close()
	defer logger.StopLogging()

	ctx := context.Background()

	suffix := fmt.Sprintf("%d", rand.Intn(999999))
	tbl := "test_msbatch_" + suffix

	cleanup := func() {
		conn.ExecContext(ctx, "drop table if exists "+tbl)
	}
	cleanup()
	defer cleanup()

	_, err := conn.ExecContext(ctx, "create table "+tbl+" (id int primary key, value nvarchar(100))")
	if err != nil {
		t.Fatal("create table failed:", err)
	}

	// Seed 5 rows.
	_, err = conn.ExecContext(ctx, "insert into "+tbl+" values (1,'a'),(2,'b'),(3,'c'),(4,'d'),(5,'e')")
	if err != nil {
		t.Fatal("insert failed:", err)
	}

	// Multi-statement batch: first UPDATE touches 3 rows, second touches 2.
	// RowsAffected is the sum, matching ExecuteNonQuery.
	batch := "update " + tbl + " set value='x' where id <= 3; " +
		"update " + tbl + " set value='y' where id > 3"
	result, err := conn.ExecContext(ctx, batch)
	if err != nil {
		t.Fatal("multi-statement batch failed:", err)
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected != 5 {
		t.Errorf("multi-statement batch: expected RowsAffected=5 (3+2), got %d", rowsAffected)
	}
}
