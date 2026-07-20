param(
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]]$GoArgs
)

$ErrorActionPreference = 'Stop'

$projectRoot = Split-Path -Parent $PSScriptRoot
$goRoot = Join-Path $projectRoot '.tools\go1.23.12'
$goExe = Join-Path $goRoot 'bin\go.exe'

if (-not (Test-Path -LiteralPath $goExe)) {
    throw "未找到项目本地 Go：$goExe"
}

$env:GOROOT = $goRoot
$env:GOCACHE = Join-Path $projectRoot '.cache\go-build'
$env:GOMODCACHE = Join-Path $projectRoot '.cache\go-mod'
$env:GOPATH = Join-Path $projectRoot '.cache\gopath'

& $goExe @GoArgs
exit $LASTEXITCODE
