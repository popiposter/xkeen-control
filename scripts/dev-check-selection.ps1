function Get-XKeenCheckLanes {
    param([string[]]$Changed)
    $joined = $Changed -join "`n"
    $go = [int]($joined -match '(?m)^(cmd|internal|config|scripts|packaging)/|(?m)^go\.(mod|sum)$|(?m)^Dockerfile\.dev$|(?m)^docker-compose\.dev\.yml$|(?m)^\.github/workflows/')
    $helpers = [int]($joined -match '(?m)^(scripts|packaging|config|internal|cmd)/|(?m)^Dockerfile\.dev$|(?m)^docker-compose\.dev\.yml$|(?m)^\.github/workflows/')
    $web = [int]($joined -match '(?m)^(web|internal/webassets)/|(?m)^Dockerfile\.dev$|(?m)^docker-compose\.dev\.yml$')
    return [ordered]@{ Go = $go; Helpers = $helpers; Web = $web; Artifact = $go }
}
