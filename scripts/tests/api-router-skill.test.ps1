#requires -Version 5.1
param([Parameter(Mandatory=$true)][string]$BundleDir, [Parameter(Mandatory=$true)][string]$TestRoot)
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot '../../skills/api-router-start/scripts/start-api-router.ps1')
function Assert-True([bool]$Condition, [string]$Message) { if (-not $Condition) { throw $Message } }
function Assert-Rejected([scriptblock]$Action, [string]$Message) {
    $rejected = $false
    try { & $Action | Out-Null } catch { $rejected = $true }
    Assert-True $rejected $Message
}
Assert-PlainRouterPath $TestRoot
Assert-True (-not (Test-Path -LiteralPath (Join-Path $TestRoot 'install'))) 'Test install directory must be new.'
$version = '0.1.0-beta.4'
$names = @("wefto-$version-windows-amd64.zip", "wefto-$version-windows-amd64.manifest.json", 'install-windows.ps1', 'SHA256SUMS')
$fixture = [pscustomobject]@{ tag_name="wefto-v$version"; draft=$false; prerelease=$true; published_at='2026-10-26T04:53:09Z'; assets=@($names | ForEach-Object { [pscustomobject]@{ name=$_ } }) }
$other = [pscustomobject]@{ tag_name='api-router-v99.0.0'; draft=$false; prerelease=$false; published_at='2026-10-27T00:00:00Z'; assets=$fixture.assets }
Assert-True ((Select-RouterRelease @($other,$fixture) '' $false).tag_name -eq $fixture.tag_name) 'Wrong product release selected.'
Assert-True ($null -eq (Select-RouterRelease @($fixture) '' $true)) 'StableOnly accepted preview.'
$incomplete = $fixture.PSObject.Copy(); $incomplete.assets = @($fixture.assets | Select-Object -First 3)
Assert-True ($null -eq (Select-RouterRelease @($incomplete) '' $false)) 'Incomplete package selected.'
Assert-Rejected { Get-RouterRelease 'api-router-v0.1.0' $false } 'Off-product tag accepted.'
Assert-Rejected {
    Save-RouterAsset ([pscustomobject]@{ name='install-windows.ps1'; browser_download_url='https://github.com/xyzane54/magpie/releases/download/wefto-v0.1.0-beta.3/install-windows.ps1' }) $TestRoot 'wefto-v0.1.0-beta.3'
} 'Asset from the previous repository accepted.'
Assert-Rejected { Assert-PlainRouterPath (Join-Path $TestRoot '../escape') } 'Path alias accepted.'

Confirm-RouterBundle $BundleDir $version
$damaged = Join-Path $TestRoot 'damaged'
New-Item -ItemType Directory -Path $damaged | Out-Null
foreach ($name in $names) { Copy-Item -LiteralPath (Join-Path $BundleDir $name) -Destination $damaged }
[IO.File]::AppendAllText((Join-Path $damaged "wefto-$version-windows-amd64.zip"), 'corruption')
Assert-Rejected { Confirm-RouterBundle $damaged $version } 'Damaged ZIP accepted.'
Assert-Rejected { Confirm-RouterBundle $BundleDir '0.0.0' } 'Wrong version accepted.'

$savedRoaming=$env:APPDATA; $savedLocal=$env:LOCALAPPDATA
$env:APPDATA=Join-Path $TestRoot 'roaming'; $env:LOCALAPPDATA=Join-Path $TestRoot 'local'
$install = Join-Path $TestRoot 'install'
$testExe = Join-Path $install 'wefto.exe'
try {
    & powershell.exe -NoProfile -ExecutionPolicy Bypass -File (Join-Path $BundleDir 'install-windows.ps1') -ManifestPath (Join-Path $BundleDir "wefto-$version-windows-amd64.manifest.json") -PackagePath (Join-Path $BundleDir "wefto-$version-windows-amd64.zip") -InstallDir $install
    Assert-True ($LASTEXITCODE -eq 0) 'Verified installation failed.'
    $started = Confirm-RouterService $testExe
    Assert-True ($started.version -eq $version) 'Started wrong version.'
    Assert-True ($started.config -eq (Join-Path $env:APPDATA 'wefto')) 'Escaped isolated config.'
    $again = Confirm-RouterService $testExe
    Assert-True ($again.pid -eq $started.pid) 'Repeated start did not reuse instance.'
    $checked = Confirm-RouterService $testExe -OnlyCheck
    Assert-True ($checked.pid -eq $started.pid) 'Read-only check failed.'
    $dirs = Invoke-RouterJson $testExe @('dirs')
    # Busy port must stay alive, so use another loopback listener and confirm
    # native fallback under a fresh layout after stopping our own service.
    Invoke-RouterJson $testExe @('service','stop') | Out-Null
    $listener = New-Object Net.Sockets.TcpListener([Net.IPAddress]::Loopback, 0)
    $listener.Start()
    try {
        $configPath = Join-Path $dirs.config 'config.json'
        $config = [IO.File]::ReadAllText($configPath) | ConvertFrom-Json
        $occupied = $listener.LocalEndpoint.Port
        $config.listen.address = '127.0.0.1:' + $occupied
        [IO.File]::WriteAllText($configPath, ($config | ConvertTo-Json -Depth 20), (New-Object Text.UTF8Encoding($false)))
        $fallback = Confirm-RouterService $testExe
        Assert-True (([Uri]$fallback.managementURL).Port -ne $occupied) 'Occupied port not avoided.'
        Assert-True ($listener.Server.IsBound) 'Unrelated listener was stopped.'
    } finally { $listener.Stop() }
    Write-Output 'PASS: product/platform selection, stable filter, incomplete release, wrong tag, path alias, real hashes, corrupt ZIP, installation, authenticated startup, repeated start, status, occupied-port fallback.'
} finally {
    if (Test-Path -LiteralPath $testExe) {
        & $testExe service stop | Out-Null
        if ($LASTEXITCODE -ne 0) { Write-Warning 'Test service stop failed; inspect the isolated test instance.' }
    }
    $env:APPDATA=$savedRoaming; $env:LOCALAPPDATA=$savedLocal
}
