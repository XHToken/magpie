# Shared by the standalone skill and the project launcher. An instance record
# selects a known CLI; that CLI must still authenticate its own live service.
function Resolve-RouterExecutable {
    param([string]$DefaultExecutable, [string]$LocalAppData)
    $known = @($DefaultExecutable,
        (Join-Path $LocalAppData 'Programs/Wefto/wefto.exe'),
        (Join-Path $LocalAppData 'Programs/API-router/api-router.exe'))
    $owners = @()
    foreach ($product in @('api-router', 'wefto')) {
        $lock = Join-Path $LocalAppData "$product/state/router.lock"
        if (-not (Test-Path -LiteralPath $lock)) { continue }
        Assert-PlainRouterPath $lock
        $recordPath = Join-Path $lock 'instance.json'
        Assert-PlainRouterPath $recordPath
        if (-not (Test-Path -LiteralPath $recordPath -PathType Leaf) -or
            (Get-Item -LiteralPath $recordPath).Length -gt 65536) {
            throw 'Service lock has no readable bounded instance record; existing data preserved. Inspect the owning service before recovery.'
        }
        $record = [IO.File]::ReadAllText($recordPath) | ConvertFrom-Json
        if (-not $record.executable -or $record.pid -le 0 -or -not $record.instanceId -or -not $record.processStamp) {
            throw 'Invalid service instance record; existing data preserved.'
        }
        $owner = @($known | Where-Object {
            [IO.Path]::GetFullPath($_) -ieq [string]$record.executable
        })
        if (-not $owner.Count) {
            throw 'Another executable owns the user layout. Use its verified CLI or explicitly switch builds; do not create another data directory or reinstall models.'
        }
        Assert-PlainRouterPath $owner[0]
        if (-not (Test-Path -LiteralPath $owner[0] -PathType Leaf)) {
            throw 'The recorded service executable is missing. Restore that installation before switching builds; existing configuration and models were preserved.'
        }
        $owners += $owner[0]
    }
    if ($owners.Count -gt 1) {
        throw 'Both legacy and renamed layouts contain service records. Preserve both and resolve the layout conflict before startup.'
    }
    if ($owners.Count -eq 1) { return $owners[0] }
    if (Test-Path -LiteralPath $DefaultExecutable -PathType Leaf) { return $DefaultExecutable }
    foreach ($candidate in $known | Select-Object -Skip 1) {
        if (Test-Path -LiteralPath $candidate -PathType Leaf) {
            Assert-PlainRouterPath $candidate
            return $candidate
        }
    }
    return $DefaultExecutable
}

# Validate dirs before any service start. A renamed binary that resolves an
# empty second layout must not silently initialize it beside existing data.
function Assert-RouterUserLayout {
    param($Layout, [string]$AppData, [string]$LocalAppData)
    $name = Split-Path -Leaf $Layout.config
    if ($name -notin @('api-router', 'wefto') -or
        $Layout.config -ine (Join-Path $AppData $name) -or
        $Layout.data -ine (Join-Path $LocalAppData $name) -or
        $Layout.state -ine (Join-Path $Layout.data 'state')) {
        throw 'Unexpected Router storage layout; startup stopped before creating another instance.'
    }
    foreach ($product in @('api-router', 'wefto')) {
        if ($product -eq $name) { continue }
        foreach ($path in @((Join-Path $LocalAppData "$product/state/router.lock"),
                            (Join-Path $AppData "$product/config.json"),
                            (Join-Path $LocalAppData "$product/models"))) {
            if (Test-Path -LiteralPath $path) {
                throw 'Another user layout contains configuration, models or a service lock. Preserve both layouts and use the documented migration; do not initialize empty settings or redownload models.'
            }
        }
    }
}
