$ErrorActionPreference = 'Stop'
$converter = Join-Path $PSScriptRoot '..\scripts\Convert-GoCoverage.ps1'
$tempDir = Join-Path ([IO.Path]::GetTempPath()) ("coverage fixture " + [Guid]::NewGuid().ToString())
$previousResultsDir = $env:RESULTS_DIR
$previousEncoding = $OutputEncoding
$previousGlobalEncoding = $global:OutputEncoding
New-Item -ItemType Directory -Path $tempDir | Out-Null
try {
    # Run from the tools module so the fixture package is available without
    # changing the driver's dependencies or requiring SQL Server.
    go test -mod=readonly "-coverprofile=$tempDir\coverage.txt" github.com/axw/gocov
    if ($LASTEXITCODE -ne 0) { throw 'Coverage fixture generation failed.' }
    $env:RESULTS_DIR = $tempDir
    $global:OutputEncoding = New-Object System.Text.UTF8Encoding($true)
    $OutputEncoding = $global:OutputEncoding
    & $converter
    if (-not $OutputEncoding.GetPreamble().Length) {
        throw 'Converter did not restore the caller encoding.'
    }
    [xml]$report = Get-Content "$tempDir\coverage.xml" -Raw
    if (-not $report.coverage.packages.package) { throw 'Cobertura report contains no packages.' }
    if (Test-Path "$tempDir\coverage.json") { throw 'Intermediate JSON was not cleaned up.' }
    Write-Host 'Coverage conversion succeeded with a BOM-emitting caller encoding.'
} finally {
    $OutputEncoding = $previousEncoding
    $global:OutputEncoding = $previousGlobalEncoding
    $env:RESULTS_DIR = $previousResultsDir
    Remove-Item -Path $tempDir -Recurse -Force
}
