#requires -Version 5.1
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot '../../skills/api-router-start/scripts/start-api-router.ps1')
function Assert-True($Value, [string]$Message) { if (-not $Value) { throw $Message } }
function Assert-Fails([scriptblock]$Action) {
    try { & $Action | Out-Null } catch { return }
    throw 'Expected the unknown or conflicting layout to be rejected.'
}
$testRoot = Join-Path ([IO.Path]::GetTempPath()) ('router-owner-test-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $testRoot | Out-Null
try {
    $default = Join-Path $testRoot 'project/api-router.exe'
    $installed = Join-Path $testRoot 'Programs/Wefto/wefto.exe'
    foreach ($exe in @($default, $installed)) {
        New-Item -ItemType Directory -Path (Split-Path -Parent $exe) -Force | Out-Null
        [IO.File]::WriteAllText($exe, 'never executed')
    }
    Assert-True ((Resolve-RouterExecutable $default $testRoot) -eq $default) 'No-owner startup changed the project default.'
    $recordPath = Join-Path $testRoot 'api-router/state/router.lock/instance.json'
    New-Item -ItemType Directory -Path (Split-Path -Parent $recordPath) -Force | Out-Null
    $record = @{pid=123; executable=$installed; instanceId='fixture'; processStamp='stamp'}
    [IO.File]::WriteAllText($recordPath, ($record | ConvertTo-Json))
    $before = [IO.File]::ReadAllText($recordPath)
    Assert-True ((Resolve-RouterExecutable $default $testRoot) -eq $installed) 'Installed owner was not reused.'
    Assert-True ([IO.File]::ReadAllText($recordPath) -ceq $before) 'Owner discovery changed the lock.'
    $roaming = Join-Path $testRoot 'roaming'
    $oldLayout = @{config=(Join-Path $roaming 'api-router'); data=(Join-Path $testRoot 'api-router'); state=(Join-Path $testRoot 'api-router/state')}
    $newLayout = @{config=(Join-Path $roaming 'wefto'); data=(Join-Path $testRoot 'wefto'); state=(Join-Path $testRoot 'wefto/state')}
    Assert-RouterUserLayout $oldLayout $roaming $testRoot
    Assert-Fails { Assert-RouterUserLayout $newLayout $roaming $testRoot }
    $record.executable = Join-Path $testRoot 'historical-test/api-router.exe'
    [IO.File]::WriteAllText($recordPath, ($record | ConvertTo-Json))
    Assert-Fails { Resolve-RouterExecutable $default $testRoot }
    [IO.File]::WriteAllText($recordPath, '{broken')
    Assert-Fails { Resolve-RouterExecutable $default $testRoot }
    Remove-Item -LiteralPath $recordPath
    Assert-Fails { Resolve-RouterExecutable $default $testRoot }
    $record.executable=$installed
    [IO.File]::WriteAllText($recordPath, ($record | ConvertTo-Json))
    $second = Join-Path $testRoot 'wefto/state/router.lock/instance.json'
    New-Item -ItemType Directory -Path (Split-Path -Parent $second) -Force | Out-Null
    [IO.File]::WriteAllText($second, ($record | ConvertTo-Json))
    Assert-Fails { Resolve-RouterExecutable $default $testRoot }
    Assert-Fails { Assert-RouterUserLayout $oldLayout $roaming $testRoot }
    $commands = New-Object 'Collections.Generic.List[string]'
    function Invoke-RouterJson([string]$Exe, [string[]]$Arguments) {
        $commands.Add(($Arguments -join ' '))
        return $newLayout
    }
    $savedRoaming=$env:APPDATA; $savedLocal=$env:LOCALAPPDATA
    try {
        $env:APPDATA=$roaming; $env:LOCALAPPDATA=$testRoot
        Assert-Fails { Confirm-RouterService $installed }
        Assert-True (($commands -join ',') -eq 'dirs') 'Layout conflict attempted to start another service.'
    } finally { $env:APPDATA=$savedRoaming; $env:LOCALAPPDATA=$savedLocal }
    Write-Host 'PASS: default, installed owner, preserved record, unknown owner, malformed/missing records, conflicting layouts, prevention of empty renamed layout'
} finally {
    $resolved = [IO.Path]::GetFullPath($testRoot)
    if ($resolved.StartsWith([IO.Path]::GetTempPath(), [StringComparison]::OrdinalIgnoreCase) -and
        (Split-Path -Leaf $resolved).StartsWith('router-owner-test-')) {
        Remove-Item -LiteralPath $resolved -Recurse -Force
    }
}
