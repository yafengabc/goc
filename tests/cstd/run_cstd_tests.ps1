# ============================================================
# run_cstd_tests.ps1 - one-click regression runner for the goc
# C89/C99/C11/C17 unit-test suite (tests\cstd).
#
# Usage:
#   powershell -ExecutionPolicy Bypass -File run_cstd_tests.ps1
# (no arguments; run from anywhere)
#
# What it does, per case file in tests\cstd\cases\:
#   1. copies the .c (and companion *.h) into a per-version build dir
#      under tests\cstd\_build\<ver>\
#   2. compiles+runs with goc  (D:\projects\goc\bin\goc.exe, single mode:
#      -std is accepted but IGNORED, every case uses the same semantics)
#   3. compiles+runs with gcc  (D:\msys\ucrt64\bin\gcc.exe, -std=<ver>
#      equals form - the MSYS2 build rejects the separated form)
#   4. normalizes both outputs (LF/CRLF, trailing whitespace, goc banner
#      lines, missing exit line == code 0, uint32 exit codes) and compares
#   5. classifies against the EXPECTED table below (drift detection):
#        goc compiles + output identical       -> PASS
#        goc compiles + output differs         -> PARTIAL / FAIL (silent gap)
#        goc rejects (expected)                -> FAIL / UNSUPPORTED / PASS(reject)
# Summary per version group; FAIL/UNSUPPORTED print the goc evidence.
# Exit code 0 = everything matches expectations, 1 = any drift.
# ============================================================
$ErrorActionPreference = 'Stop'

$goc = 'D:\projects\goc\bin\goc.exe'
$gcc = 'D:\msys\ucrt64\bin\gcc.exe'
$root = 'D:\projects\goc\tests\cstd'
$cases = Join-Path $root 'cases'
$build = Join-Path $root '_build'

# --- EXPECTED status per case file (authoritative, see CSTD_STATUS.md) ----
# status  : PASS | PARTIAL | FAIL | UNSUPPORTED
# gocFails: $true  = goc is expected to FAIL to compile this file
#           $false = goc must compile; FAIL status may then surface as a
#                    silent output difference (e.g. octal literals)
# extra   : extra gcc flags this file needs (e.g. '-trigraphs')
$expected = @{
    # ---------- C89 core ----------
    'c89_arith.c'       = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c89_types.c'       = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c89_storage.c'     = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c89_enum.c'        = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c89_struct.c'      = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c89_union.c'       = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c89_bitfield.c'    = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c89_init.c'        = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c89_logic.c'       = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c89_bitops.c'      = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c89_assign.c'      = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c89_cond.c'        = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c89_ptr.c'         = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c89_incdec.c'      = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c89_control.c'     = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c89_func.c'        = @{ status = 'PASS';        gocFails = $false; extra = '' }
    # ---------- C89 pp + literals + library ----------
    'c89_pp_obj.c'      = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c89_pp_func.c'     = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c89_pp_cond.c'     = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c89_pp_include.c'  = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c89_pp_misc.c'     = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c89_lit_int.c'     = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c89_lit_char.c'    = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c89_lit_float.c'   = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c89_lit_str.c'     = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c89_lib_stdio.c'   = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c89_lib_string.c'  = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c89_lib_stdlib.c'  = @{ status = 'PASS';         gocFails = $false; extra = '' }
    'c89_lib_ctype.c'   = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c89_lib_limits.c'  = @{ status = 'PARTIAL';     gocFails = $false; extra = '' }
    'c89_lib_varargs.c' = @{ status = 'PASS';        gocFails = $false; extra = '' }
    # ---------- C-stdlib detailed coverage (not version-prefixed; cross-checked) ----------
    # These go through a dedicated 'cstd' group below. std pins the gcc -std
    # (goc ignores it); extra carries any gcc-only defines.
    'cstd_lib_stdio2.c'  = @{ status = 'PASS'; gocFails = $false; extra = '';                std = 'c99' }
    'cstd_lib_wchar.c'   = @{ status = 'PASS'; gocFails = $false; extra = '';                std = 'c99' }
    'cstd_lib_stdlib2.c' = @{ status = 'PASS'; gocFails = $false; extra = '';                std = 'c99' }
    'cstd_lib_string2.c' = @{ status = 'PASS'; gocFails = $false; extra = '-DSTUB_STPCPY';   std = 'c2x' }
    'cstd_lib_math2.c'   = @{ status = 'PASS'; gocFails = $false; extra = '';                std = 'c2x' }
    'cstd_lib_stdbit.c'  = @{ status = 'PASS'; gocFails = $false; extra = '';                std = 'c2x' }
    'cstd_lib_time2.c'   = @{ status = 'PASS'; gocFails = $false; extra = '';                std = 'c2x' }
    'c89_trigraph.c'    = @{ status = 'UNSUPPORTED'; gocFails = $true;  extra = '-trigraphs' }
    'c89_lit_octal.c'   = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c89_pp_elif.c'     = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c89_lit_esc.c'     = @{ status = 'PASS';        gocFails = $false;  extra = '' }
    'c89_lit_dotfloat.c'= @{ status = 'PASS';        gocFails = $false;  extra = '' }
    # ---------- C99 ----------
    'c99_comment.c'     = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c99_longlong.c'    = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c99_bool.c'        = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c99_variadic_macro.c' = @{ status = 'PASS';     gocFails = $false; extra = '' }
    'c99_stdint.c'      = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c99_restrict.c'    = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c99_inline.c'      = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c99_compound.c'    = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c99_compound_file.c' = @{ status = 'UNSUPPORTED'; gocFails = $true; extra = '' }
    'c99_designated.c'  = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c99_vla.c'         = @{ status = 'UNSUPPORTED'; gocFails = $true;  extra = '' }
    'c99_mixdecl.c'     = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c99_hexfloat.c'    = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c99_complex.c'     = @{ status = 'UNSUPPORTED'; gocFails = $true;  extra = '' }
    'c99_funcname.c'    = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c99_pragma.c'      = @{ status = 'UNSUPPORTED'; gocFails = $true;  extra = '' }
    'c99_ucn.c'         = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c99_trailing.c'    = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c99_vacopy.c'      = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c99_math.c'        = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c99_implicit.c'    = @{ status = 'PASS';        gocFails = $true;  extra = '-Wno-error=implicit-int -Wno-error=implicit-function-declaration -fcommon' }
    # ---------- C11 ----------
    'c11_generic.c'     = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c11_static_assert.c' = @{ status = 'PASS';      gocFails = $false; extra = '' }
    'c11_align.c'       = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c11_thread_local.c'= @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c11_thread_local_bad.c'= @{ status = 'PASS';     gocFails = $true;  extra = '' }
    'c11_noreturn.c'    = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c11_anon.c'        = @{ status = 'PASS';        gocFails = $false; extra = '' }
    'c11_uchar.c'       = @{ status = 'UNSUPPORTED'; gocFails = $true;  extra = '' }
    'c11_atomic.c'      = @{ status = 'UNSUPPORTED'; gocFails = $true;  extra = '' }
    'c11_threads.c'     = @{ status = 'PASS';        gocFails = $false; extra = '' }
    # ---------- C17 ----------
    'c17_stdver.c'      = @{ status = 'PARTIAL';     gocFails = $false; extra = '' }
    'c17_smoke.c'       = @{ status = 'PASS';        gocFails = $false; extra = '' }
}

# --- helpers ------------------------------------------------------------
function Norm([string]$s) {
    $lines = ($s -replace "`r", '') -split "`n"
    $lines = @($lines | ForEach-Object { $_.TrimEnd() })
    return (($lines -join "`n").TrimEnd("`n"))
}

function FirstDiff($a, $b) {
    $la = $a -split "`n"; $lb = $b -split "`n"
    $n = [Math]::Min($la.Count, $lb.Count)
    for ($i = 0; $i -lt $n; $i++) { if ($la[$i] -ne $lb[$i]) { return "goc[$($la[$i])] vs gcc[$($lb[$i])]" } }
    return "length goc=$($la.Count) gcc=$($lb.Count)"
}

function Run-Goc($src, $work) {
    # goc run: compiles to a temp dir, runs the program, and passes its exit
    # code through as goc's own exit code (no "(program exited...)" line).
    $out = Join-Path $work 'goc_out.txt'
    $err = Join-Path $work 'goc_err.txt'
    $p = Start-Process -FilePath $goc -ArgumentList ('run "' + $src + '"') -NoNewWindow -Wait -PassThru `
            -RedirectStandardOutput $out -RedirectStandardError $err
    $text = [IO.File]::ReadAllText($out)
    $errText = [IO.File]::ReadAllText($err)
    $prog = ''; $code = $null
    $compiled = ($text -match '(?m)^compiled ')
    if ($p.ExitCode -eq 0 -or $compiled) {
        foreach ($ln in ($text -split "`r?`n")) {
            if ($ln -match '^compiled ') { continue }
            $prog += $ln + "`n"
        }
        # Exit != 0 with a "compiled" line means the program ran and returned
        # that code; without it the compile failed and $code stays $null.
        $code = [uint32]$p.ExitCode
        $prog = Norm $prog
    }
    return @{ exit = $p.ExitCode; prog = $prog; code = $code; err = $errText }
}

function Run-Gcc($src, $std, $extra, $exeName, $work) {
    $exe = Join-Path $work $exeName
    $cout = Join-Path $work 'gcc_comp_out.txt'
    $cerr = Join-Path $work 'gcc_comp_err.txt'
    $argStr = "-std=$std -o `"$exe`" `"$src`" $extra"
    $pg = Start-Process -FilePath $gcc -ArgumentList $argStr -NoNewWindow -Wait -PassThru `
            -RedirectStandardOutput $cout -RedirectStandardError $cerr
    if ($pg.ExitCode -ne 0) {
        return @{ ok = $false; err = [IO.File]::ReadAllText($cerr) }
    }
    $rout = Join-Path $work 'gcc_run_out.txt'
    $rerr = Join-Path $work 'gcc_run_err.txt'
    $pr = Start-Process -FilePath $exe -NoNewWindow -Wait -PassThru `
            -RedirectStandardOutput $rout -RedirectStandardError $rerr
    return @{ ok = $true; code = [uint32]$pr.ExitCode; prog = Norm ([IO.File]::ReadAllText($rout)); err = '' }
}

# --- main ----------------------------------------------------------------
$groups = @(
    @{ prefix = 'c89'; std = 'c89'; label = 'C89' },
    @{ prefix = 'c99'; std = 'c99'; label = 'C99' },
    @{ prefix = 'c11'; std = 'c11'; label = 'C11' },
    @{ prefix = 'c17'; std = 'c17'; label = 'C17' },
    @{ prefix = 'cstd_lib'; std = 'c2x'; label = 'C-stdlib' }
)

$overallOk = $true
$grand = @{ PASS = 0; PARTIAL = 0; FAIL = 0; UNSUPPORTED = 0; MISMATCH = 0 }

foreach ($g in $groups) {
    $files = @(Get-ChildItem -LiteralPath $cases -Filter ($g.prefix + '_*.c') -File | Sort-Object Name)
    if ($files.Count -eq 0) { Write-Output ("[{0}] no cases" -f $g.label); continue }
    $gdir = Join-Path $build $g.prefix
    if (Test-Path $gdir) { Remove-Item -LiteralPath $gdir -Recurse -Force }
    New-Item -ItemType Directory -Force -Path $gdir | Out-Null
    Get-ChildItem -LiteralPath $cases -Filter '*.h' -File | ForEach-Object {
        Copy-Item -LiteralPath $_.FullName -Destination $gdir -Force
    }
    Write-Output ("==== {0} ({1} files) ====" -f $g.label, $files.Count)
    $cnt = @{ PASS = 0; PARTIAL = 0; FAIL = 0; UNSUPPORTED = 0; MISMATCH = 0 }
    foreach ($f in $files) {
        $name = $f.Name
        $exp = $expected[$name]
        if ($null -eq $exp) {
            Write-Output ("{0} : NO-EXPECTED-ENTRY (add to EXPECTED table)" -f $name)
            $cnt.MISMATCH++; $overallOk = $false; continue
        }
        $gocSrc = Join-Path $gdir $name
        Copy-Item -LiteralPath $f.FullName -Destination $gocSrc -Force
        $r = Run-Goc $gocSrc $gdir
        if ($r.exit -ne 0) {
            if ($exp.gocFails) {
                $cnt[$exp.status]++
                $detail = ($r.err -split "`r?`n" | Where-Object { $_ -match 'error|note|warning' } | Select-Object -First 3) -join ' | '
                Write-Output ("{0} : {1}  (goc rejects: {2})" -f $name, $exp.status, $detail)
            } else {
                $cnt.MISMATCH++; $overallOk = $false
                $detail = ($r.err -split "`r?`n" | Select-Object -First 4) -join ' | '
                Write-Output ("{0} : MISMATCH (goc compile failed, expected {1}) :: {2}" -f $name, $exp.status, $detail)
            }
            continue
        }
        $std = if ($exp.std) { $exp.std } else { $g.std }
        $gr = Run-Gcc $gocSrc $std $exp.extra ("gcc_" + [IO.Path]::GetFileNameWithoutExtension($name) + '.exe') $gdir
        if (-not $gr.ok) {
            $cnt.MISMATCH++; $overallOk = $false
            Write-Output ("{0} : MISMATCH (gcc failed to compile, expected {1})" -f $name, $exp.status)
            continue
        }
        if ($exp.gocFails) {
            $cnt.MISMATCH++; $overallOk = $false
            Write-Output ("{0} : MISMATCH (expected goc to reject, but it compiled+runs)" -f $name)
            continue
        }
        if ($r.prog -eq $gr.prog -and $r.code -eq $gr.code) {
            if ($exp.status -eq 'PASS') { $cnt.PASS++; Write-Output ("{0} : PASS" -f $name) }
            else { $cnt.MISMATCH++; $overallOk = $false; Write-Output ("{0} : MISMATCH (outputs match, expected {1})" -f $name, $exp.status) }
        } else {
            if ($exp.status -eq 'PARTIAL' -or $exp.status -eq 'FAIL') {
                $cnt[$exp.status]++
                Write-Output ("{0} : {1}  (output differs: {2})" -f $name, $exp.status, (FirstDiff $r.prog $gr.prog))
            } else {
                $cnt.MISMATCH++; $overallOk = $false
                Write-Output ("{0} : MISMATCH (output differs, expected {1})" -f $name, $exp.status)
            }
        }
    }
    Write-Output ("---- {0} summary: PASS={1} PARTIAL={2} FAIL={3} UNSUPPORTED={4} MISMATCH={5}" -f `
        $g.label, $cnt.PASS, $cnt.PARTIAL, $cnt.FAIL, $cnt.UNSUPPORTED, $cnt.MISMATCH)
    foreach ($k in 'PASS','PARTIAL','FAIL','UNSUPPORTED') { $grand[$k] += $cnt[$k] }
    $grand.MISMATCH += $cnt.MISMATCH
    Remove-Item -LiteralPath $gdir -Recurse -Force -ErrorAction SilentlyContinue
}

Write-Output ('==== TOTAL: PASS={0} PARTIAL={1} FAIL={2} UNSUPPORTED={3} MISMATCH={4}' -f `
    $grand.PASS, $grand.PARTIAL, $grand.FAIL, $grand.UNSUPPORTED, $grand.MISMATCH)
if (-not $overallOk) { Write-Output 'RESULT: DRIFT DETECTED - see MISMATCH lines above'; exit 1 }
Write-Output 'RESULT: ALL CASES MATCH EXPECTATIONS'
exit 0
