# Source: microsoft/mssql-rs, commit 96d6abc79d60c3ce0afe661d79a7d03def0082fe
# .pipeline/scripts/Generate-SqlCertificates.ps1
# Only the success-message checkmark and whitespace have been normalized.
#
# MIT License
# Copyright (c) Microsoft Corporation.
#
# Permission is hereby granted, free of charge, to any person obtaining a copy
# of this software and associated documentation files (the "Software"), to deal
# in the Software without restriction, including without limitation the rights
# to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
# copies of the Software, and to permit persons to whom the Software is
# furnished to do so, subject to the following conditions:
#
# The above copyright notice and this permission notice shall be included in all
# copies or substantial portions of the Software.
#
# THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
# IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
# FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
# AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
# LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
# OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
# SOFTWARE

# Generate and Install SSL Certificates for SQL Server
# This script creates self-signed certificates for SQL Server TLS encryption

param (
    [string]$InstanceName = "MSSQLSERVER"
)

Write-Host "=== SQL Server Certificate Generation ===" -ForegroundColor Cyan
Write-Host "Instance: $InstanceName" -ForegroundColor Yellow

function Copy-To-Root-Store($cert) {
    Write-Host "Installing certificate to trusted root store..." -ForegroundColor Yellow
    try {
        $certPath = "MyRootCA.cer"
        Export-Certificate -Cert $cert -FilePath $certPath -Type CERT | Out-Null
        Import-Certificate -FilePath $certPath -CertStoreLocation "Cert:\LocalMachine\Root" | Out-Null
        Write-Host "Certificate installed to trusted root store" -ForegroundColor Green

        # Clean up temporary file
        Remove-Item -Path $certPath -Force -ErrorAction SilentlyContinue
    } catch {
        Write-Error "Failed to install certificate to root store: $($_.Exception.Message)"
    }
}

function Restart-SqlServiceSafely($serviceName) {
    # Freshly-provisioned images can leave SQL Server briefly in a start-pending /
    # not-yet-stoppable state (startup database recovery), which makes a plain
    # Restart-Service fail intermittently with "cannot be stopped"
    # (CouldNotStopService). Wait for a steady state, then stop/start with retries.
    $svc = Get-Service -Name $serviceName -ErrorAction Stop

    for ($i = 1; $i -le 30; $i++) {
        $svc.Refresh()
        if ($svc.Status -eq 'Running' -or $svc.Status -eq 'Stopped') { break }
        Write-Host "Waiting for $serviceName to reach a steady state (current: $($svc.Status))..."
        Start-Sleep -Seconds 5
    }

    for ($attempt = 1; $attempt -le 5; $attempt++) {
        try {
            $svc.Refresh()
            if ($svc.Status -ne 'Stopped') {
                Stop-Service -Name $serviceName -Force -ErrorAction Stop
                $svc.WaitForStatus('Stopped', [TimeSpan]::FromSeconds(120))
            }
            Start-Service -Name $serviceName -ErrorAction Stop
            $svc.WaitForStatus('Running', [TimeSpan]::FromSeconds(120))
            Write-Host "SQL service '$serviceName' restarted (attempt $attempt)."
            return
        } catch {
            Write-Host "Restart attempt $attempt for '$serviceName' failed: $($_.Exception.Message)"
            Start-Sleep -Seconds 10
        }
    }
    throw "Failed to restart SQL service '$serviceName' after multiple attempts."
}

function New-And-Install-Certificates($instanceName) {
    Write-Output "Instance name received is " + $instanceName
    $certStorePath  = "Cert:\LocalMachine\My"
    $sqlServiceAccount = (Get-WmiObject -Class Win32_Service | Where-Object { $_.Name -like "*$instanceName" }).StartName
    Write-Output $sqlServiceAccount

    # Dynamically detect SQL Server version
    $sqlServerKeys = Get-ChildItem "HKLM:\SOFTWARE\Microsoft\Microsoft SQL Server" -ErrorAction SilentlyContinue
    $versionNumber = $null

    foreach ($key in $sqlServerKeys) {
        if ($key.PSChildName -match "MSSQL(\d+)\.$instanceName$") {
            $versionNumber = $Matches[1]
            Write-Output "Detected SQL Server version: MSSQL$versionNumber for instance: $instanceName"
            break
        }
    }

    if ($null -eq $versionNumber) {
        throw "Could not detect SQL Server version for instance: $instanceName"
    }

    $cert = New-SelfSignedCertificate -Type SSLServerAuthentication -Subject "CN=$env:COMPUTERNAME" -FriendlyName "SQL Server Test self-signed" -DnsName "$env:COMPUTERNAME",'localhost'  -KeyAlgorithm RSA -KeyLength 2048 -Hash 'SHA256' -TextExtension '2.5.29.37={text}1.3.6.1.5.5.7.3.1' -NotAfter (Get-Date).AddMonths(24) -KeySpec KeyExchange -Provider "Microsoft RSA SChannel Cryptographic Provider" -CertStoreLocation $certStorePath

    $thumbprint = $cert.Thumbprint

    $certificate = Get-ChildItem $certStorePath | Where-Object thumbprint -eq $thumbprint

    $rsaCert = [System.Security.Cryptography.X509Certificates.RSACertificateExtensions]::GetRSAPrivateKey($certificate)

    $fileName = $rsaCert.key.UniqueName

    $path = "$env:ALLUSERSPROFILE\Microsoft\Crypto\RSA\MachineKeys\$fileName"

    $permissions = Get-Acl -Path $path

    $access_rule = New-Object System.Security.AccessControl.FileSystemAccessRule($sqlServiceAccount, 'Read', 'None', 'None', 'Allow')

    $permissions.AddAccessRule($access_rule)

    Set-Acl -Path $path -AclObject $permissions

    $registryPath = "HKLM:\SOFTWARE\Microsoft\Microsoft SQL Server\MSSQL$versionNumber.$instanceName\MSSQLServer\SuperSocketNetLib"
    Write-Output "Using registry path: $registryPath"

    Set-ItemProperty -Path $registryPath -Name "Certificate" -Value $thumbprint

    # Named instances register as MSSQL$<instance>; the default instance is MSSQLSERVER.
    if ($instanceName -eq "MSSQLSERVER") {
        $serviceName = "MSSQLSERVER"
    } else {
        $serviceName = "MSSQL`$$instanceName"
    }

    Restart-SqlServiceSafely -serviceName $serviceName
    Copy-To-Root-Store -cert $cert
}

Get-WmiObject -Class Win32_Service

(Get-WmiObject -Class Win32_Service | Where-Object { $_.Name -like "*MSSQLSERVER" }).StartName

New-And-Install-Certificates -instanceName $InstanceName
