param(
    [switch]$Full
)

$ErrorActionPreference = 'Stop'

$repo = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$compose = Join-Path $repo 'docker-compose.dev.yml'
$mode = if ($Full) { '--full' } else { '--fast' }
$exactHead = $null

if ($Full) {
    . (Join-Path $PSScriptRoot 'dev-check-git.ps1')
    $exactHead = Assert-XKeenNormalCleanHead -Repository $repo -Stage 'before qualification'
    & (Join-Path $PSScriptRoot 'test-dev-check-gate.ps1')
    if ($LASTEXITCODE -ne 0) {
        exit $LASTEXITCODE
    }
}

$lanes = [ordered]@{ Go = 1; Helpers = 1; Web = 1; Artifact = 1 }
if (-not $Full) {
    $base = if ($env:XKEEN_CHECK_BASE) { $env:XKEEN_CHECK_BASE } else { 'origin/main' }
    git -C $repo rev-parse --verify "$base^{commit}" *> $null
    if ($LASTEXITCODE -eq 0) {
        $changed = @(
            git -C $repo diff --name-only "$base...HEAD"
            git -C $repo diff --name-only
            git -C $repo diff --cached --name-only
            git -C $repo ls-files --others --exclude-standard
        ) | Where-Object { $_ } | Sort-Object -Unique
        $joined = $changed -join "`n"
        $lanes.Go = [int]($joined -match '(?m)^(cmd|internal|config)/|(?m)^go\.(mod|sum)$|(?m)^scripts/|(?m)^Dockerfile\.dev$|(?m)^docker-compose\.dev\.yml$|(?m)^\.github/workflows/')
        $lanes.Helpers = [int]($joined -match '(?m)^(scripts|config|internal|cmd)/|(?m)^Dockerfile\.dev$|(?m)^docker-compose\.dev\.yml$|(?m)^\.github/workflows/')
        $lanes.Web = [int]($joined -match '(?m)^(web|internal/webassets)/|(?m)^Dockerfile\.dev$|(?m)^docker-compose\.dev\.yml$')
        $lanes.Artifact = $lanes.Go
    } else {
        Write-Warning "comparison base $base is unavailable; fast mode will check every lane"
    }
}

if ($lanes.Helpers) {
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
