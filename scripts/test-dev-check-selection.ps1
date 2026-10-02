$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'dev-check-selection.ps1')
foreach ($case in @(
    @{ Paths = @('packaging/S99xkeen-control'); Expected = @(1,1,0,1) },
    @{ Paths = @('internal/nodes/transaction.go'); Expected = @(1,1,0,1) },
    @{ Paths = @('web/src/native-xkeen.jsx'); Expected = @(0,0,1,0) },
    @{ Paths = @('docs/NATIVE-XKEEN.md'); Expected = @(0,0,0,0) },
    @{ Paths = @('Dockerfile.dev'); Expected = @(1,1,1,1) },
    @{ Paths = @('internal/nodes/transaction_test.go'); Expected = @(1,0,0,0) },
    @{ Paths = @('internal/nodes/transaction_test.go', 'docs/DEVELOPMENT.md'); Expected = @(1,0,0,0) },
    @{ Paths = @('unknown-input'); Expected = @(1,1,1,1) }
)) {
    $got = Get-XKeenCheckLanes -Changed $case.Paths
    $actual = @($got.Go,$got.Helpers,$got.Web,$got.Artifact)
    if (($actual -join ',') -ne ($case.Expected -join ',')) { throw "Incorrect lane selection for $($case.Paths)" }
}
foreach ($case in @(
    @{ Paths = @('web/tests/nodes.spec.js'); Expected = @('tests/nodes.spec.js') },
    @{ Paths = @('web/src/dns-observatory.jsx'); Expected = @('tests/dns-observatory.spec.js','tests/feature-complete.spec.js','tests/task-workspace.spec.js') },
    @{ Paths = @('web/src/main.jsx'); Expected = @('*') },
    @{ Paths = @('internal/httpapi/server.go'); Expected = @('*') },
    @{ Paths = @('internal/nodes/transaction.go'); Expected = @() },
    @{ Paths = @('web/src/new-component.jsx'); Expected = @('*') },
    @{ Paths = @('web/tests/fixtures/new.js'); Expected = @('*') },
    @{ Paths = @('unknown-input'); Expected = @('*') },
    @{ Paths = @('web/src/dashboard-reader.js'); Expected = @('*') }
)) {
    $actual = @(Get-XKeenBrowserSpecs -Changed $case.Paths)
    if (($actual -join ',') -ne ($case.Expected -join ',')) { throw "Incorrect browser selection for $($case.Paths): $actual" }
}
Write-Output 'Development lane selection fixtures passed'
exit 0
