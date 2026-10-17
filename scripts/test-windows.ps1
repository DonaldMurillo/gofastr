param(
    [ValidateSet('all', 'unit', 'browser')]
    [string]$Suite = 'all',
    [string]$LogDirectory = 'dist/windows-tests'
)

$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $false
Set-Location (Join-Path $PSScriptRoot '..')
New-Item -ItemType Directory -Force $LogDirectory | Out-Null
$LogDirectory = (Resolve-Path $LogDirectory).Path

$packages = @(go list ./...)
if ($LASTEXITCODE -ne 0 -or $packages.Count -eq 0) {
    throw 'Could not enumerate Go packages.'
}
# Keep these groups aligned with scripts/test-all.sh and the Linux CI gate.
$heavy = 'gofastr/(examples/site|examples/meridian|examples/tracker|examples/layoutlab|kiln/integration|core-ui/runtime|core-ui/widget|evals/ui-quality/internal/evalrunner|framework/headless)$'
$cliBrowser = 'TestBlueprintCLIGeneratesEntireWorkingAppE2E|TestE2E_HotReload_BrowserAutoRefreshes|TestLivereloadSameBuildIDDoesNotReload|TestE2E_DevLoop_Examples'
$unitPackages = @($packages | Where-Object { $_ -notmatch $heavy })
$browserPackages = @($packages | Where-Object { $_ -match $heavy })
$packages | Set-Content (Join-Path $LogDirectory 'packages.txt')
git rev-parse HEAD | Set-Content (Join-Path $LogDirectory 'commit.txt')
$script:windowsTestsFailed = $false

function Invoke-WindowsTests {
    param([string]$Name, [string[]]$PackageSet, [string[]]$TestFlags, [int]$Parallelism = 2)
    if ($PackageSet.Count -eq 0) { throw "No packages selected for $Name." }
    # Batches stay below Windows' command-line length limit.
    for ($offset = 0; $offset -lt $PackageSet.Count; $offset += 65) {
        $last = [Math]::Min($offset + 64, $PackageSet.Count - 1)
        $batch = @($PackageSet[$offset..$last])
        $tag = '{0}-{1:D3}' -f $Name, $offset
        $stdoutLog = Join-Path $LogDirectory "$tag.jsonl"
        $stderrLog = Join-Path $LogDirectory "$tag.stderr.log"
        Write-Host "Testing $Name packages $offset..$last of $($PackageSet.Count)."
        go test -json -count=1 -p $Parallelism -timeout=20m @TestFlags @batch > $stdoutLog 2> $stderrLog
        if ($LASTEXITCODE -ne 0) {
            $script:windowsTestsFailed = $true
            Write-Host "FAILED $tag. Full output: $stdoutLog"
            Get-Content $stderrLog -Tail 20
            Get-Content $stdoutLog | ForEach-Object {
                try { $event = $_ | ConvertFrom-Json -ErrorAction Stop } catch { return }
                if ($event.Action -eq 'fail') {
                    Write-Host "FAIL $($event.Package) $($event.Test)"
                }
            }
        } else {
            Write-Host "PASS $tag"
        }
    }
}

if ($Suite -in @('all', 'unit')) {
    Invoke-WindowsTests -Name 'unit' -PackageSet $unitPackages -TestFlags @('-skip', $cliBrowser)
}
if ($Suite -in @('all', 'browser')) {
    foreach ($package in $browserPackages) {
        $name = $package.Replace('github.com/DonaldMurillo/gofastr/', '').Replace('/', '-')
        Invoke-WindowsTests -Name $name -PackageSet @($package) -Parallelism 1
    }
    Invoke-WindowsTests -Name 'cli-browser' -PackageSet @('github.com/DonaldMurillo/gofastr/cmd/gofastr') -TestFlags @('-run', $cliBrowser) -Parallelism 1
}
if ($script:windowsTestsFailed) { throw "Windows tests failed. See $LogDirectory." }
Write-Host "Windows $Suite tests passed."
