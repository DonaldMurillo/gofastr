# Provision the disposable GitHub Windows runner's database, including vector tests.
$ErrorActionPreference = 'Stop'
if ($env:GITHUB_ACTIONS -ne 'true') { throw 'This script is only for disposable GitHub Actions runners.' }

$pgRoot = 'C:\Program Files\PostgreSQL\17'
$pgBin = Join-Path $pgRoot 'bin'
if (-not (Test-Path (Join-Path $pgBin 'psql.exe'))) { throw 'PostgreSQL 17 is missing from the runner image.' }
$env:PGROOT = $pgRoot
$vswhere = Join-Path ${env:ProgramFiles(x86)} 'Microsoft Visual Studio\Installer\vswhere.exe'
$vsPath = & $vswhere -latest -products '*' -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath
if ($LASTEXITCODE -ne 0 -or -not $vsPath) { throw 'Visual C++ build tools are missing.' }
& (Join-Path $vsPath 'Common7\Tools\Launch-VsDevShell.ps1') -Arch amd64 -HostArch amd64 -SkipAutomaticLocation

$source = Join-Path $env:RUNNER_TEMP ('pgvector-' + [guid]::NewGuid())
git clone --depth 1 --branch v0.8.7 https://github.com/pgvector/pgvector.git $source
if ($LASTEXITCODE -ne 0) { throw 'Could not fetch pgvector.' }
$commit = git -C $source rev-parse HEAD
if ($LASTEXITCODE -ne 0 -or $commit -ne 'f37c13f68b57d2c3472b2214fbcff699d6d34876') { throw 'Unexpected pgvector source revision.' }
Push-Location $source
try {
    nmake /F Makefile.win
    if ($LASTEXITCODE -ne 0) { throw 'pgvector build failed.' }
    nmake /F Makefile.win install
    if ($LASTEXITCODE -ne 0) { throw 'pgvector installation failed.' }
} finally { Pop-Location }

Set-Service postgresql-x64-17 -StartupType Manual
Start-Service postgresql-x64-17
$env:PGPASSWORD = 'root'
& (Join-Path $pgBin 'createdb.exe') -h 127.0.0.1 -U postgres framework_test
if ($LASTEXITCODE -ne 0) { throw 'Test database creation failed.' }
& (Join-Path $pgBin 'psql.exe') -h 127.0.0.1 -U postgres -d framework_test -v ON_ERROR_STOP=1 -c 'CREATE EXTENSION vector; SELECT version();'
if ($LASTEXITCODE -ne 0) { throw 'PostgreSQL or pgvector is unavailable.' }
