#requires -Version 5.1
[CmdletBinding()]
param(
    [string]$Tag, [string]$InstallDir,
    [switch]$NoBrowser, [switch]$Plan, [switch]$DownloadOnly,
    [switch]$InstallOnly, [switch]$CheckOnly, [switch]$StableOnly
)
$ErrorActionPreference = 'Stop'

function Assert-PlainRouterPath([string]$Path) {
    if ($Path -notmatch '^[A-Za-z]:[\\/]') { throw 'Use an absolute local drive path.' }
    foreach ($part in $Path.Substring(2).Replace('/', '\').Split('\')) {
        if ($part -eq '.' -or $part -eq '..' -or $part.EndsWith('.') -or $part.EndsWith(' ')) { throw 'Ambiguous path component.' }
    }
    $cursor = [IO.Path]::GetFullPath($Path)
    while ($cursor) {
        if ((Test-Path -LiteralPath $cursor) -and ((Get-Item -LiteralPath $cursor -Force).Attributes -band [IO.FileAttributes]::ReparsePoint)) {
            throw "Links or reparse points are unsupported: $cursor"
        }
        $parent = [IO.Path]::GetDirectoryName($cursor)
        if ($parent -eq $cursor) { break }; $cursor = $parent
    }
}

function Invoke-RouterJson([string]$Exe, [string[]]$Arguments) {
    $result = & $Exe @Arguments 2>&1
    if ($LASTEXITCODE -ne 0) { throw ($result -join [Environment]::NewLine) }
    return ($result -join [Environment]::NewLine) | ConvertFrom-Json
}

function Select-RouterRelease($Releases, [string]$RequestedTag, [bool]$Stable) {
    $matches = @($Releases | Where-Object {
        -not $_.draft -and $_.tag_name -cmatch '^wefto-v[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?(?:\+[A-Za-z0-9.-]+)?$' -and
        (-not $RequestedTag -or $_.tag_name -ceq $RequestedTag) -and (-not $Stable -or -not $_.prerelease)
    } | Sort-Object { [DateTime]$_.published_at } -Descending)
    foreach ($release in $matches) {
        $ver = $release.tag_name.Substring(7)
        $required = @("wefto-$ver-windows-amd64.zip", "wefto-$ver-windows-amd64.manifest.json", 'install-windows.ps1', 'SHA256SUMS')
        if (@($required | Where-Object { $n = $_; @($release.assets | Where-Object { $_.name -ceq $n }).Count -ne 1 }).Count -eq 0) { return $release }
    }
    return $null
}

function Get-RouterRelease([string]$RequestedTag, [bool]$Stable) {
    $headers = @{ 'User-Agent' = 'api-router-start-skill'; Accept = 'application/vnd.github+json' }
    $base = 'https://api.github.com/repos/XHToken/magpie'
    if ($RequestedTag) {
        if ($RequestedTag -cnotmatch '^wefto-v[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?(?:\+[A-Za-z0-9.-]+)?$') { throw 'Expected an wefto-vVERSION release tag.' }
        $pageData = Invoke-RestMethod -Uri ($base + '/releases/tags/' + [Uri]::EscapeDataString($RequestedTag)) -Headers $headers -TimeoutSec 30
        $releases = @($pageData)
        return Select-RouterRelease $releases $RequestedTag $Stable
    }
    $allReleases = @()
    for ($page = 1; $page -le 20; $page++) {
        $pageData = Invoke-RestMethod -Uri ($base + "/releases?per_page=100&page=$page") -Headers $headers -TimeoutSec 30
        $releases = @($pageData)
        $allReleases += $releases
        if ($releases.Count -lt 100) { return Select-RouterRelease $allReleases '' $Stable }
    }
    throw 'Release discovery exceeded 20 pages; specify -Tag.'
}

function Save-RouterAsset($Asset, [string]$Directory, [string]$ReleaseTag) {
    $expected = 'https://github.com/XHToken/magpie/releases/download/' + [Uri]::EscapeDataString($ReleaseTag) + '/' + [Uri]::EscapeDataString($Asset.name)
    if ($Asset.browser_download_url -cne $expected -or $Asset.name -match '[\\/]') { throw 'Unexpected release asset origin or path.' }
    $target = Join-Path $Directory $Asset.name
    Assert-PlainRouterPath $target
    try {
        Invoke-WebRequest -Uri $expected -OutFile $target -UseBasicParsing -TimeoutSec 30 -Headers @{ 'User-Agent' = 'api-router-start-skill' }
    } catch {
        if ($Asset.url -cnotmatch '^https://api\.github\.com/repos/XHToken/magpie/releases/assets/[0-9]+$') { throw }
        # Retry only through the same repository's official asset endpoint.
        Invoke-WebRequest -Uri $Asset.url -OutFile $target -UseBasicParsing -TimeoutSec 180 -Headers @{ 'User-Agent'='api-router-start-skill'; Accept='application/octet-stream' }
    }
    if ((Get-Item -LiteralPath $target).Length -ne $Asset.size) { throw "Asset size mismatch: $($Asset.name)" }
    if ($Asset.digest) {
        $digest = 'sha256:' + (Get-FileHash -LiteralPath $target -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($digest -cne $Asset.digest) { throw "GitHub digest mismatch: $($Asset.name)" }
    }
}

function Confirm-RouterBundle([string]$Directory, [string]$Version) {
    $pins = @{}
    foreach ($line in [IO.File]::ReadAllLines((Join-Path $Directory 'SHA256SUMS'))) {
        if (-not $line.Trim()) { continue }
        if ($line -cnotmatch '^([0-9a-f]{64}) [ *]([^\\/]+)$') { throw 'Invalid SHA256SUMS line.' }
        if ($pins.ContainsKey($Matches[2])) { throw 'Duplicate SHA256SUMS entry.' }
        $pins[$Matches[2]] = $Matches[1]
    }
    $zipName = "wefto-$Version-windows-amd64.zip"
    $manifestName = "wefto-$Version-windows-amd64.manifest.json"
    foreach ($name in @($zipName, $manifestName, 'install-windows.ps1')) {
        if (-not $pins.ContainsKey($name) -or (Get-FileHash -LiteralPath (Join-Path $Directory $name) -Algorithm SHA256).Hash.ToLowerInvariant() -cne $pins[$name]) {
            throw "SHA256SUMS verification failed: $name"
        }
    }
    $m = [IO.File]::ReadAllText((Join-Path $Directory $manifestName)) | ConvertFrom-Json
    if ($m.product -cne 'wefto' -or $m.schemaVersion -ne 1 -or $m.version -cne $Version -or
        $m.os -cne 'windows' -or $m.architecture -cne 'amd64' -or $m.package.name -cne $zipName -or
        $m.package.sha256 -cne $pins[$zipName] -or $m.package.size -ne (Get-Item -LiteralPath (Join-Path $Directory $zipName)).Length) {
        throw 'Manifest identity, version, platform or package mismatch.'
    }
    $installer = @($m.files | Where-Object { $_.name -ceq 'install-windows.ps1' })
    if ($installer.Count -ne 1 -or $installer[0].sha256 -cne $pins['install-windows.ps1']) { throw 'Installer differs from manifest.' }
}

function Confirm-RouterService([string]$Exe, [switch]$OnlyCheck) {
    $product = if ([IO.Path]::GetFileName($Exe) -ieq 'api-router.exe') { 'api-router' } else { 'wefto' }
    $layout = Invoke-RouterJson $Exe @('dirs')
    $action = if ($OnlyCheck) { 'status' } else { 'start' }
    $status = Invoke-RouterJson $Exe @('service', $action)
    if (-not $status.running -or $status.executable -ine $Exe) { throw 'Service stopped or owned by another executable.' }
    $uri = [Uri]$status.managementURL; $ip = $null
    if ($uri.Scheme -cne 'http' -or -not [Net.IPAddress]::TryParse($uri.DnsSafeHost, [ref]$ip) -or -not [Net.IPAddress]::IsLoopback($ip) -or
        $uri.UserInfo -or $uri.Query -or $uri.Fragment -or $uri.AbsolutePath -ne '/') { throw 'Invalid loopback management URL.' }
    $saved = [IO.File]::ReadAllText((Join-Path $layout.config 'config.json')) | ConvertFrom-Json
    if ($status.managementURL -cne ('http://' + $saved.listen.address + '/')) { throw 'Saved address differs from live service.' }
    $health = Invoke-RestMethod -Uri ($status.managementURL + 'healthz') -TimeoutSec 5 -Proxy $null -MaximumRedirection 0
    if ($health.service -cne $product -or $health.status -cne 'ok' -or $health.pid -ne $status.pid) { throw 'Health endpoint identity mismatch.' }
    $expectedToken = Join-Path $layout.state 'router.lock/management-token'
    if ([IO.Path]::GetFullPath($status.tokenFile) -ine [IO.Path]::GetFullPath($expectedToken)) { throw 'Unexpected token path.' }
    Assert-PlainRouterPath $expectedToken
    $token = [IO.File]::ReadAllText($expectedToken).Trim()
    $live = Invoke-RestMethod -Uri ($status.managementURL + 'api/service') -Headers @{ Authorization = 'Bearer ' + $token } -TimeoutSec 5 -Proxy $null -MaximumRedirection 0
    if ($live.service -cne $product -or -not $live.running -or $live.pid -ne $status.pid -or $live.executable -ine $Exe -or
        $live.instanceId -cne $status.instanceId -or $live.processStamp -cne $status.processStamp) { throw 'Authenticated identity mismatch.' }
    [pscustomobject]@{ version=$status.version; executable=$Exe; pid=$status.pid; managementURL=$status.managementURL; config=$layout.config; data=$layout.data }
}

function Start-RouterSkill {
    if ($env:OS -ne 'Windows_NT') { throw 'Use prepare-unix.py on macOS/Linux.' }
    $arch = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
    if ($arch -ine 'AMD64') { throw "No supported Windows package for $arch; do not select x64 for ARM." }
    if (@(@($Plan,$DownloadOnly,$InstallOnly,$CheckOnly) | Where-Object { $_ }).Count -gt 1) { throw 'Choose one execution mode.' }
    $ordinaryLocal = [Environment]::GetFolderPath('LocalApplicationData')
    $ordinaryRoaming = [Environment]::GetFolderPath('ApplicationData')
    $targetDir = if ($InstallDir) { $InstallDir } else { Join-Path $ordinaryLocal 'Programs/Wefto' }
    Assert-PlainRouterPath $targetDir; $targetDir = [IO.Path]::GetFullPath($targetDir)
    $exe = Join-Path $targetDir 'wefto.exe'
    $legacyDir = Join-Path $ordinaryLocal 'Programs/API-router'
    if (-not $InstallDir -and -not $Tag -and -not $DownloadOnly -and -not (Test-Path -LiteralPath $exe) -and
        (Test-Path -LiteralPath (Join-Path $legacyDir 'api-router.exe') -PathType Leaf)) {
        $targetDir = $legacyDir
        $exe = Join-Path $targetDir 'api-router.exe'
        Assert-PlainRouterPath $exe
    }
    $savedRoaming = $env:APPDATA; $savedLocal = $env:LOCALAPPDATA; $savedTls = [Net.ServicePointManager]::SecurityProtocol
    try {
        $env:APPDATA = $ordinaryRoaming; $env:LOCALAPPDATA = $ordinaryLocal
        [Net.ServicePointManager]::SecurityProtocol = $savedTls -bor [Net.SecurityProtocolType]::Tls12
        if ((Test-Path -LiteralPath $exe) -and -not $DownloadOnly) {
            Assert-PlainRouterPath $exe
            if ($Tag) {
                $versionOutput = & $exe version 2>&1
                if ($LASTEXITCODE -ne 0) { throw 'Cannot identify existing installation.' }
                if ('wefto-v' + ($versionOutput -join '').Trim() -cne $Tag) { throw 'Different version installed. Follow references/update.md; existing files preserved.' }
            }
            if ($Plan -or $InstallOnly) { [pscustomobject]@{ action='reuse'; executable=$exe } | ConvertTo-Json; return }
        } else {
            if ($CheckOnly) { throw 'No installed executable to check.' }
            $release = Get-RouterRelease $Tag ([bool]$StableOnly)
            if (-not $release) { throw 'No compatible complete 端云协同 x64 release. Specify -Tag or allow previews by omitting -StableOnly.' }
            $version = $release.tag_name.Substring(7)
            if ($Plan) { [pscustomobject]@{ action='download-install'; tag=$release.tag_name; prerelease=$release.prerelease; releaseURL=$release.html_url; installDir=$targetDir } | ConvertTo-Json; return }
            $cacheParent = Join-Path $ordinaryLocal 'Wefto-skill/downloads'
            Assert-PlainRouterPath $cacheParent
            $cache = Join-Path $cacheParent ($release.tag_name + '-' + [Guid]::NewGuid().ToString('N'))
            [IO.Directory]::CreateDirectory($cache) | Out-Null
            foreach ($name in @("wefto-$version-windows-amd64.zip", "wefto-$version-windows-amd64.manifest.json", 'install-windows.ps1', 'SHA256SUMS')) {
                $asset = @($release.assets | Where-Object { $_.name -ceq $name })[0]
                Save-RouterAsset $asset $cache $release.tag_name
            }
            Confirm-RouterBundle $cache $version
            if ($DownloadOnly) { [pscustomobject]@{ action='downloaded-verified'; tag=$release.tag_name; directory=$cache } | ConvertTo-Json; return }
            & powershell.exe -NoProfile -ExecutionPolicy Bypass -File (Join-Path $cache 'install-windows.ps1') -ManifestPath (Join-Path $cache "wefto-$version-windows-amd64.manifest.json") -PackagePath (Join-Path $cache "wefto-$version-windows-amd64.zip") -InstallDir $targetDir
            if ($LASTEXITCODE -ne 0) { throw 'Official installer failed; do not start an unverified executable.' }
            if ($InstallOnly) { [pscustomobject]@{ action='installed'; version=$version; executable=$exe } | ConvertTo-Json; return }
        }
        $info = Confirm-RouterService $exe -OnlyCheck:$CheckOnly
        $info | ConvertTo-Json
        if (-not $NoBrowser -and -not $CheckOnly) { Start-Process $info.managementURL | Out-Null }
    } finally {
        $env:APPDATA = $savedRoaming; $env:LOCALAPPDATA = $savedLocal; [Net.ServicePointManager]::SecurityProtocol = $savedTls
    }
}

if ($MyInvocation.InvocationName -ne '.') {
    try { Start-RouterSkill } catch { [Console]::Error.WriteLine($_.Exception.Message); exit 1 }
}
