package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"goa"
)

// goc: a tiny C compiler.
//
// Pipeline: lex -> parse -> generate x86-64 assembly (Intel syntax) ->
// goa assembles it straight into a native executable. No gcc, no libc.
//
//   - default (Windows): PE32+, kernel32 only
//   - -target linux:     static ELF64, raw syscalls only
//
// goc also accepts a large subset of the gcc/clang command line so it can drop
// into existing build scripts as a drop-in `cc`. Unknown options that have no
// meaning for a single-translation-unit compiler (optimisation levels, warning
// flags, standard selection, machine/linker flags, ...) are accepted and
// ignored rather than rejected. There is no separate linking stage: `goc a.c
// b.c` parses each file as its own translation unit, merges the declarations
// (statics stay private to their file, duplicate externals are an error) and
// compiles the result into one executable -- so goc still cannot consume .o
// files or link objects together (see src/multi.go).
//
// Usage (goc convenience front-end):
//
//	goc file.c                 compile only (emit file.exe)
//	goc a.c b.c                compile several translation units into one exe
//	goc run file.c [args...]   compile to a temp dir, run with args (go run)
//	goc -c file.c              compile only (produce file.exe / file)
//	goc -S file.c              emit assembly only (produce file.asm)
//	goc -o app file.c          write the executable to app(..exe)
//	goc -target linux file.c   produce a Linux ELF64 instead
//
// gcc/clang-compatible subset:
//
//	goc  -c|-S|-E -O* -Wall -Werror -Wl,* -std=* -m* -g -static -pthread
//	     -f* -s -pipe -v -DNAME[=val] -I<dir> -L<dir> -l<lib> -o <file>
//	     -target <win|linux> [-shared -M* -MD -MP]
//
// Since 2026-10-02 plain `goc file.c` also compiles without auto-running
// (use `goc run file.c` to execute), so the `cc`/`cc.exe` personality no
// longer differs from the default: both just emit the executable.
func main() {
	if len(os.Args) > 1 && os.Args[1] == "run" {
		runCmd(os.Args[2:])
		return // runCmd always exits
	}

	cfg, isCC := parseArgs(os.Args[1:])

	if len(cfg.inputs) == 0 {
		fmt.Fprintln(os.Stderr, "goc: no input files")
		os.Exit(1)
	}
	outPath, err := buildProgram(cfg, isCC)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if outPath == "" || cfg.mode != "run" {
		return
	}

	if cfg.linux {
		// The output cannot run here; elfcheck loads and interprets it so the
		// result is still verified rather than merely hoped for.
		if !isCC {
			fmt.Println("(ELF binary: run it on Linux)")
		}
		return
	}

	abs, err := filepath.Abs(outPath)
	if err != nil {
		abs = outPath
	}
	rcmd := exec.Command(abs)
	rcmd.Stdout = os.Stdout
	rcmd.Stderr = os.Stderr
	if err := rcmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			fmt.Printf("(program exited with code %d)\n", ee.ExitCode())
		} else {
			fmt.Fprintln(os.Stderr, "run failed:", err)
		}
	}
}

// buildProgram runs the preprocess -> parse -> check -> gen -> goa pipeline
// for cfg's single input and returns the executable path. The -E and -S
// modes write their own outputs and return "". Compile errors terminate the
// process (exit 1), matching the historical behaviour.
func buildProgram(cfg buildCfg, isCC bool) (string, error) {
	for _, p := range cfg.inputs {
		if strings.HasSuffix(p, ".o") || strings.HasSuffix(p, ".obj") ||
			strings.HasSuffix(p, ".a") || strings.HasSuffix(p, ".lib") {
			return "", fmt.Errorf("goc: %s -- goc cannot consume object/library files (no separate linking stage)", p)
		}
	}
	if cfg.rtdiag {
		// Must happen BEFORE injectDefines/Preprocess so the macro reaches the
		// user translation unit too -- that is what makes the <rt.h> diagnostic
		// API (goc_rt_stats_t, __goc_rt_print, ...) visible to user code.
		cfg.defines = append(cfg.defines, "GOC_RTDIAG=1")
		SetRtdiag(true)
	}
	// Several .c files: each is its own translation unit (its own macros and
	// type names), and the merged program is compiled as one executable.
	if len(cfg.inputs) > 1 {
		return buildMulti(cfg, isCC)
	}

	srcPath := cfg.inputs[0]
	src, err := os.ReadFile(srcPath)
	if err != nil {
		return "", err
	}
	src = []byte(injectDefines(string(src), cfg.defines))

	toks, err := PreprocessTarget(string(src), srcPath, cfg.linux, cfg.incDirs...)
	if err != nil {
		return "", fmt.Errorf("preprocess error: %w", err)
	}

	// -E: preprocess only, emit the (directives-stripped, macro-expanded)
	// translation unit and stop.
	if cfg.mode == "preprocess" {
		out := SerializeTokens(toks)
		if cfg.outFile != "" && !isDir(cfg.outFile) {
			if err := os.WriteFile(cfg.outFile, []byte(out), 0644); err != nil {
				return "", err
			}
		} else {
			os.Stdout.WriteString(out)
			if len(out) == 0 || out[len(out)-1] != '\n' {
				os.Stdout.WriteString("\n")
			}
		}
		return "", nil
	}

	prog, err := Parse(toks)
	if err != nil {
		return "", fmt.Errorf("parse error: %w", err)
	}
	return emitProgram(prog, cfg, isCC)
}

// emitProgram is everything that happens after parsing: type-check the whole
// program, generate assembly, and hand it to goa. Shared by the single-file
// path and the multi-file one (which parses each .c separately and merges the
// translation units before calling this).
func emitProgram(prog *Program, cfg buildCfg, isCC bool) (string, error) {
	if errs := Check(prog); len(errs) > 0 {
		var b strings.Builder
		b.WriteString("type error(s):")
		for _, e := range errs {
			fmt.Fprintf(&b, "\n  %s", e.Error())
		}
		return "", errors.New(b.String())
	}
	// The LLVM back end hands the whole program to one owner: every user
	// function and every C runtime function becomes LLVM IR, compiled by
	// libLLVM into a COFF object. goa's own assembler then contributes only the
	// entry stub -- which is not C -- and links that object into the image.
	// The names LLVM defined are reported as `claimed` so genWith leaves them
	// alone and emits nothing but the stub.
	var irText string
	var claimed map[string]bool
	if cfg.llvm {
		var err error
		if irText, claimed, err = genLLVMProgram(prog, cfg.linux, cfg.opt); err != nil {
			return "", err
		}
		if len(claimed) == 0 {
			return "", fmt.Errorf("-fllvm: no function in this program can be compiled " +
				"by the LLVM front end; every one of them uses a construct it does " +
				"not model yet (variadic functions, bit-fields, _BitInt or inline asm)")
		}
	}
	asm, err := genWith(prog, cfg.linux, cfg.opt, cfg.winGUI, claimed)
	if err != nil {
		return "", fmt.Errorf("codegen error: %w", err)
	}
	// outputPaths names the outputs after the first input; a multi-file build
	// produces one program, so that is the right base name.
	asmPath, outPath := outputPaths(cfg.inputs[0], cfg.outFile, cfg.linux, cfg.mode == "asm")
	if err := os.MkdirAll(filepath.Dir(asmPath), 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(asmPath, []byte(asm), 0644); err != nil {
		return "", err
	}

	if cfg.mode == "asm" {
		if !isCC {
			fmt.Printf("assembly written to %s\n", asmPath)
		}
		return "", nil
	}

	// Hand the assembly to goa, our own assembler -- linked into this binary,
	// so there is no external goa process and nothing to find on disk. With
	// -fllvm the object's code goes into the same image.
	var obj []byte
	if irText != "" {
		if cfg.dumpIR {
			// Name the dump after the executable, which is what the user asked
			// for: "goc -fllvm -dump-ir foo.c" leaves foo.ll (and foo.obj) beside
			// foo.exe. The IR is the artifact worth reading -- it is what LLVM
			// rejected, and what its optimiser would have to say about it -- but
			// the object is kept too, since its relocations and undefined symbols
			// are what a link error is really about.
			llPath := strings.TrimSuffix(outPath, filepath.Ext(outPath)) + ".ll"
			if err := os.WriteFile(llPath, []byte(irText), 0644); err != nil {
				return "", err
			}
			if !isCC {
				fmt.Printf("IR written to %s\n", llPath)
			}
		}
		if obj, err = compileIR(irText, cfg.opt, cfg.linux); err != nil {
			// The IR is what LLVM rejected, so when it is being dumped, leave it
			// on disk before failing: without it the error names a line in a file
			// that no longer exists anywhere.
			if cfg.dumpIR {
				llPath := strings.TrimSuffix(outPath, filepath.Ext(outPath)) + ".ll"
				if werr := os.WriteFile(llPath, []byte(irText), 0644); werr == nil {
					fmt.Fprintf(os.Stderr, "IR written to %s\n", llPath)
				}
			}
			return "", err
		}
		if cfg.dumpIR {
			objPath := strings.TrimSuffix(outPath, filepath.Ext(outPath)) + ".obj"
			if err := os.WriteFile(objPath, obj, 0644); err != nil {
				return "", err
			}
			if !isCC {
				fmt.Printf("object written to %s\n", objPath)
			}
		}
	}
	if err := assemble(asm, outPath, cfg.linux, cfg.inputs[0], isCC, obj); err != nil {
		return "", err
	}
	return outPath, nil
}

// assemble runs the in-process goa assembler over asm text and writes the
// executable to outPath. It reports the same one-line summary the goa CLI
// used to print (silently, when acting as cc, because gcc is silent).
func assemble(asm, outPath string, linux bool, srcPath string, isCC bool, obj []byte) error {
	n, err := goa.AssembleWithObject(asm, obj, outPath, linux)
	if err != nil {
		return fmt.Errorf("goa failed: %w", err)
	}
	if !isCC {
		if n > 0 {
			fmt.Printf("compiled %s -> %s (%d bytes)\n", srcPath, outPath, n)
		} else {
			fmt.Printf("compiled %s -> %s\n", srcPath, outPath)
		}
	}
	return nil
}

// splitRunArgs divides `goc run` arguments into build flags, the source
// file(s) and the program arguments: everything up to the first bare token is
// a build flag (known separate-value flags swallow their value so it is not
// mistaken for the source); the first bare token is a source file, and any
// immediately following bare token that looks like a C source joins it (so
// `goc run a.c b.c` builds both); everything after that goes to the compiled
// program verbatim -- the go run contract.
func splitRunArgs(args []string) (buildArgs []string, inputs []string, progArgs []string) {
	takesValue := map[string]bool{
		"-o": true, "-D": true, "-I": true, "-target": true,
		"-MF": true, "-MT": true, "-MQ": true, "-include": true,
	}
	i := 0
	for ; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") || a == "-" {
			break
		}
		if takesValue[a] {
			i++ // skip the flag's separate value
		}
		// Attached forms (-ofile, -O2, -Wall) carry no separate value.
	}
	if i >= len(args) {
		return args, nil, nil
	}
	inputs = append(inputs, args[i])
	j := i + 1
	// Extra translation units: `goc run a.c b.c -- args to the program`.
	for ; j < len(args); j++ {
		if strings.HasPrefix(args[j], "-") && args[j] != "-" {
			break
		}
		if !isCSource(args[j]) {
			break
		}
		inputs = append(inputs, args[j])
	}
	return args[:i], inputs, args[j:]
}

// isCSource reports whether a bare `goc run` argument is another C source
// file rather than an argument for the compiled program.
func isCSource(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".c", ".i":
		return true
	}
	return false
}

// runCmd implements `goc run [build-flags] file.c [program-args...]`:
// compile into a temporary directory, execute with the given arguments and
// inherited stdio, pass the program's exit code through, and clean up.
// The program's own argc/argv come from the OS (Windows rebuilds them from
// GetCommandLineA, Linux reads [rsp] at entry), so exec-ing with progArgs is
// all the forwarding needed.
func runCmd(args []string) {
	buildArgs, inputs, progArgs := splitRunArgs(args)
	if len(inputs) == 0 {
		fmt.Fprintln(os.Stderr, "goc run: no input files")
		fmt.Fprintln(os.Stderr, "usage: goc run [-O*] [-Dname[=val]] [-Idir] file.c [file2.c...] [args...]")
		os.Exit(1)
	}

	cfg, _ := parseArgs(buildArgs)
	cfg.inputs = inputs
	cfg.mode = "compile" // the run below is runCmd's job
	if cfg.linux {
		fmt.Fprintln(os.Stderr, "goc run: cannot execute a Linux ELF on this host (drop -target linux, or use -c and run it on Linux)")
		os.Exit(1)
	}

	tmp, err := os.MkdirTemp("", "goc-run-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "goc run:", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)
	cfg.outFile = tmp

	code := 0
	func() {
		// defer (not a bare os.Exit path) so the temp dir is removed on
		// every exit from here, compile errors included.
		defer os.RemoveAll(tmp)

		outPath, err := buildProgram(cfg, false)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			code = 1
			return
		}
		abs, err := filepath.Abs(outPath)
		if err != nil {
			abs = outPath
		}
		cmd := exec.Command(abs, progArgs...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				// Pass the program's exit status through, go-run style.
				if c := ee.ExitCode(); c >= 0 {
					code = c
					return
				}
			} else {
				fmt.Fprintln(os.Stderr, "goc run:", err)
			}
			code = 1
		}
	}()
	os.Exit(code)
}

// buildCfg holds the result of parsing the command line.
type buildCfg struct {
	mode   string // run | compile | asm | preprocess
	linux  bool
	winGUI bool // -mwindows: PE subsystem 2 (GUI), no console window
	opt    int  // optimisation level from -O<level> (0 = none)
	// llvm selects the LLVM back end: the user's own functions are compiled by
	// LLVM from IR, while the entry stub, the globals and the C runtime still
	// come from goa's own assembler. Requires a libLLVM shared library at run
	// time; without one the build fails with an explanation rather than
	// silently falling back, so a missing library is never mistaken for a
	// successful build.
	llvm    bool
	dumpIR  bool // -dump-ir: keep the LLVM IR (and object) beside the output
	rtdiag  bool // -rtdiag: compile-time diagnostic runtime (memory tracker)
	outFile string
	defines []string
	incDirs []string
	inputs  []string
}

// optFromSuffix maps the suffix of an -O flag to a numeric optimisation
// level (2026-10-02 real layering, T1.1):
//
//	1 = -O/-Og/-O1 : the full current pass set
//	2 = -Os/-Oz    : size-first; every cleanup pass runs, inlining (the only
//	                 pass that grows the program) does not
//	3 = -O2        : level 1 + constant-folding enhancements (T1.2 hook)
//	4 = -O3/-Ofast : level 3 + strength reduction / register caching (T1.3,
//	                 Tier 2 hooks)
//
// Levels 3 and 4 currently execute the same passes; the distinct numbers are
// the hook points future passes gate on. -1 means "not a level we model":
// the flag stays accepted-and-ignored, like every unimplemented gcc option.
func optFromSuffix(s string) int {
	switch s {
	case "", "g":
		return 1
	case "s", "z":
		return 2
	case "fast":
		return 4
	}
	if n, err := strconv.Atoi(s); err == nil && n >= 0 && n <= 4 {
		switch n {
		case 2:
			return 3
		case 3:
			return 4
		}
		return n
	}
	return -1
}

// parseArgs turns os.Args[1:] into a buildCfg, tolerating gcc/clang options.
func parseArgs(args []string) (buildCfg, bool) {
	cfg := buildCfg{mode: "compile"} // plain `goc file.c` never auto-runs
	self := filepath.Base(os.Args[0])
	isCC := self == "cc" || self == "cc.exe" || strings.HasPrefix(self, "cc.")

	for i := 0; i < len(args); i++ {
		arg := args[i]
		// takeVal returns def if the flag carried an attached value (=form),
		// otherwise consumes the next argument.
		takeVal := func(def string) string {
			if def != "" {
				return def
			}
			i++
			if i >= len(args) {
				fmt.Fprintf(os.Stderr, "goc: %s needs an argument\n", arg)
				os.Exit(1)
			}
			return args[i]
		}

		if strings.HasPrefix(arg, "--") {
			body := strings.TrimPrefix(arg, "--")
			switch {
			case body == "version":
				fmt.Println("goc (Go-of-C)", versionInfo())
				os.Exit(0)
			case body == "help":
				printHelp()
				os.Exit(0)
			default:
				// Unknown long option: accept and ignore.
			}
			continue
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			cfg.inputs = append(cfg.inputs, arg)
			continue
		}
		// Attached-value single-letter forms: -ofile -Dname -Ipath -lfoo -Ldir,
		// plus whole families we accept-and-ignore: -W -m -f -g -s. (-O is
		// parsed for real: see optFromSuffix.)
		// -mwindows is the one machine flag goc honours: it switches the PE
		// subsystem to 2 (windows GUI) so no console window is allocated.
		// Every other -m* stays accepted-and-ignored below.
		if arg == "-mwindows" {
			cfg.winGUI = true
			continue
		}
		// -fllvm selects the LLVM back end. It has to be recognised before the
		// single-letter pass below, which sees the "f" and treats the whole
		// thing as an ignored -f option -- so a case in the long-flag switch
		// further down is never reached and the flag silently does nothing.
		if arg == "-fllvm" || arg == "--llvm" {
			cfg.llvm = true
			continue
		}
		// -dump-ir writes the generated LLVM IR next to the output as <base>.ll
		// (and keeps the intermediate object as <base>.obj). It is the first
		// thing to reach for when the IR is rejected or the result misbehaves:
		// the whole point of the LLVM path is that the module it hands over can
		// be read, and running the C compiler's optimiser over it by hand is the
		// quickest way to see what the front end actually produced. The default
		// back end has no equivalent, so the flag is accepted only with -fllvm.
		if arg == "-dump-ir" || arg == "--dump-ir" {
			cfg.dumpIR = true
			continue
		}
		if len(arg) > 2 {
			switch arg[1] {
			case 'o':
				cfg.outFile = arg[2:]
				continue
			case 'D':
				cfg.defines = append(cfg.defines, arg[2:])
				continue
			case 'I':
				cfg.incDirs = append(cfg.incDirs, arg[2:])
				continue
			case 'O':
				// -O0/-O1/-O2/-O3/-Os/-Og/-Ofast: the level selects the
				// optimisation pipeline. Unknown suffixes stay
				// accepted-and-ignored, like every unimplemented gcc option.
				if lvl := optFromSuffix(arg[2:]); lvl >= 0 {
					cfg.opt = lvl
				}
				continue
			case 'l', 'L', 'm', 'W', 'f', 'g', 's':
				// link lib / lib dir / machine / optimise / warning / feature /
				// debug / strip: ignored.
				continue
			}
		}
		// Flag with an optional "=value" or a separate value.
		name, val := arg, ""
		if eq := strings.IndexByte(arg, '='); eq >= 0 {
			name, val = arg[:eq], arg[eq+1:]
		}
		switch name {
		case "-c":
			cfg.mode = "compile"
		case "-S":
			cfg.mode = "asm"
		case "-E":
			cfg.mode = "preprocess"
		case "-o":
			cfg.outFile = takeVal(val)
		case "-D":
			cfg.defines = append(cfg.defines, takeVal(val))
		case "-I":
			cfg.incDirs = append(cfg.incDirs, takeVal(val))
		case "-target":
			v := takeVal(val)
			cfg.linux = v == "linux" || v == "elf"
		case "-rt", "-rtdiag":
			// Compile-time optional diagnostic runtime: instruments malloc/free
			// with a heap-object tracker, redzone OOB detection and (on Windows)
			// a crash handler that dumps live allocations. No separate value.
			cfg.rtdiag = true
		// The following are accepted and ignored: they only make sense for a
		// real multi-stage toolchain (separate linking, full warnings,
		// alternate standards, ...). goc is a single-pass compiler.
		case "-O":
			// Bare -O means -O1 (gcc); an "=level" value is honoured too.
			if lvl := optFromSuffix(val); lvl >= 0 {
				cfg.opt = lvl
			}
		case "-Wall", "-Wextra", "-Werror", "-Wshadow", "-w",
			"-std", "-m", "-g", "-static", "-shared", "-pthread",
			"-pipe", "-v", "-pedantic", "-ansi", "-M", "-MM", "-MD", "-MP",
			"-MF", "-MT", "-MQ",
			"-save-temps", "-ffreestanding", "-fno-builtin", "-fPIC", "-fpic",
			"-fno-stack-protector", "-nostdlib", "-nodefaultlibs", "-nolibc":
			// swallow a possible separate value for -MF/-MT/-MQ style flags
			if name == "-MF" || name == "-MT" || name == "-MQ" {
				if val == "" {
					i++ // consume the dependency-file argument
				}
			}
		default:
			// Any other -flag: accept and ignore (covers -Wl,--foo, -fsanitize,
			// and anything future gcc adds) so foreign build commands don't break.
		}
	}
	return cfg, isCC
}

// injectDefines prepends `-D` macro definitions as real #define lines so the
// preprocessor sees them before any user code. A bare `-DNAME` expands to 1,
// matching gcc; `-DNAME=val` keeps the supplied value.
func injectDefines(src string, defines []string) string {
	if len(defines) == 0 {
		return src
	}
	var b strings.Builder
	for _, d := range defines {
		if eq := strings.IndexByte(d, '='); eq >= 0 {
			b.WriteString("#define ")
			b.WriteString(d[:eq])
			b.WriteByte(' ')
			b.WriteString(d[eq+1:])
			b.WriteByte('\n')
		} else {
			b.WriteString("#define ")
			b.WriteString(d)
			b.WriteString(" 1\n")
		}
	}
	b.WriteString(src)
	return b.String()
}

// outputPaths decides where the assembly and the final executable go.
//
//	outFile semantics mirror gcc: if it is an existing directory, outputs land
//	inside it named after the source (goc's historic behaviour); otherwise it
//	is the gcc-style output file name. On Windows the executable gets a .exe
//	suffix unless one is already present.
//
//	When asmMode is true and outFile is an explicit (non-directory) file, that
//	file is the assembly output directly (gcc: "cc -S -o file.s"), and the
//	executable path is unused.
func outputPaths(srcPath, outFile string, linux, asmMode bool) (asmPath, outPath string) {
	if outFile == "" {
		base := strings.TrimSuffix(srcPath, filepath.Ext(srcPath))
		if linux {
			return base + ".asm", base
		}
		return base + ".asm", base + ".exe"
	}
	// A trailing separator, or a path that already exists as a directory,
	// means "write the outputs inside this directory" (goc's historic -o dir
	// behaviour). Anything else is the gcc-style output file name.
	if strings.HasSuffix(outFile, string(os.PathSeparator)) || isDir(outFile) {
		base := filepath.Join(outFile, filepath.Base(strings.TrimSuffix(srcPath, filepath.Ext(srcPath))))
		if linux {
			return base + ".asm", base
		}
		return base + ".asm", base + ".exe"
	}
	// gcc-style output file.
	if asmMode {
		return outFile, "" // -S -o file.s: the file is the assembly itself
	}
	if linux {
		return strings.TrimSuffix(outFile, filepath.Ext(outFile)) + ".asm", outFile
	}
	exe := outFile
	if !strings.HasSuffix(exe, ".exe") {
		exe += ".exe"
	}
	return strings.TrimSuffix(exe, ".exe") + ".asm", exe
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func printHelp() {
	fmt.Print(`goc - a tiny C compiler (gcc/clang-compatible front-end)

Usage: goc [options] file.c [file2.c ...]
       goc run [build-flags] file.c [file2.c ...] [program-args...]

Several .c files are compiled as separate translation units (each with its own
macros and type names) and merged into one executable; static keeps a symbol
private to its file.

Options:
  run             compile to a temp dir, run with the given arguments;
                  the program's exit code is passed through
  -c              compile to an executable (no auto-run)
  -S              emit assembly only
  -E              preprocess only, write the translation unit to stdout/-o
  -o <file>       output file (gcc semantics) or directory (legacy)
  -D<name>[=val]  predefine a macro (val defaults to 1)
  -I<dir>         add a header search directory
  -target linux   emit a Linux ELF64 instead of a Windows PE32+
  -mwindows       PE subsystem 2 (windows GUI): no console window is
                  allocated; hInstance comes from GetModuleHandleA(NULL)
  -rt, -rtdiag    enable the diagnostic runtime (memory tracker, redzone OOB
                  detection, leak report; <rt.h> exposes runtime_stats() /
                  runtime_print() / runtime_dump() / runtime_scan())
  -O* -Wall -W* -std -m* -g -static -pthread -f* -s -l -L -Wl,*
                  accepted and ignored (goc is a single-pass compiler)
  --version       show version
  --help          show this help

Note: goc compiles its inputs into one complete executable; it has no separate
linking stage, so it cannot consume .o files or link several objects.
`)
}

// The assembler used to be an external binary that goc located via $GOA /
// next to its own executable / PATH. Since 2026-10-04 it is the goa package
// linked into this binary (see assemble above), so there is nothing to find.
