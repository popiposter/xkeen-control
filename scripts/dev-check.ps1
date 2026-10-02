param(
    [switch]$Full,
    [ValidateSet('auto', 'working', 'commit', 'branch')][string]$Scope = 'auto',
    [string]$Base = 'origin/main',
    [switch]$Plan
)

$ErrorActionPreference = 'Stop'

$repo = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$compose = Join-Path $repo 'docker-compose.dev.yml'
$mode = if ($Full) { '--full' } else { '--fast' }
$exactHead = $null
. (Join-Path $PSScriptRoot 'dev-check-git.ps1')
. (Join-Path $PSScriptRoot 'dev-check-selection.ps1')
function Read-CheckGit([string[]]$Arguments) {
    # Keep native stderr warnings out of machine-readable changed path lists.
    $output = @(& git -C $repo @Arguments)
    if ($LASTEXITCODE -ne 0) { throw "Cannot determine check scope: git $Arguments" }
    return $output
}

if ($Full) {
    $exactHead = Assert-XKeenNormalCleanHead -Repository $repo -Stage 'before qualification'
    & (Join-Path $PSScriptRoot 'test-dev-check-gate.ps1')
    if ($LASTEXITCODE -ne 0) {
        exit $LASTEXITCODE
    }
}

$lanes = [ordered]@{ Go = 1; Helpers = 1; Web = 1; Artifact = 1 }
$changed = @()
$browserSpecs = @('*')
if (-not $Full) {
    $working = @(@(
        Read-CheckGit @('diff', '--name-only', '--no-renames', 'HEAD')
        Read-CheckGit @('ls-files', '--others', '--exclude-standard')
    ) | Where-Object { $_ } | Sort-Object -Unique)
    if ($env:XKEEN_CHECK_BASE) { $Base = $env:XKEEN_CHECK_BASE; $Scope = 'branch' }
    if ($Scope -eq 'auto') { $Scope = if ($working.Count) { 'working' } else { 'commit' } }
    $changed = @($working)
    if ($Scope -eq 'commit') { $changed += @(Read-CheckGit @('diff-tree', '--root', '--no-commit-id', '--name-only', '--no-renames', '-r', '-m', 'HEAD')) }
    if ($Scope -eq 'branch') { $changed += @(Read-CheckGit @('diff', '--name-only', '--no-renames', "$Base...HEAD")) }
    $changed = @($changed | Where-Object { $_ } | Sort-Object -Unique)
    $lanes = Get-XKeenCheckLanes -Changed $changed
    $browserSpecs = @(Get-XKeenBrowserSpecs -Changed $changed)
    if (@($browserSpecs | Where-Object { $_ -ne '*' -and -not (Test-Path -LiteralPath (Join-Path $repo "web/$_")) }).Count) {
        $browserSpecs = @('*') # Deleted/renamed specs cannot become an empty green run.
    }
    if ($browserSpecs.Count) { $lanes.Web = 1 }
}

$checkPlan = [ordered]@{
    mode = $mode.Substring(2); scope = $(if ($Full) { 'all' } else { $Scope })
    head = @(Invoke-XKeenGit $repo @('rev-parse', 'HEAD'))[0]
    changedPaths = @($changed); lanes = $lanes; browserSpecs = @($browserSpecs)
    omitted = $(if ($Full) { @() } else { @('race', 'dependency audit', 'full release qualification') })
}
$checkPlan | ConvertTo-Json -Depth 6
if ($Plan) { exit 0 }
if (-not ($lanes.Go -or $lanes.Helpers -or $lanes.Web -or $lanes.Artifact)) {
    # Documentation-only iteration needs neither Docker nor the application toolchains.
    git -C $repo diff --check
    exit $LASTEXITCODE
}

if ($lanes.Helpers) {
    & (Join-Path $PSScriptRoot 'test-dev-check-plan.ps1')
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    & (Join-Path $PSScriptRoot 'test-dev-check-selection.ps1')
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    & (Join-Path $PSScriptRoot 'test-keenetic-env.ps1')
    if ($LASTEXITCODE -ne 0) {
        exit $LASTEXITCODE
    }
}

docker compose -f $compose build dev
if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}

$runArgs = @(
    'compose', '-f', $compose, 'run', '--rm',
    '-e', "XKEEN_CHECK_GO=$($lanes.Go)",
    '-e', "XKEEN_CHECK_HELPERS=$($lanes.Helpers)",
    '-e', "XKEEN_CHECK_WEB=$($lanes.Web)",
    '-e', "XKEEN_CHECK_ARTIFACT=$($lanes.Artifact)",
    '-e', "XKEEN_CHECK_PATHS=$(ConvertTo-Json -InputObject @($changed) -Compress)",
    '-e', "XKEEN_CHECK_UI=$($browserSpecs -join ' ')",
    'dev', 'bash', 'scripts/dev-check.sh', $mode
)
& docker @runArgs
$checkExit = $LASTEXITCODE
if ($checkExit -ne 0) {
    exit $checkExit
}

git -C $repo diff --check
if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}

if ($Full) {
    $headAfter = Assert-XKeenNormalCleanHead -Repository $repo -ExpectedHead $exactHead -Stage 'after qualification'
    if ($headAfter -ne $exactHead) {
        throw "exact HEAD changed during qualification: before=$exactHead after=$headAfter"
    }
}

exit 0
