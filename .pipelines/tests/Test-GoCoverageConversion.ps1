$ErrorActionPreference = 'Stop'
$converter = Join-Path $PSScriptRoot '..\scripts\Convert-GoCoverage.ps1'
$tempDir = Join-Path ([IO.Path]::GetTempPath()) ([Guid]::NewGuid().ToString())
$previousResultsDir = $env:RESULTS_DIR
$previousEncoding = $OutputEncoding
New-Item -ItemType Directory -Path $tempDir | Out-Null
try {
    # Run from the tools module so the fixture package is available without
    # changing the driver's dependencies or requiring SQL Server.
    go test -mod=readonly "-coverprofile=$tempDir\coverage.txt" github.com/axw/gocov
    if ($LASTEXITCODE -ne 0) { throw 'Coverage fixture generation failed.' }
    $env:RESULTS_DIR = $tempDir
    $OutputEncoding = New-Object System.Text.UTF8Encoding($true)
    & $converter
    if (-not $OutputEncoding.GetPreamble().Length) {
        throw 'Converter did not restore the caller encoding.'
    }
    [xml]$report = Get-Content "$tempDir\coverage.xml" -Raw
    if (-not $report.coverage.packages.package) { throw 'Cobertura report contains no packages.' }
    Write-Host 'Coverage conversion succeeded with a BOM-emitting caller encoding.'
} finally {
    $OutputEncoding = $previousEncoding
    $env:RESULTS_DIR = $previousResultsDir
    Remove-Item -Path $tempDir -Recurse -Force
}
