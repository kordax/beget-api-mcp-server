# Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
# SPDX-License-Identifier: MIT

$ErrorActionPreference = "Stop"

$Repository = "kordax/beget-api-mcp-server"
$Binary = "beget-api-mcp-server"
$Version = if ($env:BEGET_MCP_VERSION) { $env:BEGET_MCP_VERSION } else { "latest" }
$InstallDir = if ($env:BEGET_MCP_INSTALL_DIR) {
    $env:BEGET_MCP_INSTALL_DIR
} else {
    Join-Path $env:LOCALAPPDATA "Programs\beget-api-mcp-server\bin"
}

if ($Version -eq "latest") {
    $Release = Invoke-RestMethod "https://api.github.com/repos/$Repository/releases/latest"
    $Version = $Release.tag_name
} elseif (-not $Version.StartsWith("v")) {
    $Version = "v$Version"
}

$Architecture = switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture) {
    "X64" { "amd64" }
    "Arm64" { "arm64" }
    default { throw "Unsupported Windows architecture: $_" }
}

$Archive = "${Binary}_${Version}_windows_${Architecture}.zip"
$ReleaseBase = "https://github.com/$Repository/releases/download/$Version"
$Temporary = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid().ToString())

try {
    New-Item -ItemType Directory -Path $Temporary | Out-Null
    Invoke-WebRequest "$ReleaseBase/$Archive" -OutFile (Join-Path $Temporary $Archive)
    Invoke-WebRequest "$ReleaseBase/checksums.txt" -OutFile (Join-Path $Temporary "checksums.txt")

    $ChecksumLine = Get-Content (Join-Path $Temporary "checksums.txt") |
        Where-Object { $_ -match "\s(?:\./)?$([regex]::Escape($Archive))$" } |
        Select-Object -First 1
    if (-not $ChecksumLine) {
        throw "Checksum is missing for $Archive"
    }

    $Expected = ($ChecksumLine -split "\s+")[0].ToLowerInvariant()
    $Actual = (Get-FileHash (Join-Path $Temporary $Archive) -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($Actual -ne $Expected) {
        throw "Checksum verification failed for $Archive"
    }

    Expand-Archive (Join-Path $Temporary $Archive) -DestinationPath $Temporary
    $Source = Join-Path $Temporary "${Binary}_${Version}_windows_${Architecture}\${Binary}.exe"
    New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
    Copy-Item $Source (Join-Path $InstallDir "${Binary}.exe") -Force

    $SkillSource = Join-Path $Temporary "${Binary}_${Version}_windows_${Architecture}\skills\beget-api"
    if (Test-Path $SkillSource) {
        $CodexHome = if ($env:CODEX_HOME) { $env:CODEX_HOME } else { Join-Path $HOME ".codex" }
        $SkillTarget = Join-Path $CodexHome "skills\beget-api"
        New-Item -ItemType Directory -Force -Path $SkillTarget | Out-Null
        Copy-Item (Join-Path $SkillSource "*") $SkillTarget -Recurse -Force
        Write-Host "Installed Codex skill to $SkillTarget"
    }

    $UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
    $PathEntries = @($UserPath -split ";" | Where-Object { $_ })
    if ($InstallDir -notin $PathEntries) {
        $UpdatedPath = (@($InstallDir) + $PathEntries) -join ";"
        [Environment]::SetEnvironmentVariable("Path", $UpdatedPath, "User")
        Write-Host "Added $InstallDir to the user PATH. Restart the terminal before first use."
    }

    Write-Host "Installed $Binary $Version to $InstallDir"
    Write-Host "Configure MCP clients with command: $Binary"
} finally {
    if (Test-Path $Temporary) {
        Remove-Item $Temporary -Recurse -Force
    }
}
