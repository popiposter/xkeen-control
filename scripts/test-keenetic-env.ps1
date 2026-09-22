[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$loader = Join-Path $PSScriptRoot 'keenetic-env.ps1'
$temporary = Join-Path ([IO.Path]::GetTempPath()) ("xkeen-keenetic-env-{0}" -f [Guid]::NewGuid().ToString('N'))
$names = @('KEENETIC_SSH_HOST', 'KEENETIC_SSH_PORT', 'KEENETIC_SSH_USER', 'KEENETIC_SSH_TARGET', 'KEENETIC_SSH_PASSWORD', 'KEENETIC_SSH_IDENTITY_FILE')
$saved = @{}
foreach ($name in $names) {
    $saved[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
}

function Write-Fixture {
    param([string]$Path, [string]$Contents)
    $directory = Split-Path -Parent $Path
    [void][IO.Directory]::CreateDirectory($directory)
    [IO.File]::WriteAllText($Path, $Contents, [Text.UTF8Encoding]::new($false))
}

function Assert-Rejected {
    param([string]$Name, [string]$Contents)
    $path = Join-Path $temporary ("reject-{0}.env" -f $Name)
    Write-Fixture -Path $path -Contents $Contents
    $accepted = $true
    $message = ''
    try {
        . $loader -EnvFile $path
    } catch {
        $accepted = $false
        $message = $_.Exception.Message
    }
    if ($accepted) {
        throw "rejection fixture was accepted: $Name"
    }
    if ($message.Contains('synthetic-secret-value', [StringComparison]::Ordinal)) {
        throw "rejection exposed the synthetic secret: $Name"
    }
}

try {
    [void][IO.Directory]::CreateDirectory($temporary)
    $identity = Join-Path $temporary 'keys\operator-key'
    Write-Fixture -Path $identity -Contents "synthetic-private-key-placeholder`n"

    $keyOnly = Join-Path $temporary 'key-only.env'
    Write-Fixture -Path $keyOnly -Contents @"
KEENETIC_SSH_HOST=router.example
KEENETIC_SSH_PORT=222
KEENETIC_SSH_USER=root
KEENETIC_SSH_IDENTITY_FILE=keys/operator-key
"@
    $env:KEENETIC_SSH_PASSWORD = 'stale-password'
    $output = @(. $loader -EnvFile $keyOnly 2>&1)
    if ($output.Count -ne 0 -or $env:KEENETIC_SSH_TARGET -ne 'root@router.example' -or $env:KEENETIC_SSH_IDENTITY_FILE -ne $identity -or (Test-Path Env:KEENETIC_SSH_PASSWORD)) {
        throw 'key-only fixture did not load or clear stale password state'
    }

    $passwordOnly = Join-Path $temporary 'password-only.env'
    Write-Fixture -Path $passwordOnly -Contents @"
KEENETIC_SSH_HOST=router.example
KEENETIC_SSH_PORT=22
KEENETIC_SSH_USER=operator
KEENETIC_SSH_PASSWORD=synthetic-secret-value
"@
    $output = @(. $loader -EnvFile $passwordOnly 2>&1)
    if ($output.Count -ne 0 -or $env:KEENETIC_SSH_PASSWORD -ne 'synthetic-secret-value' -or (Test-Path Env:KEENETIC_SSH_IDENTITY_FILE)) {
        throw 'password-only fixture did not load or clear stale identity state'
    }

    $both = Join-Path $temporary 'both.env'
    Write-Fixture -Path $both -Contents @"
KEENETIC_SSH_HOST=router.example
KEENETIC_SSH_PORT=2200
KEENETIC_SSH_USER=operator
KEENETIC_SSH_PASSWORD="synthetic-secret-value"
KEENETIC_SSH_IDENTITY_FILE="keys/operator-key"
"@
    $output = @(. $loader -EnvFile $both 2>&1)
    if ($output.Count -ne 0 -or $env:KEENETIC_SSH_PASSWORD -ne 'synthetic-secret-value' -or $env:KEENETIC_SSH_IDENTITY_FILE -ne $identity) {
        throw 'combined authentication fixture did not load'
    }

    $base = "KEENETIC_SSH_HOST=router.example`nKEENETIC_SSH_PORT=22`nKEENETIC_SSH_USER=root`nKEENETIC_SSH_PASSWORD=synthetic-secret-value`n"
    Assert-Rejected -Name 'unknown' -Contents ($base + "UNSUPPORTED=value`n")
    Assert-Rejected -Name 'duplicate' -Contents ($base + "KEENETIC_SSH_HOST=other.example`n")
    Assert-Rejected -Name 'missing-user' -Contents "KEENETIC_SSH_HOST=router.example`nKEENETIC_SSH_PORT=22`nKEENETIC_SSH_PASSWORD=synthetic-secret-value`n"
    Assert-Rejected -Name 'invalid-port' -Contents ($base.Replace('KEENETIC_SSH_PORT=22', 'KEENETIC_SSH_PORT=70000'))
    Assert-Rejected -Name 'no-auth' -Contents "KEENETIC_SSH_HOST=router.example`nKEENETIC_SSH_PORT=22`nKEENETIC_SSH_USER=root`n"
    Assert-Rejected -Name 'oversize' -Contents ('A' * 4097)

    $directoryInput = Join-Path $temporary 'directory-input'
    [void][IO.Directory]::CreateDirectory($directoryInput)
    $directoryAccepted = $true
    try { . $loader -EnvFile $directoryInput } catch { $directoryAccepted = $false }
    if ($directoryAccepted) { throw 'directory input was accepted' }

    $symlink = Join-Path $temporary 'linked.env'
    $linkCreated = $false
    try {
        [void](New-Item -ItemType SymbolicLink -Path $symlink -Target $passwordOnly -ErrorAction Stop)
        $linkCreated = $true
    } catch [System.UnauthorizedAccessException] {
        # Windows without Developer Mode cannot create this synthetic link.
    } catch [System.IO.IOException] {
        # Some Windows filesystems do not expose symbolic-link creation.
    }
    if ($linkCreated) {
        $linkAccepted = $true
        try { . $loader -EnvFile $symlink } catch { $linkAccepted = $false }
        if ($linkAccepted) { throw 'symlink input was accepted' }
    }

    Write-Output 'keenetic env PowerShell fixtures passed'
} finally {
    foreach ($name in $names) {
        [Environment]::SetEnvironmentVariable($name, $saved[$name], 'Process')
    }
    if (Test-Path -LiteralPath $temporary) {
        Remove-Item -LiteralPath $temporary -Recurse -Force
    }
}
