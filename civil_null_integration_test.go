package mssql

import (
	"database/sql"
	"testing"
	"time"

	"github.com/golang-sql/civil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNullCivilTypesInsertAndRead(t *testing.T) {
	db := requireTestDB(t)
	ctx := testContext(t)

	// Use a single connection because temp tables are connection-scoped
	conn, err := db.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	tableName := "#test_null_civil_types"
	_, err = conn.ExecContext(ctx, `
		CREATE TABLE `+tableName+` (
			id       INT IDENTITY PRIMARY KEY,
			d        DATE         NULL,
			dt2      DATETIME2(7) NULL,
			tm       TIME(7)      NULL
		)`)
	require.NoError(t, err)

	refTime := time.Date(2025, 6, 15, 14, 30, 45, 123456700, time.UTC)
	validDate := NullDate{Date: civil.DateOf(refTime), Valid: true}
	validDateTime := NullDateTime{DateTime: civil.DateTimeOf(refTime), Valid: true}
	validTime := NullTime{Time: civil.TimeOf(refTime), Valid: true}
	nullDate := NullDate{Valid: false}
	nullDateTime := NullDateTime{Valid: false}
	nullTime := NullTime{Valid: false}

	// Insert non-null row
	_, err = conn.ExecContext(ctx,
		"INSERT INTO "+tableName+" (d, dt2, tm) VALUES (@p1, @p2, @p3)",
		validDate, validDateTime, validTime)
	require.NoError(t, err, "insert non-null row")

	// Insert null row
	_, err = conn.ExecContext(ctx,
		"INSERT INTO "+tableName+" (d, dt2, tm) VALUES (@p1, @p2, @p3)",
		nullDate, nullDateTime, nullTime)
	require.NoError(t, err, "insert null row")

	// Read back both rows
	rows, err := conn.QueryContext(ctx,
		"SELECT d, dt2, tm FROM "+tableName+" ORDER BY id")
	require.NoError(t, err)
	defer rows.Close()

	// Row 1: non-null values
	require.True(t, rows.Next(), "expected first row")
	var gotDate NullDate
	var gotDateTime NullDateTime
	var gotTime NullTime
	err = rows.Scan(&gotDate, &gotDateTime, &gotTime)
	require.NoError(t, err, "scan non-null row")

	assert.True(t, gotDate.Valid, "date should be valid")
	assert.Equal(t, validDate.Date, gotDate.Date, "date value")
	assert.True(t, gotDateTime.Valid, "datetime should be valid")
	assert.Equal(t, validDateTime.DateTime.Date, gotDateTime.DateTime.Date, "datetime date part")
	assert.Equal(t, validDateTime.DateTime.Time.Hour, gotDateTime.DateTime.Time.Hour, "datetime hour")
	assert.Equal(t, validDateTime.DateTime.Time.Minute, gotDateTime.DateTime.Time.Minute, "datetime minute")
	assert.Equal(t, validDateTime.DateTime.Time.Second, gotDateTime.DateTime.Time.Second, "datetime second")
	assert.True(t, gotTime.Valid, "time should be valid")
	assert.Equal(t, validTime.Time.Hour, gotTime.Time.Hour, "time hour")
	assert.Equal(t, validTime.Time.Minute, gotTime.Time.Minute, "time minute")
	assert.Equal(t, validTime.Time.Second, gotTime.Time.Second, "time second")

	// Row 2: null values
	require.True(t, rows.Next(), "expected second row")
	var gotNullDate NullDate
	var gotNullDateTime NullDateTime
	var gotNullTime NullTime
	err = rows.Scan(&gotNullDate, &gotNullDateTime, &gotNullTime)
	require.NoError(t, err, "scan null row")

	assert.False(t, gotNullDate.Valid, "date should be null")
	assert.False(t, gotNullDateTime.Valid, "datetime should be null")
	assert.False(t, gotNullTime.Valid, "time should be null")

	assert.False(t, rows.Next(), "expected no more rows")
	require.NoError(t, rows.Err())
}

func TestNullCivilTypesQueryRow(t *testing.T) {
	db := requireTestDB(t)
	ctx := testContext(t)

	// Query a NULL literal cast to each type
	var d NullDate
	var dt NullDateTime
	var tm NullTime
	err := db.QueryRowContext(ctx,
		"SELECT CAST(NULL AS DATE), CAST(NULL AS DATETIME2), CAST(NULL AS TIME)").
		Scan(&d, &dt, &tm)
	require.NoError(t, err)
	assert.False(t, d.Valid, "date should be null")
	assert.False(t, dt.Valid, "datetime should be null")
	assert.False(t, tm.Valid, "time should be null")

	// Query non-null literals
	err = db.QueryRowContext(ctx,
		"SELECT CAST('2025-01-15' AS DATE), CAST('2025-01-15 10:30:00' AS DATETIME2), CAST('10:30:00' AS TIME)").
		Scan(&d, &dt, &tm)
	require.NoError(t, err)
	assert.True(t, d.Valid)
	assert.Equal(t, civil.Date{Year: 2025, Month: 1, Day: 15}, d.Date)
	assert.True(t, dt.Valid)
	assert.Equal(t, 2025, dt.DateTime.Date.Year)
	assert.Equal(t, time.Month(1), dt.DateTime.Date.Month)
	assert.Equal(t, 15, dt.DateTime.Date.Day)
	assert.True(t, tm.Valid)
	assert.Equal(t, 10, tm.Time.Hour)
	assert.Equal(t, 30, tm.Time.Minute)
}

func TestNullCivilTypesTVP(t *testing.T) {
	db := requireTestDB(t)
	ctx := testContext(t)

	typeName := "test_null_civil_tvp_type"

	// Clean up any existing type (ignore error)
	db.ExecContext(ctx, "DROP TYPE IF EXISTS "+typeName)

	_, err := db.ExecContext(ctx, `
		CREATE TYPE `+typeName+` AS TABLE (
			d   DATE         NULL,
			dt2 DATETIME2(7) NULL,
			tm  TIME(7)      NULL
		)`)
	require.NoError(t, err)
	t.Cleanup(func() {
		db.ExecContext(ctx, "DROP TYPE "+typeName)
	})

	type tvpRow struct {
		D   NullDate     `tvp:"d"`
		Dt2 NullDateTime `tvp:"dt2"`
		Tm  NullTime     `tvp:"tm"`
	}

	refTime := time.Date(2025, 3, 20, 8, 15, 30, 0, time.UTC)
	param := TVP{
		TypeName: typeName,
		Value: []tvpRow{
			{
				D:   NullDate{Date: civil.DateOf(refTime), Valid: true},
				Dt2: NullDateTime{DateTime: civil.DateTimeOf(refTime), Valid: true},
				Tm:  NullTime{Time: civil.TimeOf(refTime), Valid: true},
			},
			{
				D:   NullDate{Valid: false},
				Dt2: NullDateTime{Valid: false},
				Tm:  NullTime{Valid: false},
			},
		},
	}

	rows, err := db.QueryContext(ctx,
		"SELECT d, dt2, tm FROM @tvp ORDER BY d DESC", sql.Named("tvp", param))
	require.NoError(t, err, "TVP query")
	defer rows.Close()

	// First row should be the non-null one (non-null date sorts after null)
	require.True(t, rows.Next())
	var d NullDate
	var dt NullDateTime
	var tm NullTime
	err = rows.Scan(&d, &dt, &tm)
	require.NoError(t, err, "scan TVP non-null row")
	assert.True(t, d.Valid)
	assert.Equal(t, civil.DateOf(refTime), d.Date)
	assert.True(t, dt.Valid)
	assert.True(t, tm.Valid)

	// Second row should be null
	require.True(t, rows.Next())
	err = rows.Scan(&d, &dt, &tm)
	require.NoError(t, err, "scan TVP null row")
	assert.False(t, d.Valid)
	assert.False(t, dt.Valid)
	assert.False(t, tm.Valid)

	assert.False(t, rows.Next())
	require.NoError(t, rows.Err())
}

func TestNullCivilTypesOutputParam(t *testing.T) {
	db := requireTestDB(t)
	ctx := testContext(t)

	// Use a single connection because temp stored procedures are connection-scoped
	conn, err := db.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	procName := "#test_null_civil_output"
	_, err = conn.ExecContext(ctx, `
		CREATE PROCEDURE `+procName+`
			@outDate DATE OUTPUT,
			@outDateTime DATETIME2 OUTPUT,
			@outTime TIME OUTPUT,
			@setNull BIT
		AS
		BEGIN
			IF @setNull = 1
			BEGIN
				SET @outDate = NULL
				SET @outDateTime = NULL
				SET @outTime = NULL
			END
			ELSE
			BEGIN
				SET @outDate = '2025-12-25'
				SET @outDateTime = '2025-12-25 18:00:00'
				SET @outTime = '18:00:00'
			END
		END`)
	require.NoError(t, err)

	// Test non-null output
	var d NullDate
	var dt NullDateTime
	var tm NullTime
	_, err = conn.ExecContext(ctx, procName,
		sql.Named("outDate", sql.Out{Dest: &d}),
		sql.Named("outDateTime", sql.Out{Dest: &dt}),
		sql.Named("outTime", sql.Out{Dest: &tm}),
		sql.Named("setNull", false))
	require.NoError(t, err, "exec proc with non-null output")
	assert.True(t, d.Valid, "output date should be valid")
	assert.Equal(t, civil.Date{Year: 2025, Month: 12, Day: 25}, d.Date)
	assert.True(t, dt.Valid, "output datetime should be valid")
	assert.True(t, tm.Valid, "output time should be valid")
	assert.Equal(t, 18, tm.Time.Hour)

	// Test null output
	d = NullDate{}
	dt = NullDateTime{}
	tm = NullTime{}
	_, err = conn.ExecContext(ctx, procName,
		sql.Named("outDate", sql.Out{Dest: &d}),
		sql.Named("outDateTime", sql.Out{Dest: &dt}),
		sql.Named("outTime", sql.Out{Dest: &tm}),
		sql.Named("setNull", true))
	require.NoError(t, err, "exec proc with null output")
	assert.False(t, d.Valid, "output date should be null")
	assert.False(t, dt.Valid, "output datetime should be null")
	assert.False(t, tm.Valid, "output time should be null")
}
