[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateNotNullOrEmpty()]
    [string]$EnvFile
)

& {
    param([string]$InputPath)

    Set-StrictMode -Version Latest
    $ErrorActionPreference = 'Stop'

    $repositoryRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..') -ErrorAction Stop).Path

    function Read-KeeneticEnvironmentFile {
        param([Parameter(Mandatory = $true)][string]$Path)

        if (-not [IO.Path]::IsPathFullyQualified($Path)) {
            throw 'Keenetic environment path must be an explicit absolute host path'
        }
        $resolved = (Resolve-Path -LiteralPath $Path -ErrorAction Stop).Path
        $comparison = if ($IsWindows -or $env:OS -eq 'Windows_NT') { [StringComparison]::OrdinalIgnoreCase } else { [StringComparison]::Ordinal }
        $repositoryPrefix = $repositoryRoot.TrimEnd([IO.Path]::DirectorySeparatorChar, [IO.Path]::AltDirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
        if ($resolved.Equals($repositoryRoot, $comparison) -or $resolved.StartsWith($repositoryPrefix, $comparison)) {
            throw 'Keenetic environment must live outside the repository checkout'
        }
        $item = Get-Item -LiteralPath $resolved -Force -ErrorAction Stop
        if ($item.PSIsContainer -or $item.LinkType -or (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) -or $item.Length -le 0 -or $item.Length -gt 4096) {
            throw 'Keenetic environment must be a small regular non-link file'
        }

        $allowed = [System.Collections.Generic.HashSet[string]]::new([StringComparer]::Ordinal)
        foreach ($key in @('KEENETIC_SSH_HOST', 'KEENETIC_SSH_PORT', 'KEENETIC_SSH_USER', 'KEENETIC_SSH_PASSWORD', 'KEENETIC_SSH_IDENTITY_FILE')) {
            [void]$allowed.Add($key)
        }
        $values = [System.Collections.Generic.Dictionary[string,string]]::new([StringComparer]::Ordinal)
        $lines = @(Get-Content -LiteralPath $resolved -Encoding UTF8 -ErrorAction Stop)
        if ($lines.Count -gt 32) {
            throw 'Keenetic environment has too many lines'
        }
        foreach ($line in $lines) {
            $trimmed = $line.Trim()
            if ($trimmed.Length -eq 0 -or $trimmed.StartsWith('#', [StringComparison]::Ordinal)) {
                continue
            }
            $match = [regex]::Match($trimmed, '^(?<key>[A-Za-z_][A-Za-z0-9_]*)=(?<value>.*)$')
            if (-not $match.Success) {
                throw 'Keenetic environment contains an invalid line'
            }
            $key = $match.Groups['key'].Value
            if (-not $allowed.Contains($key)) {
                throw "Keenetic environment contains an unsupported key: $key"
            }
            if ($values.ContainsKey($key)) {
                throw "Keenetic environment repeats a key: $key"
            }
            $value = $match.Groups['value'].Value.Trim()
            if ($value -match '^"(.*)"$' -or $value -match "^'(.*)'$") {
                $value = $Matches[1]
            } elseif ($value.StartsWith('"') -or $value.EndsWith('"') -or $value.StartsWith("'") -or $value.EndsWith("'")) {
                throw "Keenetic environment has unmatched quoting for: $key"
            }
            $values.Add($key, $value)
        }
        return [pscustomobject]@{ Path = $resolved; Values = $values }
    }

    function Require-KeeneticHost {
        param([string]$Value)
        if ([String]::IsNullOrEmpty($Value) -or $Value.Length -gt 253) {
            throw 'Keenetic SSH host must contain 1..253 ASCII hostname characters'
        }
        foreach ($label in $Value.Split('.')) {
            if ($label.Length -lt 1 -or $label.Length -gt 63 -or $label -notmatch '^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$') {
                throw 'Keenetic SSH host must use 1..63 character alphanumeric/hyphen labels'
            }
        }
    }

    function Require-KeeneticUser {
        param([string]$Value)
        if ($Value -notmatch '^[A-Za-z0-9][A-Za-z0-9_.-]{0,31}$') {
            throw 'Keenetic SSH user must contain 1..32 option-safe ASCII characters'
        }
    }

    function Resolve-KeeneticIdentityFile {
        param([string]$Value, [string]$BaseDirectory)
        if ([String]::IsNullOrWhiteSpace($Value) -or $Value.IndexOf([char]0) -ge 0) {
            throw 'Keenetic SSH identity file is empty or invalid'
        }
        $candidate = if ([IO.Path]::IsPathRooted($Value)) { $Value } else { Join-Path $BaseDirectory $Value }
        $item = Get-Item -LiteralPath $candidate -Force -ErrorAction Stop
        if ($item.PSIsContainer -or $item.LinkType -or (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) -or $item.Length -le 0 -or $item.Length -gt 65536) {
            throw 'Keenetic SSH identity file must be a small regular non-link file'
        }
        return $item.FullName
    }

    $parsed = Read-KeeneticEnvironmentFile -Path $InputPath
    $values = $parsed.Values
    foreach ($required in @('KEENETIC_SSH_HOST', 'KEENETIC_SSH_PORT', 'KEENETIC_SSH_USER')) {
        if (-not $values.ContainsKey($required)) {
            throw "Keenetic environment is missing: $required"
        }
    }
    $hostValue = $values['KEENETIC_SSH_HOST']
    $portValue = $values['KEENETIC_SSH_PORT']
    $userValue = $values['KEENETIC_SSH_USER']
    Require-KeeneticHost -Value $hostValue
    Require-KeeneticUser -Value $userValue
    $portNumber = 0
    if ($portValue -notmatch '^[0-9]+$' -or -not [int]::TryParse($portValue, [ref]$portNumber) -or $portNumber -lt 1 -or $portNumber -gt 65535) {
        throw 'Keenetic SSH port is outside 1..65535'
    }

    $passwordValue = if ($values.ContainsKey('KEENETIC_SSH_PASSWORD')) { $values['KEENETIC_SSH_PASSWORD'] } else { '' }
    if ($passwordValue.IndexOf([char]0) -ge 0) {
        throw 'Keenetic SSH password contains an invalid control character'
    }
    $identityValue = if ($values.ContainsKey('KEENETIC_SSH_IDENTITY_FILE')) {
        Resolve-KeeneticIdentityFile -Value $values['KEENETIC_SSH_IDENTITY_FILE'] -BaseDirectory (Split-Path -Parent $parsed.Path)
    } else {
        ''
    }
    if ([String]::IsNullOrEmpty($passwordValue) -and [String]::IsNullOrEmpty($identityValue)) {
        throw 'Keenetic SSH requires a password or identity file'
    }

    $env:KEENETIC_SSH_HOST = $hostValue
    $env:KEENETIC_SSH_PORT = $portValue
    $env:KEENETIC_SSH_USER = $userValue
    $env:KEENETIC_SSH_TARGET = "$userValue@$hostValue"
    if ([String]::IsNullOrEmpty($passwordValue)) {
        Remove-Item Env:KEENETIC_SSH_PASSWORD -ErrorAction SilentlyContinue
    } else {
        $env:KEENETIC_SSH_PASSWORD = $passwordValue
    }
    if ([String]::IsNullOrEmpty($identityValue)) {
        Remove-Item Env:KEENETIC_SSH_IDENTITY_FILE -ErrorAction SilentlyContinue
    } else {
        $env:KEENETIC_SSH_IDENTITY_FILE = $identityValue
    }
} $EnvFile
