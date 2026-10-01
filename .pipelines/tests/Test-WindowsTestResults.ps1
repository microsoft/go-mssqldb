$ErrorActionPreference = 'Stop'
$assertScript = Join-Path $PSScriptRoot '..\scripts\Assert-WindowsTestResults.ps1'
$tempDir = Join-Path ([IO.Path]::GetTempPath()) ([Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $tempDir | Out-Null

function Test-Result {
    param(
        [string]$Name,
        [object[]]$Events,
        [string[]]$Required = @('TestConnect'),
        [string]$ExpectedError = ''
    )
    $path = Join-Path $tempDir "$Name.json"
    $Events | ForEach-Object { $_ | ConvertTo-Json -Compress } | Set-Content -Path $path
    $actualError = ''
    try {
        & $assertScript -Path $path -RequiredTests $Required
    } catch {
        $actualError = $_.Exception.Message
    }
    if ($actualError -ne $ExpectedError) {
        throw "${Name}: expected error '$ExpectedError', got '$actualError'."
    }
}

$root = 'github.com/microsoft/go-mssqldb'
$missing = 'Required Windows integration test did not pass: TestConnect'
try {
    Test-Result -Name passed -Events @(
        @{ Package = $root; Action = 'start' }
        @{ Package = $root; Test = 'TestConnect'; Action = 'pass' }
        @{ Package = $root; Action = 'pass' }
    )
    Test-Result -Name skipped -Events @(
        @{ Package = $root; Test = 'TestConnect'; Action = 'skip' }
    ) -ExpectedError $missing
    Test-Result -Name failed -Events @(
        @{ Package = $root; Test = 'TestConnect'; Action = 'fail' }
    ) -ExpectedError $missing
    Test-Result -Name absent -Events @(
        @{ Package = $root; Action = 'pass' }
    ) -ExpectedError $missing
    Test-Result -Name wrongPackage -Events @(
        @{ Package = "$root/other"; Test = 'TestConnect'; Action = 'pass' }
    ) -ExpectedError $missing
    Test-Result -Name outputIsNotPass -Events @(
        @{ Package = $root; Test = 'TestConnect'; Action = 'output'; Output = 'PASS' }
    ) -ExpectedError $missing
    Test-Result -Name failedAfterPass -Events @(
        @{ Package = $root; Test = 'TestConnect'; Action = 'pass' }
        @{ Package = $root; Test = 'TestConnect'; Action = 'fail' }
    ) -ExpectedError $missing
    Test-Result -Name namedPipe -Required @('TestConnect', 'TestNamedPipeConnection') -Events @(
        @{ Package = $root; Test = 'TestConnect'; Action = 'pass' }
        @{ Package = $root; Test = 'TestNamedPipeConnection'; Action = 'pass' }
    )
    Test-Result -Name sharedMemorySkipped -Required @('TestConnect', 'TestSharedMemoryConnection') -Events @(
        @{ Package = $root; Test = 'TestConnect'; Action = 'pass' }
        @{ Package = $root; Test = 'TestSharedMemoryConnection'; Action = 'skip' }
    ) -ExpectedError 'Required Windows integration test did not pass: TestSharedMemoryConnection'
    $encrypted = 'TestAlwaysEncryptedE2E/MSSQL_CERTIFICATE_STORE'
    Test-Result -Name certificateStore -Required @('TestConnect', $encrypted) -Events @(
        @{ Package = $root; Test = 'TestConnect'; Action = 'pass' }
        @{ Package = $root; Test = $encrypted; Action = 'pass' }
    )
    Test-Result -Name encryptionParentOnly -Required @($encrypted) -Events @(
        @{ Package = $root; Test = 'TestAlwaysEncryptedE2E'; Action = 'pass' }
    ) -ExpectedError "Required Windows integration test did not pass: $encrypted"
    Write-Host 'All 11 Windows test-result assertions passed.'
} finally {
    Remove-Item -Path $tempDir -Recurse -Force
}
