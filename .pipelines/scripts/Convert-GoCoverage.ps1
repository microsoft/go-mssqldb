$ErrorActionPreference = 'Stop'

# PowerShell@2 can supply a BOM-emitting OutputEncoding. Go's JSON decoder
# rejects that BOM on stdin; ASCII would instead corrupt non-ASCII file paths.
$previousOutputEncoding = $OutputEncoding
$previousConsoleEncoding = [Console]::OutputEncoding
try {
    $OutputEncoding = New-Object System.Text.UTF8Encoding($false)
    [Console]::OutputEncoding = $OutputEncoding
    $json = & gocov convert "$env:RESULTS_DIR\coverage.txt"
    if ($LASTEXITCODE -ne 0) { throw "gocov failed with exit code $LASTEXITCODE." }
    $xml = $json | & gocov-xml
    if ($LASTEXITCODE -ne 0) { throw "gocov-xml failed with exit code $LASTEXITCODE." }
    $xml | Set-Content -Path "$env:RESULTS_DIR\coverage.xml" -Encoding UTF8
} finally {
    $OutputEncoding = $previousOutputEncoding
    [Console]::OutputEncoding = $previousConsoleEncoding
}
