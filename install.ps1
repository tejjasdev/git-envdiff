# git-envdiff installer for Windows.
#
#   irm https://github.com/tejjasdev/git-envdiff/releases/latest/download/install.ps1 | iex
#
# Mirrors install.sh: resolves the latest release tag, downloads the
# Windows archive and checksums.txt, verifies the SHA-256 checksum,
# extracts git-envdiff.exe into %LOCALAPPDATA%\Programs\git-envdiff,
# adds that directory to your user PATH, and writes a small receipt so
# `git-envdiff update` can update itself later.
#
# NOTE: this script is implemented but not yet verified on real Windows
# hardware — see the repository README's "what's not yet verified"
# section. Please open an issue if something here doesn't work as
# described.

$ErrorActionPreference = "Stop"

$Repo = "tejjasdev/git-envdiff"
$InstallDir = if ($env:GIT_ENVDIFF_INSTALL_DIR) { $env:GIT_ENVDIFF_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "Programs\git-envdiff" }
$Version = if ($env:GIT_ENVDIFF_VERSION) { $env:GIT_ENVDIFF_VERSION } else { "latest" }
# Overridable for mirrors and for testing this script against a local server.
$GitHubBase = if ($env:GIT_ENVDIFF_GITHUB_BASE) { $env:GIT_ENVDIFF_GITHUB_BASE } else { "https://github.com" }

function Resolve-Arch {
    $arch = $env:PROCESSOR_ARCHITECTURE
    switch ($arch) {
        "AMD64" { return "amd64" }
        "ARM64" { return "arm64" }
        default { throw "Unsupported architecture: $arch" }
    }
}

function Resolve-Tag {
    if ($Version -ne "latest") { return $Version }
    # Ask GitHub where /releases/latest redirects to, without following it
    # (this avoids the anonymous GitHub API rate limit). Windows PowerShell
    # 5.1 and PowerShell 7 expose the redirect differently, so handle both.
    try {
        Invoke-WebRequest -Uri "$GitHubBase/$Repo/releases/latest" -MaximumRedirection 0 -UseBasicParsing -ErrorAction Stop | Out-Null
    }
    catch {
        $resp = $_.Exception.Response
        $location = $null
        if ($resp -and $resp.Headers) {
            if ($resp.Headers.Location) { $location = $resp.Headers.Location.ToString() }
            elseif ($resp.Headers["Location"]) { $location = $resp.Headers["Location"] }
        }
        if ($location) { return ($location -split "/")[-1] }
    }
    throw "Could not determine the latest release tag."
}

function Main {
    $arch = Resolve-Arch
    $tag = Resolve-Tag
    $version = $tag.TrimStart("v")
    $asset = "git-envdiff_${version}_windows_${arch}.zip"
    $baseUrl = "$GitHubBase/$Repo/releases/download/$tag"

    $tmpDir = Join-Path $env:TEMP ("git-envdiff-install-" + [System.Guid]::NewGuid().ToString("N"))
    New-Item -ItemType Directory -Path $tmpDir | Out-Null
    try {
        $assetPath = Join-Path $tmpDir $asset
        $checksumsPath = Join-Path $tmpDir "checksums.txt"

        Write-Host "Downloading git-envdiff $tag for windows/$arch..."
        Invoke-WebRequest -Uri "$baseUrl/$asset" -OutFile $assetPath -UseBasicParsing
        Invoke-WebRequest -Uri "$baseUrl/checksums.txt" -OutFile $checksumsPath -UseBasicParsing

        Write-Host "Verifying checksum..."
        $expectedLine = Select-String -Path $checksumsPath -Pattern ([Regex]::Escape($asset)) | Select-Object -First 1
        if (-not $expectedLine) { throw "No checksum entry found for $asset" }
        $expected = ($expectedLine.Line -split "\s+")[0]
        $got = (Get-FileHash -Path $assetPath -Algorithm SHA256).Hash.ToLower()
        if ($expected.ToLower() -ne $got) {
            throw "Checksum mismatch for $asset (expected $expected, got $got)"
        }

        Write-Host "Extracting..."
        $extractDir = Join-Path $tmpDir "extracted"
        Expand-Archive -Path $assetPath -DestinationPath $extractDir

        $binary = Get-ChildItem -Path $extractDir -Recurse -Filter "git-envdiff.exe" | Select-Object -First 1
        if (-not $binary) { throw "git-envdiff.exe not found inside $asset" }

        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
        $dest = Join-Path $InstallDir "git-envdiff.exe"
        Copy-Item -Path $binary.FullName -Destination $dest -Force

        $receiptDir = Join-Path $env:LOCALAPPDATA "git-envdiff"
        New-Item -ItemType Directory -Path $receiptDir -Force | Out-Null
        $receipt = @{
            channel      = "script"
            binary_path  = $dest
            installed_at = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
        } | ConvertTo-Json
        Set-Content -Path (Join-Path $receiptDir "receipt.json") -Value $receipt -Encoding UTF8

        Write-Host ""
        Write-Host "git-envdiff $tag installed to $dest"

        $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
        if ($userPath -notlike "*$InstallDir*") {
            [Environment]::SetEnvironmentVariable("Path", "$userPath;$InstallDir", "User")
            Write-Host ""
            Write-Host "Added $InstallDir to your user PATH. Restart your terminal for this to take effect."
        }

        Write-Host ""
        Write-Host "Next step - run this once per machine (in a new terminal):"
        Write-Host "  git-envdiff install"
    }
    finally {
        Remove-Item -Path $tmpDir -Recurse -Force -ErrorAction SilentlyContinue
    }
}

Main
