#!/bin/bash
set -e

echo "=== go-mssqldb Development Container Setup ==="

# Get the workspace folder name (handles different clone names)
WORKSPACE_DIR="${WORKSPACE_FOLDER:-/workspaces/${PWD##*/}}"
if [ -d "$WORKSPACE_DIR" ]; then
    cd "$WORKSPACE_DIR"
elif [ -d "/workspaces/go-mssqldb" ]; then
    cd /workspaces/go-mssqldb
else
    # Fallback: locate workspace by finding this module's go.mod under /workspaces
    workspace_go_mod="$(grep -rl 'module github.com/microsoft/go-mssqldb' /workspaces/*/go.mod 2>/dev/null | head -1)"
    if [ -n "$workspace_go_mod" ]; then
        cd "$(dirname "$workspace_go_mod")"
    else
        echo "Error: Could not determine workspace directory (no go.mod found under /workspaces)." >&2
        exit 1
    fi
fi
echo "📁 Working in: $(pwd)"

# Download Go dependencies
go mod download

# Verify build works
echo "🔨 Verifying build..."
go build ./...

# Wait for SQL Server to be ready (health check should have done this, but let's verify)
echo "🔄 Verifying SQL Server connection using go-sqlcmd (uses this driver!)..."
max_attempts=30
attempt=1
sql_ready=false
while [ $attempt -le $max_attempts ]; do
    if sqlcmd -S localhost -U sa -P "${SQLPASSWORD}" -C -Q "SELECT 1" > /dev/null 2>&1; then
        echo "✅ SQL Server is ready!"
        sql_ready=true
        break
    fi
    echo "   Waiting for SQL Server... (attempt $attempt/$max_attempts)"
    sleep 2
    attempt=$((attempt + 1))
done

if [ "$sql_ready" = false ]; then
    echo "⚠️  Warning: Could not verify SQL Server connection. Tests may fail."
fi

# Set up go-sqlcmd context so 'sqlcmd' (and the 'sql' wrapper) connect
# to the dev container's SQL Server without needing -S/-U/-P flags.
if [ "$sql_ready" = true ]; then
    echo "🔧 Configuring go-sqlcmd default context..."
    SQLCMD_PASSWORD="${SQLPASSWORD}" sqlcmd config add-user --name sa-dev --username sa --password-encryption none 2>/dev/null || true
    sqlcmd config add-endpoint --name local-dev --address localhost --port 1433 2>/dev/null || true
    sqlcmd config add-context --name devcontainer --user sa-dev --endpoint local-dev 2>/dev/null || true
    sqlcmd config use-context devcontainer 2>/dev/null || true
fi

# Run initial setup SQL if it exists and SQL Server is ready
if [ -f ".devcontainer/mssql/setup.sql" ]; then
    if [ "$sql_ready" = true ]; then
        echo "📋 Running setup.sql..."
        sqlcmd -S localhost -U sa -P "${SQLPASSWORD}" -C -i .devcontainer/mssql/setup.sql
    else
        echo "⚠️  Skipping setup.sql because SQL Server connection could not be verified."
    fi
fi

# Create convenience scripts on PATH so they work in every terminal type
# (aliases only work in interactive shells that source ~/.bashrc).
echo "🔧 Setting up helper scripts and aliases..."
mkdir -p ~/bin

# sql - go-sqlcmd (uses this driver - dogfooding!)
# Plain wrapper so all subcommands (create, query, open, etc.) and flags work.
# For a quick connected session: sql -S localhost -U sa -P $SQLPASSWORD -C
# Or just: sql query "SELECT 1"   (uses the current sqlcmd context)
cat > ~/bin/sql << 'SCRIPT'
#!/bin/bash
exec sqlcmd "$@"
SCRIPT

# sql-odbc - legacy ODBC sqlcmd (for compatibility testing)
cat > ~/bin/sql-odbc << 'SCRIPT'
#!/bin/bash
exec /opt/mssql-tools18/bin/sqlcmd "$@"
SCRIPT

# test-db - quick connection test against the dev container's SQL Server
cat > ~/bin/test-db << 'SCRIPT'
#!/bin/bash
exec sqlcmd -S localhost -U sa -P "$SQLPASSWORD" -C -Q "SELECT @@VERSION"
SCRIPT

chmod +x ~/bin/sql ~/bin/sql-odbc ~/bin/test-db

# Also set up bash aliases for the Go workflow shortcuts
mkdir -p ~/.bash_aliases.d
cat > ~/.bash_aliases.d/go-mssqldb << 'EOF'
# go-mssqldb development aliases
alias gtest='go test ./...'
alias gtest-unit='go test ./msdsn ./internal/... ./integratedauth ./azuread -v'
alias gtest-short='go test -short ./...'
alias gbuild='go build ./...'
alias gfmt='go fmt ./...'
alias gvet='go vet ./...'
alias glint='golangci-lint run'
EOF

# Ensure aliases are sourced from .bashrc
if ! grep -q 'go-mssqldb aliases' ~/.bashrc 2>/dev/null; then
    {
        echo ''
        echo '# go-mssqldb aliases'
        echo 'if [ -d ~/.bash_aliases.d ]; then'
        echo '    for f in ~/.bash_aliases.d/*; do'
        echo '        [ -r "$f" ] && . "$f"'
        echo '    done'
        echo 'fi'
    } >> ~/.bashrc
fi

echo ""
echo "=== Setup Complete! ==="
echo ""
echo "📖 Quick Reference:"
echo "   gtest        - Run all tests"
echo "   gtest-unit   - Run unit tests only (no SQL Server required)"
echo "   gtest-short  - Run short tests"
echo "   gbuild       - Build all packages"
echo "   gfmt         - Format code"
echo "   gvet         - Run go vet"
echo "   glint        - Run golangci-lint"
echo "   test-db      - Test database connection"
echo "   sql          - Connect to SQL Server (go-sqlcmd, uses this driver)"
echo "   sql-odbc     - Connect using legacy ODBC sqlcmd (compatibility testing)"
echo ""
echo "🔧 go-sqlcmd is installed and uses THIS driver (dogfooding!)"
echo ""
echo "🔗 SQL Server Connection:"
echo "   Server:   localhost,1433"
echo "   User:     sa"
echo "   Password: (from SQLPASSWORD environment variable)"
echo "   Database: master"
echo ""
echo "🧪 Environment variables are pre-configured for tests."
echo "   Run 'go test ./...' to execute the full test suite."
echo ""
