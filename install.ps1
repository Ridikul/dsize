# dsize installer for Windows (PowerShell).
#
#   irm https://raw.githubusercontent.com/Ridikul/dsize/main/install.ps1 | iex
#
# Override the install dir:  $env:BINDIR="C:\tools"; irm ... | iex
$ErrorActionPreference = 'Stop'

$repo = 'Ridikul/dsize'
$dest = if ($env:BINDIR) { $env:BINDIR } else { Join-Path $env:LOCALAPPDATA 'Programs\dsize' }

$tag = if ($env:DSIZE_VERSION) { $env:DSIZE_VERSION } else {
  (Invoke-RestMethod "https://api.github.com/repos/$repo/releases/latest").tag_name
}
if (-not $tag) { throw 'dsize: could not determine the latest release' }

$url = "https://github.com/$repo/releases/download/$tag/dsize-windows-amd64.exe"
New-Item -ItemType Directory -Force -Path $dest | Out-Null
$exe = Join-Path $dest 'dsize.exe'

Write-Host "dsize: downloading $tag (windows/amd64)…"
Invoke-WebRequest -Uri $url -OutFile $exe

# Add the install dir to the user PATH if it isn't already there.
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if ($userPath -notlike "*$dest*") {
  [Environment]::SetEnvironmentVariable('Path', "$userPath;$dest", 'User')
  Write-Host "dsize: added $dest to your PATH (open a new terminal to pick it up)."
}

Write-Host "dsize: installed to $exe. Try:  dsize --ui"
