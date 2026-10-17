param(
    [ValidateSet('all', 'unit', 'browser')]
    [string]$Suite = 'all',
    [ValidateRange(0, 63)]
    [int]$UnitShard = 0,
    [ValidateRange(1, 64)]
    [int]$UnitShardCount = 1,
    [string]$BrowserPackage = '',
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
if ($UnitShard -ge $UnitShardCount) { throw 'UnitShard must be less than UnitShardCount.' }
$unitPackages = @(for ($i = $UnitShard; $i -lt $unitPackages.Count; $i += $UnitShardCount) { $unitPackages[$i] })
if ($BrowserPackage) {
    $selected = @($browserPackages | Where-Object { $_ -eq "github.com/DonaldMurillo/gofastr/$BrowserPackage" })
    if ($BrowserPackage -ne 'cmd/gofastr' -and $selected.Count -ne 1) { throw "Unknown browser package: $BrowserPackage" }
    $browserPackages = $selected
}
$packages | Set-Content (Join-Path $LogDirectory 'packages.txt')
git rev-parse HEAD | Set-Content (Join-Path $LogDirectory 'commit.txt')
$script:windowsTestsFailed = $false

function Invoke-WindowsTests {
    param([string]$Name, [string[]]$PackageSet, [string[]]$TestFlags, [int]$Parallelism = 2, [string]$Timeout = '20m')
    if ($PackageSet.Count -eq 0) { throw "No packages selected for $Name." }
    # Batches stay below Windows' command-line length limit.
    for ($offset = 0; $offset -lt $PackageSet.Count; $offset += 65) {
        $last = [Math]::Min($offset + 64, $PackageSet.Count - 1)
        $batch = @($PackageSet[$offset..$last])
        $tag = '{0}-{1:D3}' -f $Name, $offset
        $stdoutLog = Join-Path $LogDirectory "$tag.jsonl"
        $stderrLog = Join-Path $LogDirectory "$tag.stderr.log"
        Write-Host "Testing $Name packages $offset..$last of $($PackageSet.Count)."
        go test -json -count=1 -p $Parallelism -timeout=$Timeout @TestFlags @batch > $stdoutLog 2> $stderrLog
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
    Invoke-WindowsTests -Name "unit-$UnitShard" -PackageSet $unitPackages -TestFlags @('-skip', $cliBrowser)
}
if ($Suite -in @('all', 'browser')) {
    foreach ($package in $browserPackages) {
        $name = $package.Replace('github.com/DonaldMurillo/gofastr/', '').Replace('/', '-')
        if ($package -eq 'github.com/DonaldMurillo/gofastr/examples/site') {
            Invoke-WindowsTests -Name $name -PackageSet @($package) -Parallelism 1 -TestFlags @('-skip', 'TestAxe')
            Invoke-WindowsTests -Name "$name-axe" -PackageSet @($package) -Parallelism 1 -Timeout '30m' -TestFlags @('-run', 'TestAxe')
        } else {
            Invoke-WindowsTests -Name $name -PackageSet @($package) -Parallelism 1
        }
    }
    if (-not $BrowserPackage -or $BrowserPackage -eq 'cmd/gofastr') {
        Invoke-WindowsTests -Name 'cli-browser' -PackageSet @('github.com/DonaldMurillo/gofastr/cmd/gofastr') -TestFlags @('-run', $cliBrowser) -Parallelism 1 -Timeout '25m'
    }
}
if ($script:windowsTestsFailed) { throw "Windows tests failed. See $LogDirectory." }
Write-Host "Windows $Suite tests passed."
