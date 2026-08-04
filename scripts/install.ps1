param(
  [string]$Version = $env:WECHATLOOM_VERSION,
  [string]$InstallDir = $env:WECHATLOOM_INSTALL_DIR,
  [string]$Repository = "wechatloom/wechatloom"
)

$ErrorActionPreference = "Stop"
if (-not $Version) {
  $release = Invoke-RestMethod "https://api.github.com/repos/$Repository/releases/latest"
  $Version = $release.tag_name.TrimStart("v")
}
if (-not $InstallDir) {
  $InstallDir = Join-Path $env:LOCALAPPDATA "WeChatLoom\bin"
}
$arch = if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq "X64") { "amd64" } else { throw "unsupported Windows architecture" }
$asset = "wechatloom_${Version}_windows_${arch}.exe"
$baseUrl = "https://github.com/$Repository/releases/download/v$Version"
$temporary = Join-Path ([System.IO.Path]::GetTempPath()) ("wechatloom-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $temporary | Out-Null
try {
  $binary = Join-Path $temporary $asset
  $checksums = Join-Path $temporary "SHA256SUMS"
  Invoke-WebRequest "$baseUrl/$asset" -OutFile $binary
  Invoke-WebRequest "$baseUrl/SHA256SUMS" -OutFile $checksums
  $expected = (Get-Content $checksums | Where-Object { $_ -match "\s+$([regex]::Escape($asset))$" } | Select-Object -First 1).Split()[0].ToLowerInvariant()
  $actual = (Get-FileHash -Algorithm SHA256 $binary).Hash.ToLowerInvariant()
  if (-not $expected -or $expected -ne $actual) { throw "checksum verification failed; nothing was installed" }
  New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
  Move-Item -Force $binary (Join-Path $InstallDir "wechatloom.exe")
  Write-Output "installed wechatloom $Version at $InstallDir\wechatloom.exe"
} finally {
  Remove-Item -Recurse -Force $temporary -ErrorAction SilentlyContinue
}
