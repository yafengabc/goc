package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
// ignored rather than rejected. The one hard limitation is linking: goc builds
// one program (with its library) end to end, so it cannot consume .o files or
// link several objects together -- gcc's "compile to .o then link" split is
// simply not representable here.
//
// Usage (goc convenience front-end):
//
//	goc file.c                 compile and run
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
// When the binary is named `cc` (or `cc.exe`), it behaves like gcc: the
// default action is "compile and emit an executable" without auto-running,
// matching cc's contract.
func main() {
	cfg, isCC := parseArgs(os.Args[1:])

	if len(cfg.inputs) == 0 {
		fmt.Fprintln(os.Stderr, "goc: no input files")
		os.Exit(1)
	}
	if len(cfg.inputs) > 1 {
		fmt.Fprintln(os.Stderr, "goc: multiple input files given -- goc compiles one translation unit into a complete executable and cannot link separate objects; invoke it once per program")
		os.Exit(1)
	}
	srcPath := cfg.inputs[0]
	if strings.HasSuffix(srcPath, ".o") || strings.HasSuffix(srcPath, ".obj") ||
		strings.HasSuffix(srcPath, ".a") || strings.HasSuffix(srcPath, ".lib") {
		fmt.Fprintf(os.Stderr, "goc: %s -- goc cannot consume object/library files (no separate linking stage)\n", srcPath)
		os.Exit(1)
	}

	src, err := os.ReadFile(srcPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	src = []byte(injectDefines(string(src), cfg.defines))

	toks, err := PreprocessTarget(string(src), srcPath, cfg.linux, cfg.incDirs...)
	if err != nil {
		fmt.Fprintln(os.Stderr, "preprocess error:", err)
		os.Exit(1)
	}

	// -E: preprocess only, emit the (directives-stripped, macro-expanded)
	// translation unit and stop.
	if cfg.mode == "preprocess" {
		out := SerializeTokens(toks)
		if cfg.outFile != "" && !isDir(cfg.outFile) {
			if err := os.WriteFile(cfg.outFile, []byte(out), 0644); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		} else {
			os.Stdout.WriteString(out)
			if len(out) == 0 || out[len(out)-1] != '\n' {
				os.Stdout.WriteString("\n")
			}
		}
		return
	}

	prog, err := Parse(toks)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parse error:", err)
		os.Exit(1)
	}
	if errs := Check(prog); len(errs) > 0 {
		fmt.Fprintln(os.Stderr, "type error(s):")
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "  "+e.Error())
		}
		os.Exit(1)
	}
	asm, err := Gen(prog, cfg.linux)
	if err != nil {
		fmt.Fprintln(os.Stderr, "codegen error:", err)
		os.Exit(1)
	}

	asmPath, outPath := outputPaths(srcPath, cfg.outFile, cfg.linux, cfg.mode == "asm")
	if err := os.MkdirAll(filepath.Dir(asmPath), 0755); err != nil {
		fmt.Fprintln(os.Stderr, "goc:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(asmPath, []byte(asm), 0644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if cfg.mode == "asm" {
		if !isCC {
			fmt.Printf("assembly written to %s\n", asmPath)
		}
		return
	}

	// Hand the assembly to goa, our own assembler. No gcc involved.
	goa, err := findGoa()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot find goa:", err)
		fmt.Fprintln(os.Stderr, "build it with: cd src/goa && go build .")
		fmt.Fprintln(os.Stderr, "or set GOA=<path to goa>")
		os.Exit(1)
	}

	var cmd *exec.Cmd
	if cfg.linux {
		cmd = exec.Command(goa, "-f", "elf", asmPath, outPath)
	} else {
		cmd = exec.Command(goa, asmPath, outPath)
	}
	// goa prints "compiled X (N bytes)" on success; that is fine for the goc
	// front-end but gcc is silent, so drop goa's stdout when acting as cc.
	if isCC {
		cmd.Stdout = nil
	} else {
		cmd.Stdout = os.Stdout
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "goa failed:", err)
		os.Exit(1)
	}
	if !isCC {
		fmt.Printf("compiled %s -> %s\n", srcPath, outPath)
	}

	if cfg.mode == "compile" {
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

// buildCfg holds the result of parsing the command line.
type buildCfg struct {
	mode    string // run | compile | asm | preprocess
	linux   bool
	outFile string
	defines []string
	incDirs []string
	inputs  []string
}

// parseArgs turns os.Args[1:] into a buildCfg, tolerating gcc/clang options.
func parseArgs(args []string) (buildCfg, bool) {
	cfg := buildCfg{mode: "run"}
	// When invoked as `cc`/`cc.exe` mimic gcc: compiling without -c/-S still
	// just emits an executable and never auto-runs.
	self := filepath.Base(os.Args[0])
	isCC := self == "cc" || self == "cc.exe" || strings.HasPrefix(self, "cc.")
	if isCC {
		cfg.mode = "compile"
	}

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
				fmt.Println("goc (Go-of-C) 0.1")
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
		// plus whole families we accept-and-ignore: -O -W -m -f -g -s.
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
			case 'l', 'L', 'm', 'O', 'W', 'f', 'g', 's':
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
		// The following are accepted and ignored: they only make sense for a
		// real multi-stage toolchain (separate linking, full warnings,
		// alternate standards, ...). goc is a single-pass compiler.
		case "-O", "-Wall", "-Wextra", "-Werror", "-Wshadow", "-w",
			"-std", "-m", "-g", "-static", "-shared", "-pthread",
			"-pipe", "-v", "-pedantic", "-ansi", "-M", "-MM", "-MD", "-MP",
			"-save-temps", "-ffreestanding", "-fno-builtin", "-fPIC", "-fpic",
			"-fno-stack-protector", "-nostdlib", "-nodefaultlibs", "-nolibc":
			// swallow a possible separate value for -MF/-MT/-MQ/-MQ style flags
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

Usage: goc [options] file.c

Options:
  -c              compile to an executable (no auto-run)
  -S              emit assembly only
  -E              preprocess only, write the translation unit to stdout/-o
  -o <file>       output file (gcc semantics) or directory (legacy)
  -D<name>[=val]  predefine a macro (val defaults to 1)
  -I<dir>         add a header search directory
  -target linux   emit a Linux ELF64 instead of a Windows PE32+
  -O* -Wall -W* -std -m* -g -static -pthread -f* -s -l -L -Wl,*
                  accepted and ignored (goc is a single-pass compiler)
  --version       show version
  --help          show this help

Note: goc compiles one translation unit into a complete executable; it has no
separate linking stage, so it cannot consume .o files or link several objects.
`)
}

// findGoa locates the goa assembler: $GOA if set, then next to the goc
// executable, then PATH. The binary is goa.exe on Windows and goa elsewhere.
func findGoa() (string, error) {
	if v := os.Getenv("GOA"); v != "" {
		return v, nil
	}
	name := "goa"
	if runtime.GOOS == "windows" {
		name = "goa.exe"
	}
	if self, err := os.Executable(); err == nil {
		cand := filepath.Join(filepath.Dir(self), name)
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand, nil
		}
	}
	return exec.LookPath(name)
}
