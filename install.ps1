# Installs the sqlmcp CLI on Windows:
#
#   irm https://raw.githubusercontent.com/tmphost/sql-mcp/main/install.ps1 | iex
#
# Environment:
#   SQLMCP_VERSION       release tag to install, such as v1.2.3 (default: latest)
#   SQLMCP_INSTALL_DIR   where to put sqlmcp.exe (default: %LOCALAPPDATA%\sqlmcp\bin)
#   SQLMCP_DOWNLOAD_URL  base URL holding the release assets, for mirrors and testing

# A script block, so piping into iex leaves no variables or preferences
# behind in the caller's session.
& {
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue' # Invoke-WebRequest is far slower with the progress bar.

    $repo = 'tmphost/sql-mcp'

    $arch = switch ($env:PROCESSOR_ARCHITECTURE) {
        'AMD64' { 'amd64' }
        'ARM64' { 'arm64' }
        default { throw "unsupported architecture $env:PROCESSOR_ARCHITECTURE" }
    }
    $version = if ($env:SQLMCP_VERSION) { $env:SQLMCP_VERSION } else { 'latest' }
    $asset = "sqlmcp_windows_$arch.zip"
    $base = if ($env:SQLMCP_DOWNLOAD_URL) {
        $env:SQLMCP_DOWNLOAD_URL.TrimEnd('/')
    } elseif ($version -eq 'latest') {
        "https://github.com/$repo/releases/latest/download"
    } else {
        "https://github.com/$repo/releases/download/$version"
    }
    $dir = if ($env:SQLMCP_INSTALL_DIR) { $env:SQLMCP_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'sqlmcp\bin' }

    $tmp = Join-Path ([IO.Path]::GetTempPath()) ("sqlmcp-" + [Guid]::NewGuid())
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
        Write-Host "Downloading $asset ($version)..."
        Invoke-WebRequest -UseBasicParsing -Uri "$base/$asset" -OutFile (Join-Path $tmp $asset)
        Invoke-WebRequest -UseBasicParsing -Uri "$base/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt')

        $line = Get-Content (Join-Path $tmp 'checksums.txt') | Where-Object { ($_ -split '\s+')[1] -in @($asset, "*$asset") } | Select-Object -First 1
        if (-not $line) { throw "$asset is not listed in checksums.txt" }
        $want = ($line -split '\s+')[0].ToLower()
        $got = (Get-FileHash -Algorithm SHA256 (Join-Path $tmp $asset)).Hash.ToLower()
        if ($want -ne $got) { throw "checksum mismatch for ${asset}: expected $want, got $got" }

        Expand-Archive -Path (Join-Path $tmp $asset) -DestinationPath $tmp -Force
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
        Copy-Item -Path (Join-Path $tmp 'sqlmcp.exe') -Destination (Join-Path $dir 'sqlmcp.exe') -Force
        Write-Host "Installed sqlmcp to $dir\sqlmcp.exe"

        # The user PATH, not the machine one, which needs admin rights.
        $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        $parts = @($userPath -split ';' | Where-Object { $_ })
        if ($parts -notcontains $dir) {
            [Environment]::SetEnvironmentVariable('Path', (($parts + $dir) -join ';'), 'User')
            $env:Path = "$env:Path;$dir"
            Write-Host "Added $dir to your user PATH; open a new terminal to use it everywhere."
        }
        Write-Host 'Run "sqlmcp login --server https://your-sql-mcp-server" to get started.'
    } finally {
        Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
    }
}
