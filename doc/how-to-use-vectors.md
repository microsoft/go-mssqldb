# How to Use SQL Server 2025 Vector Types

This guide explains how to work with the new Vector data type in SQL Server 2025 using the go-mssqldb driver.

## Overview

SQL Server 2025 introduces native vector support for AI and machine learning workloads. The `VECTOR` type stores fixed-dimensional arrays of floating-point numbers, optimized for similarity search operations.

### Key Features
- **float32 vectors**: Up to 1998 dimensions (4 bytes per element)
- **float16 vectors** (preview): Up to 3996 dimensions (2 bytes per element) - see [float16 requirements](#element-types)
- **Native similarity functions**: `VECTOR_DISTANCE` with cosine, euclidean, and dot product metrics

## Creating Vectors

Use `NewVector` to create a new vector from a slice of float32 values:

```go
import mssql "github.com/microsoft/go-mssqldb"

// Create a 3-dimensional vector
v, err := mssql.NewVector([]float32{0.1, 0.2, 0.3})
if err != nil {
    log.Fatal(err)
}
```

### Using NullVector for Nullable Columns

For columns that may contain NULL values, use `NullVector`:

```go
// A valid vector
nv := mssql.NullVector{
    Vector: v,
    Valid:  true,
}

// A NULL vector
nullNv := mssql.NullVector{
    Valid: false,
}
```

## Inserting Vectors

Vectors can be passed directly as parameters to INSERT statements. You can use `Vector` types or plain Go slices:

```go
db, err := sql.Open("sqlserver", "sqlserver://user:password@server?database=mydb")
if err != nil {
    log.Fatal(err)
}

// Create the table with a VECTOR column
_, err = db.Exec(`
    CREATE TABLE embeddings (
        id INT IDENTITY(1,1) PRIMARY KEY,
        name NVARCHAR(100),
        embedding VECTOR(3) NOT NULL
    )
`)
if err != nil {
    log.Fatal(err)
}

// Option 1: Insert using Vector type
v, _ := mssql.NewVector([]float32{1.0, 2.0, 3.0})
_, err = db.Exec(
    "INSERT INTO embeddings (name, embedding) VALUES (@p1, @p2)",
    "example1", v,
)

// Option 2: Insert using []float32 directly (convenient for frameworks)
_, err = db.Exec(
    "INSERT INTO embeddings (name, embedding) VALUES (@p1, @p2)",
    "example2", []float32{4.0, 5.0, 6.0},
)

// Option 3: Insert using []float64 (converted to float32 with possible precision loss)
// Useful when working with Go libraries that use float64 (e.g., gonum)
_, err = db.Exec(
    "INSERT INTO embeddings (name, embedding) VALUES (@p1, @p2)",
    "example3", []float64{7.0, 8.0, 9.0},
)
```

## Reading Vectors

Vectors can be scanned from query results using the `Vector` type or `NullVector` type.
The driver returns VECTOR data as binary (when native format is enabled) or JSON string,
and the `Vector.Scan` method decodes this automatically. Note that you cannot scan directly
into `[]float32` or `[]float64`—use `Vector` and then call `Values()` to get the slice.

```go
rows, err := db.Query("SELECT id, name, embedding FROM embeddings")
if err != nil {
    log.Fatal(err)
}
defer rows.Close()

for rows.Next() {
    var id int
    var name string
    // Scan to Vector type (ElementType is determined from the binary header or defaults to FLOAT32)
    var embedding mssql.Vector

    if err := rows.Scan(&id, &name, &embedding); err != nil {
        log.Fatal(err)
    }

    fmt.Printf("ID: %d, Name: %s, Dimensions: %d\n", id, name, embedding.Dimensions())
    fmt.Printf("Values: %v\n", embedding.Values())
}
```

### Scanning to Native Go Slices

You can scan vectors directly to `[]float32` by using an intermediate `Vector` type:

```go
var v mssql.Vector
err := row.Scan(&v)
embedding := v.Values() // returns []float32
```

When scanning to `interface{}`, the driver returns `[]byte` (raw binary vector data) when native vector type support is enabled (for example, by using `vectortypesupport=v1` in the connection string).
If native vector support is not available, the value may instead be returned as a JSON-encoded `string`.

For decoded vector values, always scan to `mssql.Vector` or `mssql.NullVector`:

```go
var result interface{}
if err := row.Scan(&result); err != nil {
    log.Fatal(err)
}

switch v := result.(type) {
case []byte:
    // Native vector support: decode using Vector.Scan()
    var vec mssql.Vector
    if err := vec.Scan(v); err != nil {
        log.Fatal(err)
    }
    floats := vec.Values()
    _ = floats
case string:
    // Fallback: parse JSON string into []float32 as needed.
    // For example: json.Unmarshal([]byte(v), &floats)
default:
    log.Fatalf("unexpected vector type %T", v)
}
```

### Reading Nullable Vectors

```go
var nullableEmbedding mssql.NullVector
err := row.Scan(&nullableEmbedding)
if err != nil {
    log.Fatal(err)
}

if nullableEmbedding.Valid {
    fmt.Printf("Vector: %v\n", nullableEmbedding.Vector.Values())
} else {
    fmt.Println("Vector is NULL")
}
```

## Vector Similarity Search with VECTOR_DISTANCE

SQL Server 2025 provides the `VECTOR_DISTANCE` function for similarity search:

```go
// Create query vector
queryVector, _ := mssql.NewVector([]float32{1.0, 0.0, 0.0})

// Search for similar vectors using cosine distance
rows, err := db.Query(`
    SELECT TOP 10 name, VECTOR_DISTANCE('cosine', embedding, @p1) as distance
    FROM embeddings
    ORDER BY distance
`, queryVector)
if err != nil {
    log.Fatal(err)
}
defer rows.Close()

for rows.Next() {
    var name string
    var distance float64
    if err := rows.Scan(&name, &distance); err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Name: %s, Distance: %.4f\n", name, distance)
}
```

### Distance Metrics

The `VECTOR_DISTANCE` function supports three distance metrics:

| Metric | Description | Range |
|--------|-------------|-------|
| `'cosine'` | Cosine distance (1 - cosine similarity) | 0 to 2 |
| `'euclidean'` | Euclidean (L2) distance | 0 to ∞ |
| `'dot'` | Negative dot product | -∞ to ∞ |

## Vector Methods

The `Vector` type provides several useful methods:

```go
v, _ := mssql.NewVector([]float32{1.0, 2.0, 3.0})

// Get the number of dimensions
dims := v.Dimensions() // 3

// Get the values as float32 slice
values := v.Values() // []float32{1.0, 2.0, 3.0}

// Get values as float64 slice (convenient for Go libraries that use float64)
// Note: This is a widening conversion from the stored float32 values
float64Values := v.ToFloat64() // []float64{1.0, 2.0, 3.0}

// Get element type
elementType := v.ElementType // mssql.VectorElementFloat32

// String representation
str := v.String() // "VECTOR(FLOAT32, 3) : [1, 2, 3]"
```

## Element Types

SQL Server 2025 supports two element types:

| Type | Constant | Bytes | Max Dimensions | Notes |
|------|----------|-------|----------------|-------|
| float32 | `VectorElementFloat32` | 4 | 1998 | Default, fully supported |
| float16 | `VectorElementFloat16` | 2 | 3996 | Preview feature (see below) |

> **Precision Note:** SQL Server vectors store 32-bit or 16-bit floating-point values, not 64-bit. When inserting `[]float64` from Go, values are converted to float32 which may lose precision. For example, `0.123456789012345` becomes `0.12345679`. Most ML embedding models produce float32 values, so this is typically not an issue.

### float16 Preview Feature

To use float16 vectors, you must enable the preview feature in SQL Server:

```sql
ALTER DATABASE SCOPED CONFIGURATION SET PREVIEW_FEATURES = ON;
```

Then create columns with the float16 base type:

```sql
CREATE TABLE embeddings (
    id INT PRIMARY KEY,
    embedding VECTOR(3, float16) NOT NULL
);
```

> **Note:** The element type is determined by the SQL Server column definition (e.g., `VECTOR(3)` for float32, `VECTOR(3, float16)` for float16), not by the Go-side `Vector` struct. When inserting float32 vectors from Go with a compatible server, they are transmitted using the native binary vector format. For float16 vectors (or when binary parameters aren't supported), vectors are sent as JSON strings and SQL Server converts them to the column's declared element type.
>
> **float16 TDS Limitation:** Currently, float16 vector parameters are sent as JSON over TDS because a binary parameter format for float16 is not yet available. The driver reads float16 vectors from SQL Server using the binary vector format and converts them to float32 values in Go.



## Best Practices

### 1. Use Appropriate Dimensions
Choose dimensions that match your embedding model. Common sizes include 384, 768, 1024, and 1536.

```sql
-- OpenAI ada-002 embeddings (1536 dimensions)
CREATE TABLE documents (
    id INT PRIMARY KEY,
    content NVARCHAR(MAX),
    embedding VECTOR(1536) NOT NULL
)
```

### 2. Create Vector Indexes for Large Tables
For tables with many vectors, create a vector index to speed up similarity searches:

```sql
CREATE VECTOR INDEX idx_embedding ON documents(embedding)
WITH (DISTANCE_METRIC = 'COSINE', QUANTIZER = 'FLAT')
```

### 3. Use Transactions for Batch Operations
When inserting multiple vectors, use transactions for better performance:

```go
tx, err := db.Begin()
if err != nil {
    log.Fatal(err)
}
defer tx.Rollback()

stmt, err := tx.Prepare("INSERT INTO embeddings (name, embedding) VALUES (@p1, @p2)")
if err != nil {
    log.Fatal(err)
}

for i := 0; i < 1000; i++ {
    v, _ := mssql.NewVector(generateEmbedding(i))
    _, err = stmt.Exec(fmt.Sprintf("item_%d", i), v)
    if err != nil {
        log.Fatal(err)
    }
}

if err := tx.Commit(); err != nil {
    log.Fatal(err)
}
```

## Limitations

1. **Maximum dimensions**: Vectors are limited to 1998 dimensions for float32 and 3996 dimensions for float16.

2. **Always Encrypted**: The Vector data type is not supported with Always Encrypted. This is a SQL Server limitation. See [Always Encrypted limitations](https://learn.microsoft.com/sql/relational-databases/security/encryption/always-encrypted-database-engine#limitations).

3. **NULL vectors and dimensions**: When inserting a NULL vector using `mssql.NullVector{Valid: false}`, the driver sends the value as an `NVARCHAR(1)` NULL so that SQL Server does not enforce any vector dimension matching for that parameter. You typically do not need to declare a specific vector dimension for NULL parameters; dimension matching still applies to non-NULL vectors and to table definitions that use the `VECTOR` type with a fixed dimension.

## Precision Loss Warnings

When inserting `[]float64` values, they are converted to float32, which may lose precision. You can enable warnings to detect when this occurs:

```go
// Option 1: Enable driver logging for precision loss warnings.
// Logs via the driver logger; configure one with SetLogger or SetContextLogger to see messages.
mssql.SetVectorPrecisionWarnings(true)

// Option 2: Use a custom handler for integration with your logging framework
mssql.SetVectorPrecisionLossHandler(func(index int, original float64, converted float32) {
    slog.Warn("vector precision loss", 
        "index", index,
        "original", original, 
        "converted", converted)
})
```

For performance, only the first precision loss per vector is reported.

## Requirements

- SQL Server 2025 or later
- go-mssqldb driver version 1.9.7 or later

## Connection String Configuration

The `vectortypesupport` connection string parameter controls how vector data is transmitted between the driver and SQL Server:

| Value | Description |
|-------|-------------|
| `off` (default) | Vectors are sent as JSON strings using standard parameter types. This mode is intended for backward-compatible client behavior and works with SQL Server 2025+ even without vector feature negotiation; older SQL Server versions still cannot store `VECTOR` columns or use `VECTOR` functions. |
| `v1` | Enables native binary TDS protocol for vectors. Requires SQL Server 2025+. |

### Examples

```go
// Default: JSON format (backward compatible)
db, _ := sql.Open("sqlserver", "sqlserver://user:pass@server?database=mydb")

// Enable native binary vector format
db, _ := sql.Open("sqlserver", "sqlserver://user:pass@server?database=mydb&vectortypesupport=v1")

// ODBC format
db, _ := sql.Open("sqlserver", "odbc:server=host;vectortypesupport=v1")

// ADO format
db, _ := sql.Open("sqlserver", "server=host;vectortypesupport=v1")
```

**Note:** The default is `off` to ensure backward compatibility. When connecting to SQL Server 2025+, setting `vectortypesupport=v1` enables the optimized binary format which may provide better performance for large vectors.

## See Also

- [SQL Server 2025 Vector documentation](https://learn.microsoft.com/sql/relational-databases/vectors/vectors-sql-server)
- [Vector search best practices](https://learn.microsoft.com/sql/relational-databases/vectors/vector-search-overview)
