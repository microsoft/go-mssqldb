# Azure pipelines for go-mssqldb

## Purpose

Created by @shueybubbles, a member of the SQL Server team at Microsoft. I built these pipelines to run tests against specific configurations of SQL Server and Azure SQL Database in our internal Azure Devops subscriptions.

Each YML will be sufficiently parameterized to be runnable in other environments.

## Windows AppVeyor migration

`windows-validation.yml` runs on `RUST-PUBLIC-X64-WUS3`, demanding image
`RUST-W22-SQL25`, following the custom-pool approach used by
[mssql-rs](https://github.com/microsoft/mssql-rs/blob/main/.pipeline/templates/validation-stages.yml).
This is not the standard Azure Pipelines `windows-2025` image.

The pipeline preserves the four AppVeyor configurations with Go 1.25:

| Job | Architecture | Authentication / feature |
| --- | --- | --- |
| SQL auth | amd64 | SQL authentication, default transport |
| Always Encrypted | amd64 | SQL authentication, Windows certificate-store encryption |
| Named pipes | 386 | SQL authentication, `-tags np`, `PROTOCOL=np` |
| Shared memory / SSPI | amd64 | Windows authentication, `-tags sm`, `PROTOCOL=lpc` |

Unlike AppVeyor, it builds and tests `./...`, not just the root package. The 386
job executes 32-bit tests on the x64 agent. Each job publishes JUnit, Cobertura,
native Go coverage, and test JSON. Required connection/transport/encryption tests
must actually pass; a skipped test does not satisfy the job.

The matrix uses `maxParallel: 1`: only one Windows job runs at a time per
pipeline run, with a fresh agent for each configuration. This does not limit
concurrency between separate pipeline runs.

Reporting tools are pinned together in the separate `tools/go.mod` module and
installed with `go install -mod=readonly tool`. Its `golang.org/x/tools` version
is compatible with Go 1.25; installing `gocov@v1.2.1` independently selects an
older dependency that fails to compile with this toolchain. Keep tool dependencies
out of the driver's root module and update `tools/go.sum` alongside the tool pins.

### Agent prerequisites

- A fresh, disposable Windows agent per job; never point this pipeline at a shared
  or persistent SQL Server. Setup changes SQL logins, machine certificate stores,
  network protocols, and services.
- SQL Server 2025 **default instance** (`MSSQLSERVER`), SQL Browser, and `sqlcmd`
  installed. Unlike AppVeyor, the pipeline does not use named instance `SQL2025`;
  it does not replace named-instance discovery coverage with a new named instance.
- The agent identity must be a local administrator and already have SQL Server
  sysadmin access through Windows authentication. Setup cannot bootstrap access
  to an instance that denies that identity.
- Windows PowerShell 5.1, certificate cmdlets, and outbound access to Go downloads,
  the Go module proxy, and Azure Pipelines artifact endpoints.

Setup discovers the SQL instance registry ID, enables mixed authentication and
TCP/named pipes/shared memory, starts SQL Browser, and creates database `test`.
It generates a random, secret-masked SQL password and binds a short-lived TLS
certificate trusted only on the disposable agent. The driver's TLS defaults
remain unchanged. Bootstrap `sqlcmd -C` calls are restricted to local CI setup;
the TCP readiness probe validates the certificate.

`SQLSERVER_DSN`, `INSTANCE`, and `SQLINSTANCE` are explicitly cleared so they cannot
override the matrix's connection settings. Azure-dependent tests are not supplied
cloud credentials. The Always Encrypted tests provision their own CurrentUser
certificates separately from the SQL Server TLS certificate.

### Onboarding and cutover

1. Arrange access to `RUST-PUBLIC-X64-WUS3` and its image for the target Azure
   DevOps project; pool names are project-scoped. Confirm disposable-agent
   isolation and capacity for one active job per pipeline run with the pool owner.
2. Authorize the Azure Pipelines GitHub integration for `microsoft/go-mssqldb`
   and create a pipeline pointing at `.pipelines/windows-validation.yml`.
3. Enable GitHub fork PR validation with the appropriate maintainer-approval
   policy. Do not expose stored secrets, privileged service connections, or
   trusted agent pools to fork PRs. This pipeline needs none of those credentials.
4. Run alongside AppVeyor and confirm all four jobs execute on real Windows agents.
   PRs targeting `main` and pushes to `main` trigger the pipeline; feature-branch
   pushes do not create a second build.
5. Add the Azure pipeline's reported GitHub status as a required check before
   removing the AppVeyor requirement. Then disable the AppVeyor project, remove
   `appveyor.yml`, and update the repository's AppVeyor references.

AppVeyor is intentionally retained during onboarding. Windows coverage is published
to Azure Pipelines rather than uploaded to Codecov by this new pipeline; Linux
Codecov reporting remains unchanged. If Windows coverage must also appear in
Codecov after cutover, arrange an approved upload path for PR artifacts without
exposing a Codecov secret to fork code before retiring AppVeyor.

The result-assertion script has a standalone regression harness requiring no SQL
Server or additional test framework:

```powershell
powershell.exe -NoProfile -File .pipelines/tests/Test-WindowsTestResults.ps1
```
