#Requires -Version 5.1
<#
.SYNOPSIS
    Download and install the latest localsend-cli release for Windows.

.DESCRIPTION
    Run interactively:
        irm https://raw.githubusercontent.com/tingkai-c/localsend-cli/main/install.ps1 | iex

    Environment variable overrides:
        VERSION       Tag to install (e.g. v1.3.1). Default: latest release.
        INSTALL_DIR   Install directory. Default: %LOCALAPPDATA%\Programs\localsend-cli.
        BIN_NAME      Installed binary name (without .exe). Default: localsend-cli.
#>

$ErrorActionPreference = 'Stop'
$ProgressPreference    = 'SilentlyContinue'

$Repo    = 'tingkai-c/localsend-cli'
$BinName = if ($env:BIN_NAME) { $env:BIN_NAME } else { 'localsend-cli' }

function Write-Info($msg) { Write-Host "==> $msg" }

function Get-Arch {
    switch -Wildcard ($env:PROCESSOR_ARCHITECTURE) {
        'AMD64' { return 'amd64' }
        'ARM64' { return 'arm64' }
        default {
            throw "Unsupported architecture: $($env:PROCESSOR_ARCHITECTURE) (supported: AMD64, ARM64)"
        }
    }
}

function Resolve-Version {
    if ($env:VERSION -and $env:VERSION -ne 'latest') {
        return ($env:VERSION -replace '^v', '')
    }
    $api = "https://api.github.com/repos/$Repo/releases/latest"
    $rel = Invoke-RestMethod -Headers @{ 'User-Agent' = 'localsend-cli-install' } -Uri $api
    if (-not $rel.tag_name) { throw 'Could not resolve latest release tag.' }
    return ($rel.tag_name -replace '^v', '')
}

function Get-InstallDir {
    if ($env:INSTALL_DIR) { return $env:INSTALL_DIR }
    return (Join-Path $env:LOCALAPPDATA 'Programs\localsend-cli')
}

function Add-ToUserPath($dir) {
    $current = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (-not $current) { $current = '' }
    $entries = $current -split ';' | Where-Object { $_ -ne '' }
    if ($entries -contains $dir) { return $false }
    $new = if ($current) { "$current;$dir" } else { $dir }
    [Environment]::SetEnvironmentVariable('Path', $new, 'User')
    return $true
}

try {
    $arch    = Get-Arch
    $version = Resolve-Version
    Write-Info "Installing localsend-cli $version for windows/$arch"

    $base    = "localsend-cli_${version}_windows_${arch}"
    $archive = "$base.zip"
    $baseUrl = "https://github.com/$Repo/releases/download/v$version"

    $tmp = Join-Path $env:TEMP "localsend-cli-install-$([guid]::NewGuid())"
    New-Item -ItemType Directory -Path $tmp | Out-Null

    try {
        $archivePath  = Join-Path $tmp $archive
        $checksumPath = Join-Path $tmp 'checksums.txt'

        Write-Info "Downloading $archive"
        Invoke-WebRequest -Uri "$baseUrl/$archive" -OutFile $archivePath -UseBasicParsing
        Invoke-WebRequest -Uri "$baseUrl/checksums.txt" -OutFile $checksumPath -UseBasicParsing

        Write-Info 'Verifying SHA-256'
        $expectedLine = Get-Content $checksumPath | Where-Object { $_ -match "\s$([Regex]::Escape($archive))$" } | Select-Object -First 1
        if (-not $expectedLine) { throw "Checksum for $archive not found in checksums.txt" }
        $expected = ($expectedLine -split '\s+')[0].ToLower()
        $actual   = (Get-FileHash -Algorithm SHA256 -Path $archivePath).Hash.ToLower()
        if ($expected -ne $actual) {
            throw "Checksum mismatch for ${archive}`n  expected: $expected`n  actual:   $actual"
        }

        Write-Info 'Extracting'
        $extract = Join-Path $tmp 'extract'
        Expand-Archive -LiteralPath $archivePath -DestinationPath $extract -Force

        $exe = Get-ChildItem -Path $extract -Recurse -Filter 'localsend-cli.exe' | Select-Object -First 1
        if (-not $exe) { throw 'localsend-cli.exe not found in archive' }

        $installDir = Get-InstallDir
        if (-not (Test-Path $installDir)) {
            New-Item -ItemType Directory -Path $installDir | Out-Null
        }
        $target = Join-Path $installDir "$BinName.exe"
        Write-Info "Installing to $target"
        Copy-Item -LiteralPath $exe.FullName -Destination $target -Force

        if (Add-ToUserPath $installDir) {
            Write-Host ''
            Write-Host "Note: added $installDir to your user PATH. Open a new shell to pick it up."
        }

        Write-Host ''
        Write-Host "Done. Run: $BinName --help"
    }
    finally {
        Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
    }
}
catch {
    Write-Error "install.ps1: $($_.Exception.Message)"
    exit 1
}
