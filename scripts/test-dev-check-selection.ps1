$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'dev-check-selection.ps1')
foreach ($case in @(
    @{ Paths = @('packaging/S99xkeen-control'); Expected = @(1,1,0,1) },
    @{ Paths = @('internal/nodes/transaction.go'); Expected = @(1,1,0,1) },
    @{ Paths = @('web/src/native-xkeen.jsx'); Expected = @(0,0,1,0) },
    @{ Paths = @('docs/NATIVE-XKEEN.md'); Expected = @(0,0,0,0) },
    @{ Paths = @('Dockerfile.dev'); Expected = @(1,1,1,1) }
)) {
    $got = Get-XKeenCheckLanes -Changed $case.Paths
    $actual = @($got.Go,$got.Helpers,$got.Web,$got.Artifact)
    if (($actual -join ',') -ne ($case.Expected -join ',')) { throw "Incorrect lane selection for $($case.Paths)" }
}
Write-Output 'Development lane selection fixtures passed'
exit 0
