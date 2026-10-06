// Command gocregress is a Go re-implementation of run_tests.sh: it builds the
// goc / goa / elfcheck toolchain, then runs the end-to-end example legs
// (Windows + Linux targets, at -O0 / -O1 / -Os), the goa assembler example
// suite, and the three Go unit-test modules. It is meant as a drop-in
// experiment beside the bash harness -- it does NOT replace run_tests.sh.
//
// The behavioural guardrail is unchanged from the bash version: every
// optimisation pass must keep the goldens in src/expected/<name>.txt true at
// -O0, -O1 and -Os. The Linux legs are executed under QEMU's TCG core (via
// tools/ucrun/ucrun.exe, a standalone C program linked against libunicorn.dll),
// not by a hand-written interpreter, so they prove real instruction semantics
// rather than agreement with our own codegen. Unicorn is a C++ library that
// installs its own SEH/exception handling, which conflicts with Go's Vectored
// Exception Handler; it therefore cannot be hosted inside the Go process
// (neither via pure syscall nor cgo) and must run as a separate C process.
//
// Usage:
//
//	go run ./tools/gocregress            (or: go build -o bin/gocregress.exe ./tools/gocregress && ./bin/gocregress.exe)
//	gocregress -p=false                  sequential
//	gocregress -j 4                      limit parallelism to 4 jobs
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// winOnly are the examples that cannot be exercised on the Linux (ELF64)
// target by THIS harness and are therefore skipped there. Most import
// Windows-only DLLs (user32/gdi32/kernel32). c11_threads_basic is here only
// because the Linux leg runs under ucrun, which implements neither clone nor
// futex -- the program itself now works on a real Linux kernel (verified under
// WSL after the r10 syscall ABI fix); a WSL-based Linux leg would run it.
//
// winOnly also relaxes the golden rule: an entry with no src/expected file is
// a compile-only smoke test, while one that has a golden (c11_threads_basic)
// is still run and compared -- but only on the Windows leg.
var winOnly = map[string]bool{
	"wintest":           true,
	"winbox":            true,
	"winreg":            true,
	"c11_threads_basic": true,
}

// repoRoot is set in main() and read by childEnv()/buildArtifacts().
var repoRoot string

func main() {
	root := flag.String("root", "", "repo root (default: auto-detect from cwd)")
	parallel := flag.Bool("p", true, "run legs/suites in parallel")
	jobs := flag.Int("j", 0, "max parallel jobs (0 = all)")
	flag.Parse()

	var err error
	repoRoot, err = resolveRoot(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gocregress:", err)
		os.Exit(2)
	}

	// Fresh output dirs. Each of the six example legs gets its OWN directory so
	// concurrent legs never share a working dir (which made goc's intermediate
	// .asm/temp files collide and produced spurious cross-target assemble errors).
	for _, d := range []string{
		"bin/goc-out", "bin/goc-out-o1", "bin/goc-out-os",
		"bin/goc-out-linux", "bin/goc-out-linux-o1", "bin/goc-out-linux-os",
	} {
		os.RemoveAll(filepath.Join(repoRoot, d))
	}
	os.MkdirAll(filepath.Join(repoRoot, "bin"), 0o755)

	fmt.Println("== building goc ==")
	if err := buildTool("src/goc", "bin/goc.exe", "./cmd/goc"); err != nil {
		fmt.Println("BUILD FAILED:", err)
		os.Exit(1)
	}
	fmt.Println("== building goa ==")
	if err := buildTool("src/goa", "bin/goa.exe", "./cmd/goa"); err != nil {
		fmt.Println("GOA BUILD FAILED:", err)
		os.Exit(1)
	}
	fmt.Println("== building elfcheck ==")
	if err := buildTool("tools", "bin/elfcheck.exe", "./elfcheck"); err != nil {
		fmt.Println("ELFCHECK BUILD FAILED:", err)
		os.Exit(1)
	}

	ucrunExe, ucrunOK := buildUcrun()
	if ucrunOK {
		fmt.Printf("== linux runner: %s (unicorn ELF emulator) ==\n", ucrunExe)
	} else {
		fmt.Println("== WARNING: could not build ucrun.exe; the Linux legs will be SKIPPED. ==")
		fmt.Println("==          Install an MSYS2 ucrt64 gcc (with the unicorn package) to enable them. ==")
	}

	type task struct {
		name string
		fn   func() (report string, pass, fail, rc int)
	}
	tasks := []task{
		{"windows target (-O0)", func() (string, int, int, int) {
			return runLeg("", "", false, nil, ucrunExe)
		}},
		{"windows target -O1 (IR peephole on)", func() (string, int, int, int) {
			return runLeg("-o1", "O1/", false, []string{"-O1"}, ucrunExe)
		}},
		{"windows target -Os (size first)", func() (string, int, int, int) {
			return runLeg("-os", "Os/", false, []string{"-Os"}, ucrunExe)
		}},
		{"linux target -O0 (ELF64 under QEMU/Unicorn)", func() (string, int, int, int) {
			return runLeg("-linux", "linux/", true, nil, ucrunExe)
		}},
		{"linux target -O1", func() (string, int, int, int) {
			return runLeg("-linux-o1", "O1/linux/", true, []string{"-O1"}, ucrunExe)
		}},
		{"linux target -Os", func() (string, int, int, int) {
			return runLeg("-linux-os", "Os/linux/", true, []string{"-Os"}, ucrunExe)
		}},
		{"goa examples (via src/goa/run_tests.sh)", func() (string, int, int, int) {
			return runGoa(ucrunExe)
		}},
		{"unit tests: src/goc", func() (string, int, int, int) {
			return runUnit("src/goc")
		}},
		{"unit tests: src/goa", func() (string, int, int, int) {
			return runUnit("src/goa")
		}},
		{"unit tests: tools", func() (string, int, int, int) {
			return runUnit("tools")
		}},
	}

	resReport := make([]string, len(tasks))
	resPass := make([]int, len(tasks))
	resFail := make([]int, len(tasks))

	var wg sync.WaitGroup
	sem := make(chan struct{}, *jobs)
	if *jobs <= 0 {
		sem = make(chan struct{}, len(tasks))
	}
	if !*parallel {
		sem = make(chan struct{}, 1)
	}
	var printMu sync.Mutex
	logf := func(format string, a ...interface{}) {
		printMu.Lock()
		defer printMu.Unlock()
		fmt.Printf(format, a...)
	}

	for i := range tasks {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			logf("== running %s ==\n", tasks[i].name)
			rep, p, f, _ := tasks[i].fn()
			resReport[i], resPass[i], resFail[i] = rep, p, f
			logf("%s", rep)
		}(i)
	}
	wg.Wait()

	totalPass, totalFail := 0, 0
	for i := range tasks {
		totalPass += resPass[i]
		totalFail += resFail[i]
	}
	fmt.Println("-----------------------------")
	fmt.Printf("pass=%d fail=%d\n", totalPass, totalFail)
	if totalFail > 0 {
		os.Exit(1)
	}
}

// runLeg compiles every example in src/examples with the given goc flags, runs
// it, and byte-compares stdout against src/expected/<name>.txt. It mirrors the
// run_win_leg / run_linux_leg shells from run_tests.sh. dirSuffix / label
// separate output dirs and report prefixes so parallel legs never clobber each
// other. targetLinux switches on the ELF64 path (compile with -target linux,
// execute via tools/ucrun/ucrun.exe, a standalone unicorn-based ELF runner).
func runLeg(dirSuffix, label string, targetLinux bool, flags []string, ucrunExe string) (string, int, int, int) {
	var sb strings.Builder
	outDir := filepath.Join(repoRoot, "bin", "goc-out"+dirSuffix)
	os.MkdirAll(outDir, 0o755)
	env := childEnv()
	goc := filepath.Join(repoRoot, "bin", "goc.exe")

	// One case per example .c, plus one multi-translation-unit build: every
	// .c in src/examples/multi is compiled together into a single program,
	// which is how `goc a.c b.c` behaves.
	type exCase struct {
		name string
		srcs []string
	}
	var cases []exCase
	examples, _ := filepath.Glob(filepath.Join(repoRoot, "src", "examples", "*.c"))
	sort.Strings(examples)
	for _, src := range examples {
		cases = append(cases, exCase{strings.TrimSuffix(filepath.Base(src), ".c"), []string{src}})
	}
	if multi, _ := filepath.Glob(filepath.Join(repoRoot, "src", "examples", "multi", "*.c")); len(multi) > 0 {
		sort.Strings(multi)
		cases = append(cases, exCase{"multi", multi})
	}

	pass, fail := 0, 0
	for _, ex := range cases {
		name, srcs := ex.name, ex.srcs
		exp := filepath.Join(repoRoot, "src", "expected", name+".txt")
		_, expErr := os.Stat(exp)

		if targetLinux && winOnly[name] {
			fmt.Fprintf(&sb, "SKIP  %s%s  (imports Windows DLLs)\n", label, name)
			continue
		}
		if expErr != nil {
			if winOnly[name] {
				// No golden: prove the Win32 imports compile + link.
				cargs := append([]string{"-c"}, flags...)
				cargs = append(cargs, "-o", outDir)
				cargs = append(cargs, srcs...)
				_, cerr, rc, err := runCmd(repoRoot, env, goc, cargs...)
				if err != nil || rc != 0 {
					fmt.Fprintf(&sb, "FAIL  %s%s (compile): %s\n", label, name, string(cerr))
					fail++
				} else {
					fmt.Fprintf(&sb, "ok    %s%-10s compile-only\n", label, name)
					pass++
				}
			} else {
				fmt.Fprintf(&sb, "SKIP  %s%s (no src/expected/%s.txt)\n", label, name, name)
			}
			continue
		}

		// Compile.
		cargs := append([]string{"-c"}, flags...)
		if targetLinux {
			cargs = append(cargs, "-target", "linux")
		}
		// A single file picks up its own name from -o <dir>; a multi-file
		// build has no single source to name itself after, so pass the
		// explicit output file instead.
		outArg := outDir
		if len(srcs) > 1 {
			outArg = filepath.Join(outDir, name)
		}
		cargs = append(cargs, "-o", outArg)
		cargs = append(cargs, srcs...)
		_, cerr, rc, err := runCmd(repoRoot, env, goc, cargs...)
		if err != nil || rc != 0 {
			fmt.Fprintf(&sb, "FAIL  %s%s (compile): %s\n", label, name, string(cerr))
			fail++
			continue
		}

		var got []byte
		var runRc int
		if targetLinux {
			if ucrunExe == "" {
				// No Linux runner available: nothing to execute against.
				fmt.Fprintf(&sb, "SKIP  %s%s (no ucrun.exe; Linux runner unavailable)\n", label, name)
				continue
			}
			// ELF structure check (headers/segments only); output correctness
			// is decided by the unicorn run below, so we ignore this rc.
			runCmd(repoRoot, env, filepath.Join(repoRoot, "bin", "elfcheck.exe"),
				"--structure-only", filepath.Join(outDir, name))
			// ucrun prints the program's stdout to its stdout (what we diff)
			// and its own diagnostics to stderr.
			var serr []byte
			got, serr, runRc, _ = runCmd(outDir, env, ucrunExe,
				filepath.Join(outDir, name))
			got = stripCR(got)
			_ = serr
		} else {
			exe := filepath.Join(outDir, name+".exe")
			got, _, runRc, _ = runCmd(outDir, env, exe)
			got = stripCR(got)
			// A freshly written exe can be momentarily locked by the real-time
			// antivirus scan; exec then fails with 126/127. Retry with
			// backoff: under parallel load one retry is not always enough.
			for try := 0; (runRc == 126 || runRc == 127) && try < 4; try++ {
				time.Sleep(time.Second)
				got, _, runRc, _ = runCmd(outDir, env, exe)
				got = stripCR(got)
			}
		}

		expb, _ := os.ReadFile(exp)
		if runRc != 0 || !bytes.Equal(expb, got) {
			fmt.Fprintf(&sb, "FAIL  %s%s (exit=%d)\n", label, name, runRc)
			sb.WriteString(diffText(expb, got))
			fail++
		} else {
			prod := filepath.Join(outDir, name)
			if !targetLinux {
				prod += ".exe"
			}
			if fi, e := os.Stat(prod); e == nil {
				fmt.Fprintf(&sb, "ok    %s%-10s %6d bytes\n", label, name, fi.Size())
			} else {
				fmt.Fprintf(&sb, "ok    %s%s\n", label, name)
			}
			pass++
		}
	}
	return sb.String(), pass, fail, 0
}

// runGoa shells out to goa/run_tests.sh (which assembles each goa example,
// runs it natively on Windows, and under QEMU on Linux). We reuse it rather
// than grow a second copy here.
func runGoa(ucrunExe string) (string, int, int, int) {
	env := childEnv()
	if ucrunExe != "" {
		env = append(env, "UCRUN="+ucrunExe)
	}
	// Headless harness: never block on the interactive msgboxcheck desktop.
	if os.Getenv("GOC_SKIP_MSGBOX") == "" {
		env = append(env, "GOC_SKIP_MSGBOX=1")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		bash = "bash"
	}
	out, serr, rc, _ := runCmd(repoRoot, env, bash, filepath.Join(repoRoot, "src", "goa", "run_tests.sh"))
	report := string(out) + string(serr)
	if rc == 0 {
		return report + "ok    goa examples suite\n", 1, 0, 0
	}
	return report + "FAIL  goa examples suite\n", 0, 1, rc
}

// runUnit runs `go test -count=1 -v ./...` in one Go module and counts the
// --- PASS lines.
func runUnit(mod string) (string, int, int, int) {
	env := childEnv()
	dir := filepath.Join(repoRoot, mod)
	out, _, rc, _ := runCmd(dir, env, "go", "test", "-count=1", "-v", "./...")
	var sb strings.Builder
	sb.WriteString(string(out))
	n := countPass(string(out))
	if rc == 0 {
		fmt.Fprintf(&sb, "ok    unit: %-10s (%d tests)\n", mod, n)
		return sb.String(), 1, 0, 0
	}
	fmt.Fprintf(&sb, "FAIL  unit: %s\n", mod)
	return sb.String(), 0, 1, rc
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// buildTool builds one module's command into out. The package argument is the
// command's directory relative to the module root, and it is not always ".":
// src/goc is a library package (package compiler) whose main lives in
// cmd/goc, so building "." there produces a Go archive -- a file that starts
// with "!<arch>" -- and every later compile then fails with a shell-level
// "syntax error near unexpected token" that says nothing about the compiler.
func buildTool(dir, out, pkg string) error {
	env := childEnv()
	_, stderr, rc, err := runCmd(filepath.Join(repoRoot, dir), env, "go",
		"build", "-trimpath", "-ldflags=-s -w", "-o", filepath.Join(repoRoot, out), pkg)
	if err != nil {
		return err
	}
	if rc != 0 {
		return fmt.Errorf("%s", stderr)
	}
	return nil
}

// runCmd runs name with args in dir, capturing stdout/stderr separately and the
// process exit code.
func runCmd(dir string, env []string, name string, args ...string) (stdout, stderr []byte, rc int, err error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = env
	var so, se bytes.Buffer
	cmd.Stdout = &so
	cmd.Stderr = &se
	err = cmd.Run()
	stdout, stderr = so.Bytes(), se.Bytes()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			rc = ee.ExitCode()
		} else {
			rc = -1
		}
	}
	return
}

// childEnv returns a sanitized environment for subprocesses. Under the sandbox
// TMP/TEMP/HOME are stripped, which breaks `go build`; we restore them with
// sensible defaults (mirroring run_tests.sh's preamble).
func childEnv() []string {
	set := map[string]string{}
	home := os.Getenv("HOME")
	if home == "" || !dirExists(home) {
		home = repoRoot
	}
	tmp := firstNonEmpty(os.Getenv("TMP"), os.Getenv("TEMP"), "/tmp")
	cache := os.Getenv("GOCACHE")
	if cache == "" {
		cache = filepath.Join(repoRoot, ".gocache")
	}
	set["HOME"] = home
	set["TMP"] = tmp
	set["TEMP"] = tmp
	set["TMPDIR"] = tmp
	set["GOTMPDIR"] = tmp
	set["GOCACHE"] = cache
	return overrideEnv(os.Environ(), set)
}

func overrideEnv(base []string, set map[string]string) []string {
	drop := map[string]bool{}
	for k := range set {
		drop[k] = true
	}
	out := make([]string, 0, len(base)+len(set))
	for _, e := range base {
		if i := strings.IndexByte(e, '='); i > 0 {
			if drop[e[:i]] {
				continue
			}
		}
		out = append(out, e)
	}
	for k, v := range set {
		out = append(out, k+"="+v)
	}
	return out
}

// findGcc locates the MSYS2 ucrt64 gcc (the only toolchain that can compile the
// standalone unicorn-based ucrun.exe). Honours GOC_CC, then known locations.
func findGcc() string {
	if p := os.Getenv("GOC_CC"); p != "" {
		if _, err := os.Stat(nativePath(p)); err == nil {
			return nativePath(p)
		}
	}
	cands := []string{
		"/d/msys/ucrt64/bin/gcc.exe",
		"/c/msys64/ucrt64/bin/gcc.exe",
		"gcc",
	}
	for _, c := range cands {
		if c == "" {
			continue
		}
		path := nativePath(c)
		if _, err := os.Stat(path); err == nil {
			return path
		}
		if p, e := exec.LookPath(c); e == nil {
			return p
		}
	}
	return ""
}

// buildUcrun compiles tools/ucrun/ucrun.c into bin/ucrun.exe, statically
// linked against libunicorn.a so it is a single self-contained file (no
// runtime DLLs needed). Returns the path and whether it is usable. It is a
// no-op (returns the existing binary) when the binary is already up to date.
func buildUcrun() (string, bool) {
	exe := filepath.Join(repoRoot, "bin", "ucrun.exe")
	src := filepath.Join(repoRoot, "tools", "ucrun", "ucrun.c")
	if fi, err := os.Stat(exe); err == nil {
		if si, err := os.Stat(src); err != nil || fi.ModTime().After(si.ModTime()) {
			return exe, true
		}
	}
	gcc := findGcc()
	if gcc == "" {
		return "", false
	}
	// Derive the include/lib dirs from the gcc location when it is the MSYS2
	// ucrt64/mingw64 toolchain (its sysroot holds the unicorn headers/libs).
	// The static libunicorn.a is C++: link libstdc++/libgcc/winpthread in as
	// well so the MinGW CRT runs its global constructors. (A *dynamically*
	// linked ucrun.exe also works, but then four runtime DLLs must sit beside
	// it; static keeps bin/ clean.)
	inc, lib := "", ""
	low := strings.ToLower(gcc)
	if strings.Contains(low, "ucrt64") || strings.Contains(low, "mingw64") {
		root := filepath.Dir(filepath.Dir(gcc)) // .../ucrt64/bin/gcc -> .../ucrt64
		inc = filepath.Join(root, "include")
		lib = filepath.Join(root, "lib")
	}
	args := []string{src, "-o", exe}
	if inc != "" {
		args = append(args, "-I", inc)
	}
	staticUc := ""
	if lib != "" {
		p := filepath.Join(lib, "libunicorn.a")
		if _, err := os.Stat(p); err == nil {
			staticUc = p
		} else {
			args = append(args, "-L", lib)
		}
	}
	if staticUc != "" {
		// Static unicorn is C++: link the C++ runtime in too so the MinGW CRT
		// runs its global constructors.
		args = append(args, staticUc, "-static", "-lstdc++", "-lgcc", "-lwinpthread")
	}
	args = append(args, "-lunicorn")
	cmd := exec.Command(gcc, args...)
	cmd.Dir = repoRoot
	cmd.Env = childEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "ucrun: build failed: %v\n%s\n", err, out)
		return "", false
	}
	return exe, true
}

func resolveRoot(explicit string) (string, error) {
	if explicit != "" {
		return nativePath(explicit), nil
	}
	// Prefer the executable's own location: when built into <root>/bin/, the
	// parent of bin/ is the repo root. This is robust to the cwd being in
	// MSYS form (/d/projects/goc) which Windows APIs do not understand.
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		if strings.EqualFold(filepath.Base(dir), "bin") {
			return filepath.Dir(dir), nil
		}
	}
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		nd := nativePath(dir)
		if dirExists(filepath.Join(nd, "run_tests.sh")) &&
			dirExists(filepath.Join(nd, "src", "goc", "main.go")) {
			return nd, nil
		}
		parent := filepath.Dir(nd)
		if parent == nd {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("could not find repo root (looked for run_tests.sh + src/main.go); pass -root")
}

// nativePath converts a MSYS/Cygwin-style path (/d/projects/goc) to the native
// Windows form (D:/projects/goc) that the OS APIs and child processes expect.
// Plain native paths and unknown forms pass through unchanged.
func nativePath(p string) string {
	if len(p) >= 3 && p[0] == '/' && p[2] == '/' && p[1] >= 'a' && p[1] <= 'z' {
		return strings.ToUpper(string(p[1])) + ":" + p[2:]
	}
	if len(p) >= 2 && p[1] == ':' {
		return p // already a Windows path
	}
	if c, err := exec.LookPath("cygpath"); err == nil {
		if out, err := exec.Command(c, "-w", p).Output(); err == nil && len(out) > 0 {
			return strings.TrimSpace(string(out))
		}
	}
	return p
}

func diffText(expb, got []byte) string {
	if d, err := exec.LookPath("diff"); err == nil {
		expF, e1 := os.CreateTemp("", "exp-*.txt")
		gotF, e2 := os.CreateTemp("", "got-*.txt")
		if e1 == nil && e2 == nil {
			expF.Write(expb)
			expF.Close()
			gotF.Write(got)
			gotF.Close()
			out, _ := exec.Command(d, "-u", expF.Name(), gotF.Name()).CombinedOutput()
			os.Remove(expF.Name())
			os.Remove(gotF.Name())
			if len(out) > 0 {
				return string(out)
			}
		}
	}
	return simpleDiff(expb, got)
}

// simpleDiff shows the first block of lines that differs between expected and
// got (common prefix/suffix trimmed). Used when `diff` is unavailable.
func simpleDiff(expb, got []byte) string {
	ea := strings.Split(string(expb), "\n")
	ga := strings.Split(string(got), "\n")
	i := 0
	for i < len(ea) && i < len(ga) && ea[i] == ga[i] {
		i++
	}
	j, k := len(ea), len(ga)
	for j > i && k > i && ea[j-1] == ga[k-1] {
		j--
		k--
	}
	var sb strings.Builder
	sb.WriteString("--- expected\n")
	for x := i; x < j; x++ {
		sb.WriteString("- " + ea[x] + "\n")
	}
	sb.WriteString("--- got\n")
	for x := i; x < k; x++ {
		sb.WriteString("+ " + ga[x] + "\n")
	}
	return sb.String()
}

func countPass(out string) int {
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "--- PASS") {
			n++
		}
	}
	return n
}

func stripCR(b []byte) []byte {
	return bytes.ReplaceAll(b, []byte("\r"), nil)
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
