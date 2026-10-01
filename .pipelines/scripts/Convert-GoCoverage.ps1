$ErrorActionPreference = 'Stop'

# Keep native output as bytes. Windows PowerShell's text pipeline can add a
# BOM or transcode JSON, even when the converter sets its local OutputEncoding.
$jsonPath = Join-Path $env:RESULTS_DIR 'coverage.json'
try {
    & $env:ComSpec /d /c 'gocov convert "%RESULTS_DIR%\coverage.txt" > "%RESULTS_DIR%\coverage.json"'
    if ($LASTEXITCODE -ne 0) { throw "gocov failed with exit code $LASTEXITCODE." }
    & $env:ComSpec /d /c 'gocov-xml < "%RESULTS_DIR%\coverage.json" > "%RESULTS_DIR%\coverage.xml"'
    if ($LASTEXITCODE -ne 0) { throw "gocov-xml failed with exit code $LASTEXITCODE." }
} finally {
    if (Test-Path -LiteralPath $jsonPath) {
        Remove-Item -LiteralPath $jsonPath
    }
}
