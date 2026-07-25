# Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
# SPDX-License-Identifier: MIT

$ErrorActionPreference = "Stop"

$ProjectRoot = Split-Path -Parent $PSScriptRoot
$Installer = Join-Path $ProjectRoot "install.ps1"
$TestRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("beget-api-installer-test-" + [System.Guid]::NewGuid())
$EnvironmentNames = @("BEGET_MCP_VERSION", "BEGET_MCP_INSTALL_DIR")
$SavedEnvironment = @{}
foreach ($Name in $EnvironmentNames) {
    $SavedEnvironment[$Name] = [Environment]::GetEnvironmentVariable($Name, "Process")
}

function Assert-True {
    param(
        [Parameter(Mandatory = $true)]
        [bool] $Condition,
        [Parameter(Mandatory = $true)]
        [string] $Message
    )

    if (-not $Condition) {
        throw "installer test: $Message"
    }
}

$Architecture = switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture) {
    "X64" { "amd64" }
    "Arm64" { "arm64" }
    default { throw "Unsupported test architecture: $_" }
}

$global:InstallerTestArchive = ""
$global:InstallerTestDownloadURLs = @()
$global:InstallerTestReleaseURLs = @()
$global:InstallerTestLatestVersion = ""

function global:Invoke-WebRequest {
    param(
        [Parameter(Position = 0, Mandatory = $true)]
        [string] $Uri,
        [Parameter(Mandatory = $true)]
        [string] $OutFile
    )

    $global:InstallerTestDownloadURLs += $Uri
    if ($Uri.EndsWith("/checksums.txt")) {
        Set-Content -LiteralPath $OutFile -Value (("0" * 64) + "  " + $global:InstallerTestArchive)
        return
    }

    [System.IO.File]::WriteAllText($OutFile, "invalid archive fixture")
}

function global:Invoke-RestMethod {
    param(
        [Parameter(Position = 0, Mandatory = $true)]
        [string] $Uri
    )

    $global:InstallerTestReleaseURLs += $Uri
    return [PSCustomObject]@{ tag_name = $global:InstallerTestLatestVersion }
}

function Test-ChecksumFailure {
    param(
        [Parameter(Mandatory = $true)]
        [string] $RequestedVersion,
        [Parameter(Mandatory = $true)]
        [string] $ResolvedVersion,
        [Parameter(Mandatory = $true)]
        [bool] $ExpectReleaseLookup
    )

    $CaseRoot = Join-Path $TestRoot $ResolvedVersion
    $InstallDir = Join-Path $CaseRoot "bin"
    New-Item -ItemType Directory -Force -Path $CaseRoot | Out-Null

    $env:BEGET_MCP_VERSION = $RequestedVersion
    $env:BEGET_MCP_INSTALL_DIR = $InstallDir
    $global:InstallerTestArchive = "beget-api-mcp-server_${ResolvedVersion}_windows_${Architecture}.zip"
    $global:InstallerTestDownloadURLs = @()
    $global:InstallerTestReleaseURLs = @()
    $global:InstallerTestLatestVersion = $ResolvedVersion

    $FailedAsExpected = $false
    try {
        & $Installer
    } catch {
        $FailedAsExpected = $_.Exception.Message -match "Checksum verification failed"
    }

    Assert-True $FailedAsExpected "PowerShell installer accepted an invalid checksum"
    $ExpectedBase = "https://github.com/kordax/beget-api-mcp-server/releases/download/$ResolvedVersion"
    Assert-True ($global:InstallerTestDownloadURLs -contains "$ExpectedBase/$global:InstallerTestArchive") "archive URL does not match the resolved version and architecture"
    Assert-True ($global:InstallerTestDownloadURLs -contains "$ExpectedBase/checksums.txt") "checksums URL does not match the resolved version"
    Assert-True (-not (Test-Path (Join-Path $InstallDir "beget-api-mcp-server.exe"))) "binary was installed after checksum failure"

    $ExpectedReleaseURL = "https://api.github.com/repos/kordax/beget-api-mcp-server/releases/latest"
    if ($ExpectReleaseLookup) {
        Assert-True ($global:InstallerTestReleaseURLs.Count -eq 1) "latest release endpoint was not called exactly once"
        Assert-True ($global:InstallerTestReleaseURLs[0] -eq $ExpectedReleaseURL) "latest release endpoint is incorrect"
    } else {
        Assert-True ($global:InstallerTestReleaseURLs.Count -eq 0) "explicit version unexpectedly queried the latest release"
    }
}

try {
    New-Item -ItemType Directory -Force -Path $TestRoot | Out-Null
    Test-ChecksumFailure -RequestedVersion "latest" -ResolvedVersion "v9.8.7" -ExpectReleaseLookup $true
    Test-ChecksumFailure -RequestedVersion "0.7.0" -ResolvedVersion "v0.7.0" -ExpectReleaseLookup $false
    Write-Host "PowerShell installer behavioral tests passed"
} finally {
    foreach ($Name in $EnvironmentNames) {
        [Environment]::SetEnvironmentVariable($Name, $SavedEnvironment[$Name], "Process")
    }
    Remove-Item -Path Function:\Invoke-WebRequest -ErrorAction SilentlyContinue
    Remove-Item -Path Function:\Invoke-RestMethod -ErrorAction SilentlyContinue
    if (Test-Path $TestRoot) {
        Remove-Item $TestRoot -Recurse -Force
    }
}
