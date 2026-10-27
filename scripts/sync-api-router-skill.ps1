#requires -Version 5.1
[CmdletBinding()]
param([Parameter(Mandatory=$true)][string[]]$TargetDirectories)
$ErrorActionPreference = 'Stop'
$source = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../skills/api-router-start'))
. (Join-Path $source 'scripts/start-api-router.ps1')
$files = @(Get-ChildItem -LiteralPath $source -Recurse -File | Where-Object {
    $_.FullName -notmatch '[\\/]__pycache__[\\/]' -and $_.Extension -ne '.pyc'
})
$targets = @($TargetDirectories | ForEach-Object {
    Assert-PlainRouterPath $_
    $target = [IO.Path]::GetFullPath($_).TrimEnd('\', '/')
    if ($target -ieq $source -or -not (Test-Path -LiteralPath (Join-Path $target 'SKILL.md') -PathType Leaf)) {
        throw "Expected an existing installed skill directory: $target"
    }
    foreach ($file in $files) {
        Assert-PlainRouterPath (Join-Path $target $file.FullName.Substring($source.Length + 1))
    }
    $target
} | Select-Object -Unique)
$backupRoot = Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) ('api-router-skill/backups/' + [DateTime]::UtcNow.ToString('yyyyMMddTHHmmss') + '-' + [Guid]::NewGuid().ToString('N'))
Assert-PlainRouterPath $backupRoot
New-Item -ItemType Directory -Path $backupRoot | Out-Null
$index = 0
foreach ($target in $targets) {
    $backup = Join-Path $backupRoot ([string]$index++)
    New-Item -ItemType Directory -Path $backup | Out-Null
    [IO.File]::WriteAllText((Join-Path $backup 'target.txt'), $target)
    # Back up managed files only; retain unrelated host files and caches.
    foreach ($file in $files) {
        $relative = $file.FullName.Substring($source.Length + 1)
        $previous = Join-Path $target $relative
        if (Test-Path -LiteralPath $previous -PathType Leaf) {
            $saved = Join-Path $backup $relative
            New-Item -ItemType Directory -Path (Split-Path -Parent $saved) -Force | Out-Null
            Copy-Item -LiteralPath $previous -Destination $saved
        }
    }
    foreach ($file in $files) {
        $destination = Join-Path $target $file.FullName.Substring($source.Length + 1)
        New-Item -ItemType Directory -Path (Split-Path -Parent $destination) -Force | Out-Null
        Copy-Item -LiteralPath $file.FullName -Destination $destination -Force
        if ((Get-FileHash -LiteralPath $file.FullName).Hash -cne (Get-FileHash -LiteralPath $destination).Hash) {
            throw "Skill verification failed: $destination . Backup: $backup"
        }
    }
    [pscustomobject]@{target=$target; backup=$backup; files=$files.Count}
}
