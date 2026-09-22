Set-StrictMode -Version Latest

function Invoke-XKeenGit {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Repository,

        [Parameter(Mandatory = $true)]
        [string[]]$Arguments
    )

    $output = @(& git -C $Repository @Arguments 2>&1)
    if ($LASTEXITCODE -ne 0) {
        throw "git $($Arguments -join ' ') failed for $Repository"
    }
    return $output
}

function ConvertTo-XKeenAbsolutePath {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )

    return [IO.Path]::GetFullPath($Path.Replace('/', '\')).TrimEnd('\')
}

function Assert-XKeenNormalCleanHead {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory = $true)]
        [string]$Repository,

        [string]$ExpectedHead,

        [Parameter(Mandatory = $true)]
        [string]$Stage
    )

    $repositoryPath = ConvertTo-XKeenAbsolutePath (Resolve-Path -LiteralPath $Repository).Path
    $root = (Invoke-XKeenGit -Repository $repositoryPath -Arguments @('rev-parse', '--show-toplevel') | Select-Object -First 1).Trim()
    $rootPath = ConvertTo-XKeenAbsolutePath $root
    if (-not $rootPath.Equals($repositoryPath, [StringComparison]::OrdinalIgnoreCase)) {
        throw "full qualification must run from the repository root: expected=$repositoryPath actual=$rootPath"
    }

    $gitDir = (Invoke-XKeenGit -Repository $repositoryPath -Arguments @('rev-parse', '--path-format=absolute', '--git-dir') | Select-Object -First 1).Trim()
    $commonDir = (Invoke-XKeenGit -Repository $repositoryPath -Arguments @('rev-parse', '--path-format=absolute', '--git-common-dir') | Select-Object -First 1).Trim()
    $gitDirPath = ConvertTo-XKeenAbsolutePath $gitDir
    $commonDirPath = ConvertTo-XKeenAbsolutePath $commonDir
    if (-not $gitDirPath.Equals($commonDirPath, [StringComparison]::OrdinalIgnoreCase)) {
        throw 'full qualification must run from the normal branch checkout, not a linked Git worktree'
    }

    $head = (Invoke-XKeenGit -Repository $repositoryPath -Arguments @('rev-parse', 'HEAD') | Select-Object -First 1).Trim()
    if ($ExpectedHead -and $head -ne $ExpectedHead) {
        throw "exact HEAD changed during qualification: before=$ExpectedHead after=$head"
    }

    $status = @(Invoke-XKeenGit -Repository $repositoryPath -Arguments @('status', '--porcelain=v1', '--untracked-files=all'))
    if ($status.Count -ne 0) {
        $details = $status -join '; '
        throw "full qualification requires a clean Git worktree and index at $Stage (including non-ignored untracked files): $details"
    }

    Write-Host "exact HEAD $Stage`: $head"
    return $head
}
