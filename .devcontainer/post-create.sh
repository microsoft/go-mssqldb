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
    # Fallback: locate workspace by finding go.mod under /workspaces
    workspace_go_mod="$(find /workspaces -maxdepth 2 -name 'go.mod' -type f -print -quit 2>/dev/null)"
    if [ -n "$workspace_go_mod" ]; then
        cd "$(dirname "$workspace_go_mod")"
    else
        echo "Error: Could not determine workspace directory (no go.mod found under /workspaces)." >&2
        exit 1
    fi
fi

# Download Go dependencies
echo "📦 Downloading Go dependencies..."
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

# Run initial setup SQL if it exists and SQL Server is ready
if [ -f ".devcontainer/mssql/setup.sql" ]; then
    if [ "$sql_ready" = true ]; then
        echo "📋 Running setup.sql..."
        sqlcmd -S localhost -U sa -P "${SQLPASSWORD}" -C -i .devcontainer/mssql/setup.sql
    else
        echo "⚠️  Skipping setup.sql because SQL Server connection could not be verified."
    fi
fi

# Create useful aliases in a dedicated directory (safe and idempotent)
echo "🔧 Setting up helpful aliases..."
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

# sqlcmd alias using go-sqlcmd (which uses this driver!)
alias sql='sqlcmd -S localhost -U sa -P "$SQLPASSWORD" -C'

# Quick test connection
alias test-db='sqlcmd -S localhost -U sa -P "$SQLPASSWORD" -C -Q "SELECT @@VERSION"'
EOF

# Ensure aliases are sourced from .bashrc
if ! grep -q 'go-mssqldb aliases' ~/.bashrc 2>/dev/null; then
    {
        echo ''
        echo '# go-mssqldb aliases'
        echo 'if [ -f ~/.bash_aliases ]; then'
        echo '    # Source traditional aliases file if present'
        echo '    . ~/.bash_aliases'
        echo 'fi'
        echo ''
        echo 'if [ -d ~/.bash_aliases.d ]; then'
        echo '    # Source all alias snippets from ~/.bash_aliases.d'
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
echo "   sql          - Connect to SQL Server (go-sqlcmd)"
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
