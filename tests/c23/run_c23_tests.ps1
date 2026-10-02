<#
run_c23_tests.ps1 — one-click rerun of the whole tests\c23 suite.
Usage:
  powershell -NoProfile -ExecutionPolicy Bypass -File D:\projects\goc\tests\c23\run_c23_tests.ps1

What it does:
  - walks cases\*.c, runs _tools\check_case.ps1 for each (goc compile+run vs gcc -std=c2x)
  - prints a PASS/FAIL/UNSUPPORTED/PARTIAL summary table (mechanical verdict driven by
    each case's EXPECT: tag), prints goc diagnostics for FAIL/DIFF/BROKEN_TEST cases
  - writes _build\results.csv for machine consumption
  - cleans stale goc-produced .exe files in cases\ before running

Note: the verdicts here are mechanical. Semantic classification (UNSUPPORTED rationale,
PARTIAL detail) lives in C23_STATUS.md.
#>
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)

$suiteRoot = $PSScriptRoot
$caseDir  = Join-Path $suiteRoot 'cases'
$buildDir = Join-Path $suiteRoot '_build'
$helper   = Join-Path $suiteRoot '_tools\check_case.ps1'
New-Item -ItemType Directory -Force $buildDir | Out-Null
Get-ChildItem $caseDir -Filter *.exe | Remove-Item -Force -ErrorAction SilentlyContinue
Get-ChildItem $caseDir -Filter *.asm | Remove-Item -Force -ErrorAction SilentlyContinue

$files = Get-ChildItem $caseDir -Filter *.c | Sort-Object Name
$rows  = @()
$fails = @()

foreach ($f in $files) {
    $out = @(& $helper -CaseFile $f.FullName -BuildDir $buildDir)
    $vm = $out | Select-String 'MECHANICAL VERDICT: (\S+)(.*)'
    $verdict = '?'
    $detail = ''
    if ($vm) {
        $verdict = $vm.Matches[0].Groups[1].Value
        $detail  = $vm.Matches[0].Groups[2].Value.Trim()
    }
    $rows += [pscustomobject]@{ File = $f.Name; Verdict = $verdict; Detail = $detail }
    if ($verdict -in @('FAIL','DIFF','BROKEN_TEST','TIMEOUT')) {
        $fails += ''
        $fails += ('----- ' + $f.Name + ' [' + $verdict + '] -----')
        $fails += $out
    }
}

Write-Output ''
Write-Output '==== C23 test suite summary (mechanical) ===='
$rows | Format-Table -AutoSize | Out-String -Width 200 | Write-Output
Write-Output '==== verdict counts ===='
$rows | Group-Object Verdict | Sort-Object Name | ForEach-Object { '{0}: {1}' -f $_.Name, $_.Count }

if ($fails.Count) {
    Write-Output ''
    Write-Output '==== goc diagnostics for failed cases ===='
    $fails | ForEach-Object { Write-Output $_ }
}

$csv = Join-Path $buildDir 'results.csv'
$rows | Export-Csv -NoTypeInformation -Encoding UTF8 $csv
Write-Output ''
Write-Output ('CSV written: ' + $csv)

# tidy up regenerable build byproducts (goc produces .exe/.asm next to each case)
Get-ChildItem $caseDir -Filter *.exe | Remove-Item -Force -ErrorAction SilentlyContinue
Get-ChildItem $caseDir -Filter *.asm | Remove-Item -Force -ErrorAction SilentlyContinue
