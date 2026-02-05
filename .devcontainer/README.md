# go-mssqldb Development Container

This folder contains the configuration for a VS Code Dev Container / GitHub Codespaces development environment for the go-mssqldb driver.

> **⚠️ Security Note**: The SQL Server password for this devcontainer is provided via the `$SQLPASSWORD` environment variable and is a non-production, development-only default.
> For the default devcontainer setup, the password value (`MssqlDriver@2025!`) is checked into this repository for convenience; override it via environment variables or Codespaces/CI secrets for non-local use.
> When using VS Code's MSSQL extension, copy the value from `$SQLPASSWORD` when prompted.
> Never expose port 1433 outside of localhost or use these credentials in any production or shared environment.

## What's Included

- **Go 1.24** development environment with all necessary tools
- **SQL Server 2025** (Developer Edition) running in a sidecar container with AI/vector capabilities
- **Pre-configured VS Code extensions**:
  - Go (official extension)
  - MS SQL (for database management)
  - Docker
  - GitHub Copilot
  - GitLens
- **SQL Server tools**: go-sqlcmd (uses this driver - dogfooding!)
- **Go quality tools**: golangci-lint, gopls, delve debugger, staticcheck

## Quick Start

### Using VS Code (Recommended)

1. Install the [Dev Containers extension](https://marketplace.visualstudio.com/items?itemName=ms-vscode-remote.remote-containers)
2. Open this repository in VS Code
3. When prompted, click **"Reopen in Container"**, or:
   - Press `F1` and select **"Dev Containers: Reopen in Container"**
4. Wait for the container to build (first time takes ~5 minutes)
5. Start developing!

### Using GitHub Codespaces

1. Click the green **"Code"** button on the repository
2. Select **"Codespaces"** tab
3. Click **"Create codespace on main"** (or your preferred branch)
4. Wait for the environment to start

## Running Tests

Environment variables are pre-configured for running integration tests:

```bash
# Run all tests (includes SQL Server integration tests)
go test ./...

# Run unit tests only (no SQL Server required)
go test ./msdsn ./internal/... ./integratedauth ./azuread -v

# Run short tests
go test -short ./...
```

### Helpful Aliases

After the container starts, these aliases are available:

| Alias | Command |
|-------|---------|
| `gtest` | Run all tests |
| `gtest-unit` | Run unit tests only |
| `gtest-short` | Run short tests |
| `gbuild` | Build all packages |
| `gfmt` | Format code |
| `gvet` | Run go vet |
| `glint` | Run golangci-lint |
| `test-db` | Test database connection |
| `sql` | Connect to SQL Server (using go-sqlcmd) |

## SQL Server Connection

The SQL Server instance is accessible at:

- **Server**: `localhost,1433`
- **Username**: `sa`
- **Password**: `MssqlDriver@2025!`
- **Database**: `master` (default) or `GoDriverTest` (created for testing)

### Connecting with go-sqlcmd

The container includes [go-sqlcmd](https://github.com/microsoft/go-sqlcmd), which is built on this driver (dogfooding!):

```bash
# Using the alias
sql

# Or explicitly (-C trusts the self-signed certificate used by the devcontainer SQL Server)
sqlcmd -S localhost -U sa -P "MssqlDriver@2025!" -C
```

### VS Code SQL Extension

The MSSQL extension is pre-configured with a connection profile named **"mssql-container"**. Click the SQL Server icon in the Activity Bar to connect.

## Environment Variables

The following environment variables are set automatically:

| Variable | Value |
|----------|-------|
| `HOST` | `localhost` |
| `SQLUSER` | `sa` |
| `SQLPASSWORD` | `MssqlDriver@2025!` (default, overridable via Codespaces Secrets) |
| `DATABASE` | `master` |
| `SQLSERVER_DSN` | Full connection string |

## Customization

### Adding SQL Setup Scripts

The `setup.sql` script in `.devcontainer/mssql/` is executed automatically when the container starts. To run additional SQL scripts, either add them to `setup.sql` or update `post-create.sh` to execute them explicitly.

### Modifying the SA Password

The devcontainer uses environment variable substitution for the password. To change it:

**Option 1: Environment Variable Override (Recommended)**

Set `SQLPASSWORD` in your environment before opening the devcontainer:
- **GitHub Codespaces**: Add `SQLPASSWORD` as a [Codespaces Secret](https://docs.github.com/en/codespaces/managing-your-codespaces/managing-encrypted-secrets-for-your-codespaces)
- **VS Code**: Set `SQLPASSWORD` in your shell before opening VS Code

**Option 2: Edit Configuration Files**

1. Update both `SA_PASSWORD` and `MSSQL_SA_PASSWORD` defaults in `docker-compose.yml` (these must match)
2. Update `SQLPASSWORD` default in `devcontainer.json` (remoteEnv section)
3. Update the password in the `mssql.connections` settings in `devcontainer.json`

### Using a Different SQL Server Version

Edit `docker-compose.yml` and change the image tag:

```yaml
db:
  image: mcr.microsoft.com/mssql/server:2022-latest  # or 2019-latest
```

> **Note:** SQL Server 2025 is the default as it includes the latest features like JSON type support, vector search, and AI capabilities that this driver supports.

## Troubleshooting

### ARM64 (Apple Silicon) Users

SQL Server does not have native ARM64 container images. **Use GitHub Codespaces** instead - it runs on x86_64 infrastructure where SQL Server works natively.

### SQL Server not starting

Check the Docker logs:
```bash
docker logs $(docker ps -qf "name=db")
```

Common issues:
- Insufficient memory (SQL Server requires at least 2GB RAM)
- Port 1433 already in use
- ARM64 architecture issues (see above)

### Connection refused

Wait a few seconds after the container starts. SQL Server takes ~30 seconds to become ready. The health check should handle this automatically.

### Tests failing with "no database connection string"

Ensure the environment variables are set:
```bash
echo $SQLSERVER_DSN
```

If empty, try restarting the terminal or running:
```bash
source ~/.bashrc
```

## Files Reference

| File | Purpose |
|------|---------|
| `devcontainer.json` | Main configuration file |
| `docker-compose.yml` | Container orchestration (Go + SQL Server) |
| `Dockerfile` | Go development container image |
| `post-create.sh` | Setup script (runs after container creation) |
| `mssql/setup.sql` | Initial database setup script |

## Contributing

When modifying the devcontainer:

1. Test locally with `Dev Containers: Rebuild Container`
2. Ensure all tests pass: `go test ./...`
3. Verify SQL connection works: `test-db`
