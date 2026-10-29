#requires -Version 5.1
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot '..\start-project.ps1')

function Assert-True($Value, [string]$Message) { if (-not $Value) { throw $Message } }
function Assert-Fails([scriptblock]$Action, [string]$Pattern) {
    try { & $Action } catch {
        if ($_.Exception.Message -notmatch $Pattern) { throw }
        return
    }
    throw "Expected failure: $Pattern"
}

$routerTestRoot = Join-Path ([IO.Path]::GetTempPath()) ('router-start-test-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $routerTestRoot | Out-Null
try {
    $routerTestExecutable = Join-Path $routerTestRoot 'wefto.exe'
    [IO.File]::WriteAllText($routerTestExecutable, 'not executed: native CLI is replaced by a test double')
    $routerTestState = Join-Path $routerTestRoot 'local/wefto/state'
    New-Item -ItemType Directory -Path $routerTestState | Out-Null
    $routerTestAppData = Join-Path $routerTestRoot 'roaming'
    $routerTestLocalAppData = Join-Path $routerTestRoot 'local'
    New-Item -ItemType Directory -Path (Join-Path $routerTestAppData 'wefto') | Out-Null
    [IO.File]::WriteAllText((Join-Path $routerTestAppData 'wefto\config.json'), '{"listen":{"address":"127.0.0.1:13145"}}')
    $routerTestHost = Join-Path $routerTestRoot 'workbuddy'
    New-Item -ItemType Directory -Path $routerTestHost | Out-Null
    $routerTestModels = Join-Path $routerTestHost 'models.json'
    $routerTestApps = @{apps = @(@{id = 'workbuddy'; configured = $true; cloud = @{model = 'fixture-model'}})}
    [IO.File]::WriteAllText($routerTestModels, '[{"id":"fixture-model","url":"http://127.0.0.1:13145/app/workbuddy/v1"}]')
    Assert-WorkBuddyRouterAddress $routerTestHost $routerTestApps 'http://127.0.0.1:13145/'
    [IO.File]::WriteAllText($routerTestModels, '{"models":[{"id":"fixture-model","url":"http://127.0.0.1:13145/app/workbuddy/v1"}]}')
    Assert-WorkBuddyRouterAddress $routerTestHost $routerTestApps 'http://127.0.0.1:13145/'
    Write-Host 'PASS: both WorkBuddy configuration shapes match the live endpoint'

    $routerAliasApps = '{"apps":[{"id":"workbuddy","configured":true,"models":["group/ttt"],"modelAliases":{"ttt":"group/ttt"}}]}' | ConvertFrom-Json
    [IO.File]::WriteAllText($routerTestModels, '[{"id":"ttt","url":"http://127.0.0.1:13145/app/workbuddy/v1"}]')
    Assert-WorkBuddyRouterAddress $routerTestHost $routerAliasApps 'http://127.0.0.1:13145/'
    [IO.File]::WriteAllText($routerTestModels, '[{"id":"ttt","url":"http://127.0.0.1:58116/app/workbuddy/v1"}]')
    Assert-Fails { Assert-WorkBuddyRouterAddress $routerTestHost $routerAliasApps 'http://127.0.0.1:13145/' } 'endpoint differs'
    Write-Host 'PASS: readable WorkBuddy model aliases resolve to their saved target and reject stale endpoints'

    [IO.File]::WriteAllText($routerTestModels, '[{"id":"fixture-model","url":"http://127.0.0.1:58116/app/workbuddy/v1"}]')
    $routerTestBefore = [IO.File]::ReadAllText($routerTestModels)
    Assert-Fails { Assert-WorkBuddyRouterAddress $routerTestHost $routerTestApps 'http://127.0.0.1:13145/' } 'endpoint differs'
    Assert-True ([IO.File]::ReadAllText($routerTestModels) -eq $routerTestBefore) 'Endpoint check rewrote host configuration'
    Write-Host 'PASS: a stale WorkBuddy port fails visibly and preserves configuration'

    $routerAliasApps = '{"apps":[{"id":"workbuddy","configured":true,"cloud":{"model":"fixture-model"},"models":["fixture-model"],"modelAliases":{"Readable model":"fixture-model"}}]}' | ConvertFrom-Json
    [IO.File]::WriteAllText($routerTestModels, '[{"id":"Readable model","url":"http://127.0.0.1:13145/app/workbuddy/v1"}]')
    Assert-WorkBuddyRouterAddress $routerTestHost $routerAliasApps 'http://127.0.0.1:13145/'
    [IO.File]::WriteAllText($routerTestModels, '[{"id":"Readable model","url":"http://127.0.0.1:58116/app/workbuddy/v1"}]')
    $routerTestBefore = [IO.File]::ReadAllText($routerTestModels)
    Assert-Fails { Assert-WorkBuddyRouterAddress $routerTestHost $routerAliasApps 'http://127.0.0.1:13145/' } 'endpoint differs'
    Assert-True ([IO.File]::ReadAllText($routerTestModels) -eq $routerTestBefore) 'Alias endpoint check rewrote host configuration'
    Write-Host 'PASS: readable WorkBuddy aliases resolve only the saved target and still reject stale endpoints'

    $routerTestTokenFile = Join-Path $routerTestState 'router.lock/management-token'
    New-Item -ItemType Directory -Path (Split-Path -Parent $routerTestTokenFile) | Out-Null
    [IO.File]::WriteAllText($routerTestTokenFile, 'fixture-token')
    $routerTestCommands = New-Object 'Collections.Generic.List[string]'
    function Invoke-WeftoCommand {
        param([string]$Executable, [string[]]$Arguments)
        Assert-True ($env:APPDATA -eq $routerTestAppData -and $env:LOCALAPPDATA -eq $routerTestLocalAppData) 'Startup inherited a test instance environment'
        $routerTestCommands.Add(($Arguments -join ' '))
        if ($Arguments[0] -eq 'dirs') {
            return @{config = (Join-Path $routerTestAppData 'wefto'); data = (Join-Path $routerTestLocalAppData 'wefto'); state = $routerTestState}
        }
        return @{running = $true; executable = $Executable; pid = 123; version = 'fixture'; managementURL = 'http://127.0.0.1:13145/'; tokenFile = $routerTestTokenFile; instanceId = 'fixture'; processStamp = 'stamp'}
    }
    function Invoke-RestMethod {
        param($Uri, $TimeoutSec, $Proxy, $Headers, $MaximumRedirection)
        $product = if ($script:routerTestInstalledOwner) { 'wefto' } else { 'wefto' }
        if ($Uri.EndsWith('/healthz')) { return @{service = $product; status = 'ok'; pid = 123} }
        if ($Uri.EndsWith('/api/service')) {
            $exe = if ($script:routerTestInstalledOwner) { $script:routerTestInstalledOwner } else { $routerTestExecutable }
            return @{service = $product; running = $true; pid = 123; executable = $exe; instanceId = 'fixture'; processStamp = 'stamp'}
        }
        Assert-True ($Uri -eq 'http://127.0.0.1:13145/api/apps') 'Token sent to an unexpected endpoint'
        Assert-True ($Headers.Authorization -eq 'Bearer fixture-token') 'Missing management authorization'
        return @{apps = @()}
    }
    $routerTestSavedAppData = $env:APPDATA
    $routerTestSavedLocalAppData = $env:LOCALAPPDATA
    # The native CLI validates unknown locks. No record lets the launcher
    # select its default; create our token only when the fake CLI is invoked.
    $routerTestRecord = Join-Path $routerTestState 'router.lock/instance.json'
    [IO.File]::WriteAllText($routerTestRecord, (@{pid=123; executable=$routerTestExecutable; instanceId='fixture'; processStamp='stamp'} | ConvertTo-Json))
    try {
        $env:APPDATA = 'unrelated-test-instance-roaming'
        $env:LOCALAPPDATA = 'unrelated-test-instance-local'
        Start-WeftoProject -Executable $routerTestExecutable -AppData $routerTestAppData -LocalAppData $routerTestLocalAppData -WorkBuddyRoot $routerTestHost -CheckOnly -NoBrowser
        Assert-True (($routerTestCommands -join ',') -eq 'dirs,service status') 'Read-only startup check started a service'
        Assert-True ($env:APPDATA -eq 'unrelated-test-instance-roaming' -and $env:LOCALAPPDATA -eq 'unrelated-test-instance-local') 'Caller environment was not restored'
        $routerTestCommands.Clear()
        Start-WeftoProject -Executable $routerTestExecutable -AppData $routerTestAppData -LocalAppData $routerTestLocalAppData -WorkBuddyRoot $routerTestHost -NoBrowser
        Assert-True (($routerTestCommands -join ',') -eq 'dirs,service start') 'Startup did not use the native service start command'
        $routerTestCommands.Clear()
        $script:routerTestInstalledOwner = Join-Path $routerTestLocalAppData 'Programs/Wefto/wefto.exe'
        New-Item -ItemType Directory -Path (Split-Path -Parent $script:routerTestInstalledOwner) -Force | Out-Null
        [IO.File]::WriteAllText($script:routerTestInstalledOwner, 'not executed')
        [IO.File]::WriteAllText($routerTestRecord, (@{pid=123; executable=$script:routerTestInstalledOwner; instanceId='fixture'; processStamp='stamp'} | ConvertTo-Json))
        # This used to select the project EXE and fail against the installed
        # service. Check the CLI path, not just successful output.
        function Invoke-WeftoCommand {
            param([string]$Executable, [string[]]$Arguments)
            Assert-True ($Executable -eq $script:routerTestInstalledOwner) 'Project launcher did not reuse the installed owner'
            $routerTestCommands.Add(($Arguments -join ' '))
            if ($Arguments[0] -eq 'dirs') {
                return @{config=(Join-Path $routerTestAppData 'wefto'); data=(Join-Path $routerTestLocalAppData 'wefto'); state=$routerTestState}
            }
            return @{running=$true; executable=$Executable; pid=123; version='fixture'; managementURL='http://127.0.0.1:13145/'; tokenFile=$routerTestTokenFile; instanceId='fixture'; processStamp='stamp'}
        }
        Start-WeftoProject -Executable $routerTestExecutable -AppData $routerTestAppData -LocalAppData $routerTestLocalAppData -WorkBuddyRoot $routerTestHost -CheckOnly -NoBrowser
        Assert-True (($routerTestCommands -join ',') -eq 'dirs,service status') 'Owner reuse started another service'
        Write-Host 'PASS: project startup reuses the installed owner and authenticates its service'
        function Invoke-RestMethod {
            param($Uri, $TimeoutSec, $Proxy, $Headers, $MaximumRedirection)
            if ($Uri.EndsWith('/healthz')) { return @{service='wefto'; status='ok'; pid=123} }
            return @{service='wefto'; running=$true; pid=123; executable=$script:routerTestInstalledOwner; instanceId='foreign'; processStamp='stamp'}
        }
        Assert-Fails { Start-WeftoProject -Executable $routerTestExecutable -AppData $routerTestAppData -LocalAppData $routerTestLocalAppData -WorkBuddyRoot $routerTestHost -CheckOnly -NoBrowser } 'Authenticated service identity mismatch'
        Write-Host 'PASS: owner reuse rejects an authenticated instance mismatch'
    } finally {
        $env:APPDATA = $routerTestSavedAppData
        $env:LOCALAPPDATA = $routerTestSavedLocalAppData
    }
    Write-Host 'PASS: startup pins storage roots, uses the native lifecycle and restores the caller environment'

} finally {
    # The only recursive cleanup target is the literal, verified test directory.
    if ($routerTestRoot -and [IO.Path]::GetFullPath($routerTestRoot).StartsWith([IO.Path]::GetTempPath(), [StringComparison]::OrdinalIgnoreCase) -and
        (Split-Path -Leaf $routerTestRoot).StartsWith('router-start-test-')) {
        Remove-Item -LiteralPath $routerTestRoot -Recurse -Force
    }
}
