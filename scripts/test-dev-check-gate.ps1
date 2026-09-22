$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

. (Join-Path $PSScriptRoot 'dev-check-git.ps1')

function Assert-Rejected {
    param(
        [Parameter(Mandatory = $true)]
        [scriptblock]$Action,

        [Parameter(Mandatory = $true)]
        [string]$Case
    )

    try {
        & $Action
    } catch {
        return
    }
    throw "exact-HEAD gate accepted $Case"
}

$tempRoot = ConvertTo-XKeenAbsolutePath ([IO.Path]::GetTempPath())
$fixture = Join-Path $tempRoot ("xkeen-dev-check-gate-" + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $fixture | Out-Null

try {
    & git -C $fixture init --quiet
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    & git -C $fixture config user.name 'xkeen-control qualification fixture'
    & git -C $fixture config user.email 'fixture@example.invalid'
    Set-Content -LiteralPath (Join-Path $fixture 'tracked.txt') -Value 'baseline' -NoNewline
    & git -C $fixture add tracked.txt
    & git -C $fixture commit --quiet -m baseline
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

    $head = Assert-XKeenNormalCleanHead -Repository $fixture -Stage 'in clean regression fixture'

    Set-Content -LiteralPath (Join-Path $fixture 'untracked.txt') -Value 'untracked' -NoNewline
    Assert-Rejected -Case 'an untracked file' -Action { Assert-XKeenNormalCleanHead -Repository $fixture -Stage 'in untracked regression fixture' | Out-Null }
    Remove-Item -LiteralPath (Join-Path $fixture 'untracked.txt')

    Set-Content -LiteralPath (Join-Path $fixture 'tracked.txt') -Value 'modified' -NoNewline
    Assert-Rejected -Case 'a modified file' -Action { Assert-XKeenNormalCleanHead -Repository $fixture -Stage 'in modified regression fixture' | Out-Null }
    & git -C $fixture restore tracked.txt

    Set-Content -LiteralPath (Join-Path $fixture 'tracked.txt') -Value 'staged' -NoNewline
    & git -C $fixture add tracked.txt
    Assert-Rejected -Case 'a staged file' -Action { Assert-XKeenNormalCleanHead -Repository $fixture -Stage 'in staged regression fixture' | Out-Null }
    & git -C $fixture restore --staged tracked.txt
    & git -C $fixture restore tracked.txt

    Set-Content -LiteralPath (Join-Path $fixture 'tracked.txt') -Value 'next commit' -NoNewline
    & git -C $fixture add tracked.txt
    & git -C $fixture commit --quiet -m next
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    Assert-Rejected -Case 'a different clean HEAD' -Action { Assert-XKeenNormalCleanHead -Repository $fixture -ExpectedHead $head -Stage 'after regression fixture HEAD change' | Out-Null }

    Write-Host 'exact-HEAD Git gate regression: PASS'
} finally {
    $fixturePath = ConvertTo-XKeenAbsolutePath $fixture
    if (-not $fixturePath.StartsWith($tempRoot + '\', [StringComparison]::OrdinalIgnoreCase)) {
        throw "refusing to remove unexpected fixture path: $fixturePath"
    }
    Remove-Item -LiteralPath $fixturePath -Recurse -Force
}
