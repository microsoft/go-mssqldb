[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Path,
    [Parameter(Mandatory = $true)][string[]]$RequiredTests
)

$ErrorActionPreference = 'Stop'
$passed = @{}
Get-Content -Path $Path | ForEach-Object {
    $event = $_ | ConvertFrom-Json
    if ($event.Package -eq 'github.com/microsoft/go-mssqldb' -and $event.Test) {
        if ($event.Action -eq 'pass') {
            $passed[$event.Test] = $true
        } elseif ($event.Action -in @('skip', 'fail')) {
            $passed[$event.Test] = $false
        }
    }
}
foreach ($test in $RequiredTests) {
    if (-not $passed[$test]) {
        throw "Required Windows integration test did not pass: $test"
    }
}
