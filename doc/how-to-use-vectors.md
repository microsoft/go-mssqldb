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

Vectors can be passed directly as parameters to INSERT statements:

```go
db, err := sql.Open("sqlserver", "sqlserver://user:password@server/database")
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

// Insert a vector
v, _ := mssql.NewVector([]float32{1.0, 2.0, 3.0})
_, err = db.Exec(
    "INSERT INTO embeddings (name, embedding) VALUES (@p1, @p2)",
    "example", v,
)
if err != nil {
    log.Fatal(err)
}
```

## Reading Vectors

Vectors can be scanned from query results using the `Vector` or `NullVector` types:

```go
rows, err := db.Query("SELECT id, name, embedding FROM embeddings")
if err != nil {
    log.Fatal(err)
}
defer rows.Close()

for rows.Next() {
    var id int
    var name string
    var embedding mssql.Vector

    if err := rows.Scan(&id, &name, &embedding); err != nil {
        log.Fatal(err)
    }

    fmt.Printf("ID: %d, Name: %s, Dimensions: %d\n", id, name, embedding.Dimensions())
    fmt.Printf("Values: %v\n", embedding.Values())
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

SQL Server 2025 provides the `VECTOR_DISTANCE` function for similarity search. When using vectors as parameters to `VECTOR_DISTANCE`, you must cast the parameter to the appropriate vector type:

```go
// Create query vector
queryVector, _ := mssql.NewVector([]float32{1.0, 0.0, 0.0})

// Search for similar vectors using cosine distance
// Note: CAST is required when using a parameter in VECTOR_DISTANCE
rows, err := db.Query(`
    SELECT TOP 10 name, VECTOR_DISTANCE('cosine', embedding, CAST(@p1 AS VECTOR(3))) as distance
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

// Get values as float64 slice (for higher precision operations)
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

> **Note:** The element type is determined by the SQL Server column definition (e.g., `VECTOR(3)` for float32, `VECTOR(3, float16)` for float16), not by the Go-side `Vector` struct. When inserting vectors from Go, they are transmitted as JSON strings, and SQL Server converts them to the column's declared element type.
>
> **float16 TDS Limitation:** Currently, float16 vectors are transmitted as JSON over TDS. Binary transport for float16 is not yet available in drivers. This driver can read float16 vectors from SQL Server, which are returned as JSON and converted to float32 values in Go.



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

1. **VECTOR_DISTANCE parameters**: When passing a Go Vector as a parameter to `VECTOR_DISTANCE`, you must use `CAST(@param AS VECTOR(N))` to convert it to the appropriate vector type.

2. **Column type metadata**: When using `ColumnTypes()`, vector columns may report as `NVARCHAR` due to how parameters are encoded. The actual data is still properly handled as vectors.

3. **Maximum dimensions**: Vectors are limited to 1998 dimensions for float32 and 3996 dimensions for float16.

4. **Always Encrypted**: The Vector data type is not supported with Always Encrypted. This is a SQL Server limitation. See [Always Encrypted limitations](https://learn.microsoft.com/sql/relational-databases/security/encryption/always-encrypted-database-engine#limitations).

## Requirements

- SQL Server 2025 or later
- go-mssqldb driver version 1.9.4 or later

## See Also

- [SQL Server 2025 Vector documentation](https://learn.microsoft.com/sql/relational-databases/vectors/vectors-sql-server)
- [Vector search best practices](https://learn.microsoft.com/sql/relational-databases/vectors/vector-search-overview)
