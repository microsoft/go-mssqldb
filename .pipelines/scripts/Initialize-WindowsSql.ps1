# Requires a disposable RUST-W22-SQL25 agent and Windows-authenticated SQL sysadmin access.
[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

Get-Command sqlcmd -ErrorAction Stop | Out-Null
$service = Get-Service -Name MSSQLSERVER -ErrorAction Stop
$instanceId = Get-ItemPropertyValue `
    -Path 'HKLM:\SOFTWARE\Microsoft\Microsoft SQL Server\Instance Names\SQL' -Name MSSQLSERVER
if ($instanceId -notlike 'MSSQL17.*') {
    throw "Expected SQL Server 2025 default instance, found $instanceId."
}
$sqlRoot = "HKLM:\SOFTWARE\Microsoft\Microsoft SQL Server\$instanceId\MSSQLServer"
$networkRoot = "$sqlRoot\SuperSocketNetLib"

function Invoke-SqlcmdExitCode {
    param([string[]]$SqlArgs)
    # Windows PowerShell can turn native stderr into a terminating error before
    # callers inspect the exit code. Scope this preference to the native call.
    $ErrorActionPreference = 'Continue'
    & sqlcmd @SqlArgs *> $null
    return $LASTEXITCODE
}

function Wait-Sql {
    param([string[]]$ConnectionArgs)
    $deadline = (Get-Date).AddMinutes(3)
    do {
        # sqlcmd's nonzero status is expected while SQL recovers after a restart.
        $code = Invoke-SqlcmdExitCode -SqlArgs ($ConnectionArgs + @('-b', '-l', '5', '-t', '5', '-Q', 'SELECT 1'))
        if ($code -eq 0) { return }
        Start-Sleep -Seconds 5
    } while ((Get-Date) -lt $deadline)
    throw 'SQL Server did not accept the configured connection within three minutes.'
}

function Invoke-Sql {
    param([string]$Query)
    for ($attempt = 1; $attempt -le 5; $attempt++) {
        # Do not echo SQL text or sqlcmd output: the login batch contains a password.
        $code = Invoke-SqlcmdExitCode -SqlArgs @('-S', 'lpc:localhost', '-E', '-C', '-b', '-l', '5', '-t', '30', '-Q', $Query)
        if ($code -eq 0) { return }
        Write-Warning "SQL setup batch failed (exit $code, attempt $attempt/5)."
        if ($attempt -lt 5) { Start-Sleep -Seconds (5 * $attempt) }
    }
    throw 'SQL setup failed. The agent identity must have sysadmin access to the default instance.'
}

Set-ItemProperty -Path $sqlRoot -Name LoginMode -Value 2
foreach ($protocol in @('Tcp', 'Np', 'Sm')) {
    Set-ItemProperty -Path "$networkRoot\$protocol" -Name Enabled -Value 1
}
Set-ItemProperty -Path "$networkRoot\Tcp" -Name ListenOnAllIPs -Value 1
Set-ItemProperty -Path "$networkRoot\Tcp\IPAll" -Name TcpDynamicPorts -Value ''
Set-ItemProperty -Path "$networkRoot\Tcp\IPAll" -Name TcpPort -Value '1433'
Set-ItemProperty -Path "$networkRoot\Np" -Name PipeName -Value '\\.\pipe\sql\query'
Set-Service -Name SQLBrowser -StartupType Automatic
Start-Service -Name SQLBrowser

$deadline = (Get-Date).AddMinutes(2)
while ($service.Status -notin @('Running', 'Stopped')) {
    if ((Get-Date) -ge $deadline) { throw 'SQL service did not reach a steady state.' }
    Start-Sleep -Seconds 2
    $service.Refresh()
}
if ($service.Status -eq 'Running') {
    Stop-Service -Name MSSQLSERVER -Force
    $service.WaitForStatus('Stopped', [TimeSpan]::FromMinutes(2))
}
Start-Service -Name MSSQLSERVER
$service.WaitForStatus('Running', [TimeSpan]::FromMinutes(2))
Wait-Sql -ConnectionArgs @('-S', 'lpc:localhost', '-E', '-C')

$bytes = New-Object byte[] 24
$rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
try {
    $rng.GetBytes($bytes)
} finally {
    $rng.Dispose()
}
# Hex avoids SQL quoting and URL escaping; the suffix ensures all password character classes.
$password = [BitConverter]::ToString($bytes).Replace('-', '') + 'aA1!'
Write-Host "##vso[task.setvariable variable=WindowsSqlPassword;issecret=true]$password"
Invoke-Sql -Query "ALTER LOGIN sa WITH PASSWORD = '$password'; ALTER LOGIN sa ENABLE;"
Invoke-Sql -Query "IF DB_ID(N'test') IS NULL CREATE DATABASE test;"
Invoke-Sql -Query "IF IS_SRVROLEMEMBER(N'sysadmin') <> 1 THROW 50000, 'Agent identity must be sysadmin', 1;"

$env:SQLCMDPASSWORD = $password
try {
    Wait-Sql -ConnectionArgs @('-S', 'tcp:localhost,1433', '-U', 'sa', '-N', '-d', 'test')
    Wait-Sql -ConnectionArgs @('-S', 'np:\\.\pipe\sql\query', '-U', 'sa', '-C', '-d', 'test')
    Wait-Sql -ConnectionArgs @('-S', 'lpc:localhost', '-E', '-C', '-d', 'test')
} finally {
    Remove-Item Env:\SQLCMDPASSWORD
}
Write-Host 'SQL Server 2025 is ready for TCP, named pipes, and shared-memory tests.'
