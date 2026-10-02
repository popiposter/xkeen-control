function Get-XKeenCheckLanes {
    param([string[]]$Changed)
    $joined = $Changed -join "`n"
    $go = [int]($joined -match '(?m)^(cmd|internal|config|scripts|packaging)/|(?m)^go\.(mod|sum)$|(?m)^Dockerfile\.dev$|(?m)^docker-compose\.dev\.yml$|(?m)^\.github/workflows/')
    $nonTest = @($Changed | Where-Object { $_ -notmatch '_test\.go$' }) -join "`n"
    $helpers = [int]($nonTest -match '(?m)^(scripts|packaging|config|internal|cmd)/|(?m)^Dockerfile\.dev$|(?m)^docker-compose\.dev\.yml$|(?m)^\.github/workflows/')
    $web = [int]($joined -match '(?m)^(web|internal/webassets)/|(?m)^Dockerfile\.dev$|(?m)^docker-compose\.dev\.yml$')
    $unknown = @($Changed | Where-Object { $_ -notmatch '^(cmd|internal|config|scripts|packaging|web|docs|plan|\.github)/|^go\.(mod|sum)$|^Dockerfile\.dev$|^docker-compose\.dev\.yml$|^[^/]+\.md$|^\.git(ignore|attributes)$' })
    if ($unknown.Count) { return [ordered]@{ Go = 1; Helpers = 1; Web = 1; Artifact = 1 } }
    $runtime = @($Changed | Where-Object { $_ -match '^(cmd|internal|config|scripts|packaging)/|^go\.(mod|sum)$|^Dockerfile\.dev$|^docker-compose\.dev\.yml$|^\.github/workflows/' -and $_ -notmatch '_test\.go$|^scripts/test-|\.test\.mjs$' })
    $artifact = [int]($go -and $runtime.Count -gt 0)
    return [ordered]@{ Go = $go; Helpers = $helpers; Web = $web; Artifact = $artifact }
}

function Get-XKeenBrowserSpecs {
    param([string[]]$Changed)
    $specs = [Collections.Generic.HashSet[string]]::new()
    $areas = @{
        'dns-observatory' = 'dns-observatory'; 'routing-policy' = 'routing-policy'
        'performance-policy' = 'performance-policy'; 'system-panel' = 'system-panel'
        'notifications' = 'system-panel'; 'native-xkeen' = 'native-xkeen'
    }
    foreach ($path in $Changed) {
        if ($path -match '^web/tests/[^/]+\.spec\.js$') { [void]$specs.Add($path.Substring(4)); continue }
        if ($path -match '^web/src/([^/]+)\.jsx$' -and $areas.ContainsKey($Matches[1])) {
            [void]$specs.Add("tests/$($areas[$Matches[1]]).spec.js")
            [void]$specs.Add('tests/feature-complete.spec.js')
            [void]$specs.Add('tests/task-workspace.spec.js')
            continue
        }
        if ($path -match '^web/unit/') { continue }
        # Selector/git/package-graph helpers are qualified by dispatch fixtures.
        # Only the actual host/Linux orchestration scripts require browser fallback.
        if ($path -match '^(web/|internal/(httpapi|auth|runtime|webassets)/|Dockerfile\.dev$|docker-compose\.dev\.yml$|scripts/dev-check\.(ps1|sh)$|\.github/workflows/)') { return @('*') }
        if ($path -notmatch '^(cmd|internal|config|scripts|packaging|docs|plan|\.github)/|^go\.(mod|sum)$|^[^/]+\.md$|^\.git(ignore|attributes)$') { return @('*') }
    }
    return @($specs | Sort-Object)
}

function Get-XKeenWebBuild {
    param([string[]]$Changed)
    foreach ($path in $Changed) {
        if ($path -match '^web/(tests|unit)/') { continue }
        if ($path -match '^(web/|internal/webassets/|scripts/|Dockerfile\.dev$|docker-compose\.dev\.yml$|\.github/workflows/)') { return 1 }
        if ($path -notmatch '^(cmd|internal|config|packaging|docs|plan|\.github)/|^go\.(mod|sum)$|^[^/]+\.md$|^\.git(ignore|attributes)$') { return 1 }
    }
    return 0
}
