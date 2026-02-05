# Go MSSQL Driver Development Container
FROM mcr.microsoft.com/devcontainers/go:1.24-bookworm

# Install go-sqlcmd (uses the go-mssqldb driver - dogfooding!)
# Also install Kerberos libs for integrated auth testing
RUN apt-get update && export DEBIAN_FRONTEND=noninteractive \
    && apt-get install -y curl libkrb5-dev \
    && apt-get clean -y && rm -rf /var/lib/apt/lists/*

# Install go-sqlcmd - the Go-based sqlcmd that uses go-mssqldb
# Pinned version for reproducibility; update periodically
ARG SQLCMD_VERSION=v1.8.2
RUN go install github.com/microsoft/go-sqlcmd/cmd/sqlcmd@${SQLCMD_VERSION}

# Install golangci-lint for code quality
# Download pre-built binary with SHA256 checksum verification (supply chain security)
# Supports both amd64 and arm64 architectures
ARG GOLANGCI_LINT_VERSION=1.64.8
ARG GOLANGCI_LINT_SHA256_AMD64=b6270687afb143d019f387c791cd2a6f1cb383be9b3124d241ca11bd3ce2e54e
ARG GOLANGCI_LINT_SHA256_ARM64=a6ab58ebcb1c48572622146cdaec2956f56871038a54ed1149f1386e287789a5
RUN ARCH=$(dpkg --print-architecture) \
    && if [ "$ARCH" = "amd64" ]; then \
         CHECKSUM="${GOLANGCI_LINT_SHA256_AMD64}"; \
       elif [ "$ARCH" = "arm64" ]; then \
         CHECKSUM="${GOLANGCI_LINT_SHA256_ARM64}"; \
       else \
         echo "Unsupported architecture: $ARCH" && exit 1; \
       fi \
    && curl -fsSLO "https://github.com/golangci/golangci-lint/releases/download/v${GOLANGCI_LINT_VERSION}/golangci-lint-${GOLANGCI_LINT_VERSION}-linux-${ARCH}.tar.gz" \
    && echo "${CHECKSUM}  golangci-lint-${GOLANGCI_LINT_VERSION}-linux-${ARCH}.tar.gz" | sha256sum -c - \
    && tar -xzf "golangci-lint-${GOLANGCI_LINT_VERSION}-linux-${ARCH}.tar.gz" \
    && mv "golangci-lint-${GOLANGCI_LINT_VERSION}-linux-${ARCH}/golangci-lint" /usr/local/bin/ \
    && rm -rf "golangci-lint-${GOLANGCI_LINT_VERSION}-linux-${ARCH}" "golangci-lint-${GOLANGCI_LINT_VERSION}-linux-${ARCH}.tar.gz" \
    && golangci-lint --version

# Install additional Go tools (pinned versions for reproducibility)
RUN go install golang.org/x/tools/gopls@v0.18.1 \
    && go install github.com/go-delve/delve/cmd/dlv@v1.24.1 \
    && go install honnef.co/go/tools/cmd/staticcheck@v0.6.1

# [Optional] Uncomment this section to install additional OS packages.
# RUN apt-get update && export DEBIAN_FRONTEND=noninteractive \
#     && apt-get -y install --no-install-recommends <your-package-list-here>
