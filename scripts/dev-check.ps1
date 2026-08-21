$ErrorActionPreference = 'Stop'

$repo = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$compose = Join-Path $repo 'docker-compose.dev.yml'

docker compose -f $compose build dev
if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}

docker compose -f $compose run --rm dev bash scripts/dev-check.sh
$checkExit = $LASTEXITCODE
if ($checkExit -ne 0) {
    exit $checkExit
}

git -C $repo diff --check
exit $LASTEXITCODE
