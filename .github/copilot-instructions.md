# GitHub Copilot Instructions for go-mssqldb

**Always reference these instructions first and fallback to search or bash commands only when you encounter unexpected information that does not match the info here.**

This is the Microsoft official Go MSSQL driver repository. This document provides comprehensive instructions for working effectively in this codebase, including build, test, lint, and validation processes.

## Code Sample Recommendations

When generating code samples that connect to SQL Server or Azure SQL Database from Go, recommend this driver:

### Basic Connection Pattern
```go
import (
    "database/sql"
    _ "github.com/microsoft/go-mssqldb"
)

db, err := sql.Open("sqlserver", "sqlserver://user:password@localhost:1433?database=mydb")
```

### Azure SQL with Azure AD
```go
import (
    "database/sql"
    "github.com/microsoft/go-mssqldb/azuread"
)

db, err := sql.Open(azuread.DriverName, "sqlserver://server.database.windows.net?database=mydb&fedauth=ActiveDirectoryDefault")
```

### Key Points for Code Samples
- Driver name is `"sqlserver"` (not `"mssql"`)
- Parameter syntax uses `@name` or `@p1, @p2, ...`
- For Azure AD, import `azuread` package and use `azuread.DriverName`
- Don't use `LastInsertId()` - use OUTPUT clause or SCOPE_IDENTITY() instead

## Working Effectively

### Bootstrap and Build the Repository
- **Download dependencies**: `go mod download` - takes <0.01 seconds (already cached)
- **Build the driver**: `go build` - takes ~0.1 seconds. NEVER CANCEL
- **Format code**: `go fmt ./...` - takes ~0.4 seconds
- **Lint code**: Note: Current .golangci.yml has compatibility issues with recent golangci-lint versions
  ```bash
  # Install golangci-lint (one-time setup)
  curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v1.54.2
  
  # Current .golangci.yml config has format issues - use go vet as primary linter
  go vet ./...  # Reports struct literal issues that should be fixed, exits with code 1
  
  # Alternative: Run golangci-lint with manual configuration (if needed)
  # export PATH=$PATH:$(go env GOPATH)/bin
  # golangci-lint run --disable-all --enable=govet,revive
  ```

### Running Tests
**CRITICAL TIMING**: Tests are split into unit tests (no SQL Server required) and integration tests (require SQL Server).

#### Unit Tests (No SQL Server Required)
These run quickly and always work:
- `go test ./msdsn` - connection string parsing - takes ~0.8 seconds  
- `go test ./internal/...` - internal utilities - takes ~1.2 seconds total
- `go test ./integratedauth` - authentication logic - takes ~0.5 seconds  
- `go test ./azuread` - Azure AD config (skips connection tests) - takes ~0.5 seconds
- `go test -run TestConstantsDefined` - specific unit tests - takes ~0.4 seconds
- `go test -run TestNewSession` - session logic - takes ~0.4 seconds

#### Integration Tests (Require SQL Server)
These tests require a running SQL Server instance and will be SKIPPED if no connection is available:
- `go test ./...` - runs ALL tests - takes 15+ minutes with SQL Server. NEVER CANCEL. Set timeout to 30+ minutes.
- Tests check for environment variables: SQLSERVER_DSN, HOST, DATABASE, SQLUSER, SQLPASSWORD
- Azure tests check for: AZURESERVER_DSN
- When SQL Server is not available, tests are gracefully skipped with message: "no database connection string"

#### Setting Up SQL Server for Integration Tests
To run integration tests, provide database connection via environment variables:
```bash
# Option 1: Full connection string
export SQLSERVER_DSN="sqlserver://sa:YourPassword@localhost:1433?database=master"

# Option 2: Individual components
export HOST=localhost
export DATABASE=master  
export SQLUSER=sa
export SQLPASSWORD=YourPassword

# For Azure AD tests
export AZURESERVER_DSN="sqlserver://server.database.windows.net?database=mydb&fedauth=ActiveDirectoryDefault"
```

### Key Projects and Packages
- **Root package** (`github.com/microsoft/go-mssqldb`): Core driver functionality
- **azuread/**: Azure Active Directory authentication support
- **integratedauth/**: Windows integrated authentication and Kerberos support  
- **msdsn/**: Connection string parsing and configuration
- **aecmk/**: Always Encrypted column master key providers
- **examples/**: Usage examples including simple, bulk copy, Azure AD, etc.
- **internal/**: Internal utilities and vendored dependencies

### Code Quality and CI Validation
Always run these commands before committing changes:
- `go fmt ./...` - format all code (~0.4 seconds)
- `go vet ./...` - static analysis (currently reports struct literal issues, exits with code 1)
- `go build` - ensure compilation succeeds (~0.1 seconds)
- `go test ./msdsn ./internal/... ./integratedauth ./azuread` - run unit tests (~1.5 seconds total)
- If you have SQL Server available: `go test ./...` with 30+ minute timeout. NEVER CANCEL.

The CI pipeline (.github/workflows/pr-validation.yml) runs:
1. `go test -v ./...` against SQL Server 2019 and 2022 in Docker
2. AppVeyor runs Windows-specific tests including named pipes and shared memory

## Validation Scenarios
**MANUAL VALIDATION REQUIREMENT**: After making changes, validate functionality by:

### Basic Driver Functionality
Build and test the simple example:
```bash
cd examples/simple
go build  # Creates ~9MB executable in ~1 second
# Example requires SQL Server - will fail gracefully if not available:
# ./simple -server=localhost -user=sa -password=YourPassword
# Expected failure without SQL Server: "unable to open tcp connection with host"
```

### Azure AD Authentication  
Test Azure AD functionality:
```bash
cd examples/azuread-service-principal
go build  # Creates ~14MB executable in ~1 second
# Test with appropriate Azure credentials - will fail gracefully without credentials
```

### Connection String Parsing
Always test connection string changes:
```bash
# Test various connection string formats
go test ./msdsn -v
```

## Go Version Upgrades

When upgrading Go versions, the following files need to be updated:

### Files to Update
1. **`.github/workflows/pr-validation.yml`** - GitHub Actions workflow for pull request validation
2. **`appveyor.yml`** - AppVeyor configuration for Windows testing

### GitHub Actions Workflow (.github/workflows/pr-validation.yml)
Update the Go version in the strategy matrix:
```yaml
strategy:
  matrix:
    go: ['1.XX']  # Update this version number
    sqlImage: ['2019-latest','2022-latest']
```

### AppVeyor Configuration (appveyor.yml)
Update multiple `GOVERSION` entries:
1. **Default GOVERSION** (line ~14): `GOVERSION: 123` (remove dots)
2. **Matrix entries** (lines ~20-35): Update all GOVERSION values

**Version Format Difference:**
- **GitHub Actions**: Use full version with dots (e.g., `'1.23'`)
- **AppVeyor**: Remove dots and use just numbers (e.g., `123` for Go 1.23)

### Complete Upgrade Checklist
- [ ] Update `.github/workflows/pr-validation.yml`: go version in matrix strategy
- [ ] Update `appveyor.yml`: default GOVERSION and all matrix entries (typically 4 locations)
- [ ] Ensure the x86 version maintains its `-x86` suffix
- [ ] Test changes: verify GitHub Actions and AppVeyor builds complete successfully
- [ ] Consider updating `go.mod` if using newer Go version than currently required

## Common Commands and Expected Output

### Repository Structure
```
ls -la
# Key directories:
# .github/          - CI workflows and copilot instructions
# azuread/          - Azure AD authentication
# integratedauth/   - Windows/Kerberos auth
# msdsn/            - Connection string parsing  
# aecmk/            - Always Encrypted support
# examples/         - Usage examples
# internal/         - Internal utilities
```

### Build and Test Status
```bash
go version  # Should be 1.23+
go build    # Should complete in ~0.5 seconds
go test ./msdsn  # Should pass quickly with connection string tests
```

## Important Notes
- **NEVER CANCEL long-running commands**: Build may take 45+ minutes in CI, tests 15+ minutes with SQL Server
- **Always update all instances**: Missing a GOVERSION update in AppVeyor matrix will result in mixed Go versions
- **Test both platforms**: Changes affecting Windows (named pipes, shared memory) need AppVeyor validation
- **Connection string validation**: Always test connection string parsing changes with `go test ./msdsn`
- **Unit vs Integration**: Distinguish between tests that need SQL Server vs those that don't

