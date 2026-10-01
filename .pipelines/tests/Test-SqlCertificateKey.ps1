$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$source = Join-Path $PSScriptRoot '..\scripts\Generate-SqlCertificates.ps1'
$tokens = $null
$parseErrors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile($source, [ref]$tokens, [ref]$parseErrors)
if ($parseErrors.Count) { throw ($parseErrors | Out-String) }

function Get-AssignmentExpression([string]$Name) {
    $assignments = @($ast.FindAll({
        param($node)
        $node -is [System.Management.Automation.Language.AssignmentStatementAst] -and
        $node.Left -is [System.Management.Automation.Language.VariableExpressionAst] -and
        $node.Left.VariablePath.UserPath -eq $Name
    }, $true))
    if ($assignments.Count -ne 1) { throw "Expected one assignment to $Name in certificate script." }
    return [scriptblock]::Create($assignments[0].Right.Extent.Text)
}

# Execute only certificate/key operations, not SQL service, ACL, or trust-store changes.
$certStorePath = 'Cert:\CurrentUser\My'
$cert = $null
$rsaCert = $null
try {
    $cert = & (Get-AssignmentExpression 'cert')
    $certificate = Get-Item "$certStorePath\$($cert.Thumbprint)"
    $rsaCert = & (Get-AssignmentExpression 'rsaCert')
    $fileName = & (Get-AssignmentExpression 'fileName')
    if ([string]::IsNullOrWhiteSpace($fileName)) { throw 'Certificate key filename is empty.' }
    if ($fileName -ne $rsaCert.Key.UniqueName) { throw 'Certificate key filename does not match the persisted key.' }
    Write-Host "Certificate key lookup succeeded for $($rsaCert.GetType().Name)."
} finally {
    if ($null -ne $rsaCert) { $rsaCert.Dispose() }
    if ($null -ne $cert) {
        Remove-Item "$certStorePath\$($cert.Thumbprint)" -DeleteKey
        $cert.Dispose()
    }
}
