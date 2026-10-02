<#
check_case.ps1 — run a single C23 test case through both goc and gcc -std=c2x,
dump labeled stdout/stderr/exit codes, and emit a MECHANICAL VERDICT.

Usage:
  powershell -NoProfile -ExecutionPolicy Bypass -File check_case.ps1 -CaseFile <abs path>

The EXPECT tag is read from the case's header comment:
  EXPECT: PASS         -> both compilers must compile+run with identical output/exit
  EXPECT: REJECT       -> a conforming C23 compiler must reject the file
  EXPECT: GOC-REJECT   -> goc is expected to reject a C23-removed construct; gcc's
                          stance (accept/reject) is recorded for reference only
  EXPECT: UNSUPPORTED  -> goc is expected to fail (known design gap), gcc must pass

Note on goc stdout: on a successful compile+run goc prints two "compiled ..." log
lines and (only for a nonzero program exit) a "(program exited with code N)" line;
all of those are stripped before comparing with gcc output.

Mechanical verdicts: PASS / FAIL / UNSUPPORTED / PARTIAL / DIFF / BROKEN_TEST / TIMEOUT.
The mechanical verdict is a hint; the final semantic verdict (UNSUPPORTED rationale,
PARTIAL detail) is recorded by humans in the status documents.
#>
param(
    [Parameter(Mandatory = $true)][string]$CaseFile,
    [string]$BuildDir = ''
)
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)

if (-not $BuildDir) { $BuildDir = Join-Path (Split-Path $PSScriptRoot -Parent) '_build' }

$goc = 'D:\projects\goc\bin\goc.exe'
$gcc = 'D:\msys\ucrt64\bin\gcc.exe'
$caseDir  = Split-Path $CaseFile -Parent
$caseName = [IO.Path]::GetFileNameWithoutExtension($CaseFile)
if (-not (Test-Path $CaseFile)) { Write-Error "case file not found: $CaseFile" }
New-Item -ItemType Directory -Force $BuildDir | Out-Null

function Run-Native {
    param([string]$Exe, [string[]]$ArgList, [string]$WorkDir, [int]$TimeoutMs = 90000)
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $Exe
    $psi.WorkingDirectory = $WorkDir
    $psi.UseShellExecute = $false
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.StandardOutputEncoding = New-Object System.Text.UTF8Encoding($false)
    $psi.StandardErrorEncoding  = New-Object System.Text.UTF8Encoding($false)
    $q = ($ArgList | ForEach-Object { '"' + $_.Replace('"', '\"') + '"' }) -join ' '
    $psi.Arguments = $q
    $p = New-Object System.Diagnostics.Process
    $p.StartInfo = $psi
    [void]$p.Start()
    $so = $p.StandardOutput.ReadToEndAsync()
    $se = $p.StandardError.ReadToEndAsync()
    if (-not $p.WaitForExit($TimeoutMs)) {
        try { $p.Kill() } catch {}
        return @{ Exit = -1; Stdout = ''; Stderr = 'TIMEOUT after ' + $TimeoutMs + 'ms (killed)' }
    }
    $so.Wait(); $se.Wait()
    return @{ Exit = $p.ExitCode; Stdout = $so.Result; Stderr = $se.Result }
}

function Get-ExpectTag {
    param([string]$Text)
    $m = [regex]::Match($Text, 'EXPECT:\s*([A-Za-z-]+)')
    if ($m.Success) { return $m.Groups[1].Value.ToUpper() }
    return 'PASS'
}

function Normalize([string]$s) {
    return (($s -replace "`r", '').TrimEnd())
}

$srcText = [IO.File]::ReadAllText($CaseFile, (New-Object System.Text.UTF8Encoding($false, $true)))
$expect = Get-ExpectTag $srcText

# ---- goc (writes .exe next to the source file) ----
$gocRes = Run-Native $goc @($CaseFile) $caseDir
$gocProgExit = 0
$em = [regex]::Match($gocRes.Stdout, '\(program exited with code (\d+)\)')
if ($em.Success) { $gocProgExit = [int]$em.Groups[1].Value }
elseif ($gocRes.Exit -ne 0) { $gocProgExit = $gocRes.Exit }

# ---- gcc compile + run ----
$gccExe = Join-Path $BuildDir ($caseName + '_gcc.exe')
$gccCompile = Run-Native $gcc @('-std=c2x', '-Wall', '-Wextra', '-o', $gccExe, $CaseFile) $caseDir
$gccRun = @{ Exit = -1; Stdout = ''; Stderr = '(not run: compile failed)' }
if ($gccCompile.Exit -eq 0 -and (Test-Path $gccExe)) {
    $gccRun = Run-Native $gccExe @() $caseDir
}

# ---- mechanical verdict ----
$gocOutClean = (($gocRes.Stdout -split "`r?`n") | Where-Object { $_ -notmatch '^(compiled |\(program exited with code)' }) -join "`n"
$verdict = '?'
$detail = ''
$gccCompileOk = ($gccCompile.Exit -eq 0)

switch ($expect) {
    'GOC-REJECT' {
        if ($gocRes.Exit -ne 0) { $verdict = 'PASS' }
        else { $verdict = 'FAIL'; $detail = 'goc accepted a construct that C23 removed (expected rejection)' }
    }
    'REJECT' {
        if (-not $gccCompileOk) {
            if ($gocRes.Exit -ne 0) { $verdict = 'PASS' }
            else { $verdict = 'FAIL'; $detail = 'goc accepted code that gcc -std=c2x rejects (C23 requires rejection)' }
        } else {
            $verdict = 'BROKEN_TEST'; $detail = 'gcc -std=c2x compiled it; EXPECT: REJECT tag is wrong'
        }
    }
    'UNSUPPORTED' {
        if (-not $gccCompileOk) {
            $verdict = 'BROKEN_TEST'; $detail = 'gcc failed to compile: ' + ((($gccCompile.Stderr -split "`r?`n") | Select-Object -First 3) -join ' | ')
        } elseif ($gocRes.Exit -ne 0) {
            $verdict = 'UNSUPPORTED'
        } else {
            $same = (Normalize $gocOutClean) -eq (Normalize $gccRun.Stdout)
            if ($same -and $gocProgExit -eq $gccRun.Exit) { $verdict = 'PASS'; $detail = 'EXPECT: UNSUPPORTED but goc compiled+ran it, output matches gcc' }
            else { $verdict = 'DIFF'; $detail = 'EXPECT: UNSUPPORTED but goc compiled+ran it, output/exit differs from gcc' }
        }
    }
    default {
        if (-not $gccCompileOk) {
            $verdict = 'BROKEN_TEST'; $detail = 'gcc failed to compile: ' + ((($gccCompile.Stderr -split "`r?`n") | Select-Object -First 3) -join ' | ')
        } elseif ($gocRes.Exit -ne 0) {
            $verdict = 'FAIL'; $detail = 'goc failed to compile/run'
        } else {
            $same = (Normalize $gocOutClean) -eq (Normalize $gccRun.Stdout)
            if ($same -and $gocProgExit -eq $gccRun.Exit) { $verdict = 'PASS' }
            else {
                $gs = [regex]::Match($gocOutClean, 'SUMMARY:\s*(\d+)/(\d+)')
                $cs = [regex]::Match($gccRun.Stdout, 'SUMMARY:\s*(\d+)/(\d+)')
                if ($gs.Success -and $cs.Success -and $gs.Value -ne $cs.Value) {
                    $verdict = 'PARTIAL'; $detail = 'SUMMARY mismatch: goc=' + $gs.Value + ' gcc=' + $cs.Value
                } else {
                    $verdict = 'DIFF'; $detail = 'stdout or exit code mismatch (goc exit=' + $gocProgExit + ', gcc exit=' + $gccRun.Exit + ')'
                }
            }
        }
    }
}

# ---- report ----
Write-Output ('=== {0}  EXPECT={1} ===' -f $caseName, $expect)
Write-Output ('-- goc -- exit={0} (program exit={1})' -f $gocRes.Exit, $gocProgExit)
Write-Output 'stdout:'
Write-Output $gocRes.Stdout
Write-Output 'stderr:'
Write-Output $gocRes.Stderr
Write-Output ('-- gcc compile -- exit={0}' -f $gccCompile.Exit)
Write-Output 'stderr:'
Write-Output $gccCompile.Stderr
Write-Output ('-- gcc run -- exit={0}' -f $gccRun.Exit)
Write-Output 'stdout:'
Write-Output $gccRun.Stdout
Write-Output 'stderr:'
Write-Output $gccRun.Stderr
Write-Output ('MECHANICAL VERDICT: {0} {1}' -f $verdict, $detail)
