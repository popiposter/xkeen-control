$ErrorActionPreference = 'Stop'
$fixtureRoot = Join-Path ([IO.Path]::GetTempPath()) ('xkeen-check-plan-' + [guid]::NewGuid().ToString('N'))
$fixtureScripts = Join-Path $fixtureRoot 'scripts'
New-Item -ItemType Directory -Path $fixtureScripts -Force | Out-Null
try {
    foreach ($name in @('dev-check.ps1','dev-check-git.ps1','dev-check-selection.ps1')) {
        Copy-Item -LiteralPath (Join-Path $PSScriptRoot $name) -Destination $fixtureScripts
    }
    git -C $fixtureRoot init -q
    git -C $fixtureRoot config user.name Fixture
    git -C $fixtureRoot config user.email fixture@example.invalid
    git -C $fixtureRoot config core.autocrlf false
    git -C $fixtureRoot add .
    git -C $fixtureRoot commit -qm base
    if ($LASTEXITCODE) { throw 'fixture initialization failed' }
    New-Item -ItemType Directory -Path (Join-Path $fixtureRoot 'internal/example') -Force | Out-Null
    Set-Content -LiteralPath (Join-Path $fixtureRoot 'internal/example/value_test.go') -Value '// synthetic'
    git -C $fixtureRoot add .
    git -C $fixtureRoot commit -qm test
    $savedBase = $env:XKEEN_CHECK_BASE
    $env:XKEEN_CHECK_BASE = ''
    function Read-Plan([string[]]$Options = @()) {
        $json = & pwsh -NoProfile -File (Join-Path $fixtureScripts 'dev-check.ps1') -Plan @Options
        if ($LASTEXITCODE) { throw 'plan failed' }
        return ($json -join "`n" | ConvertFrom-Json)
    }
    $plan = Read-Plan
    if ($plan.scope -ne 'commit' -or $plan.head.Length -ne 40 -or $plan.lanes.Artifact -ne 0 -or $plan.lanes.Go -ne 1) { throw 'clean commit scope incorrect' }
    Set-Content -LiteralPath (Join-Path $fixtureRoot 'README.md') -Value 'Synthetic documentation'
    $plan = Read-Plan
    if ($plan.scope -ne 'working' -or $plan.changedPaths.Count -ne 1 -or $plan.lanes.Go -ne 0) { throw 'working scope includes previous commit' }
    $plan = Read-Plan @('-Scope','commit')
    if ($plan.changedPaths.Count -ne 2 -or $plan.lanes.Go -ne 1) { throw 'commit scope omitted local edits' }
    $plan = Read-Plan @('-Scope','branch','-Base','HEAD~1')
    if ($plan.changedPaths.Count -ne 2) { throw 'branch scope incorrect' }
    & pwsh -NoProfile -File (Join-Path $fixtureScripts 'dev-check.ps1') -Plan -Scope branch -Base nonexistent *> $null
    if ($LASTEXITCODE -eq 0) { throw 'missing base was accepted' }
    Write-Output 'Development scope plan fixtures passed'
} finally {
    if (Test-Path variable:savedBase) { $env:XKEEN_CHECK_BASE = $savedBase }
    # Exact locally created fixture, resolved inside the system temp directory.
    $resolved = [IO.Path]::GetFullPath($fixtureRoot)
    $tempRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
    if (-not $resolved.StartsWith($tempRoot, [StringComparison]::OrdinalIgnoreCase)) { throw 'unsafe fixture cleanup' }
    Remove-Item -LiteralPath $resolved -Recurse -Force
}
exit 0
