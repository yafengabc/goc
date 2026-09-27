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
// Usage:
//
//	goc file.c                 compile and run
//	goc -c file.c              compile only (produce file.exe / file)
//	goc -S file.c              emit assembly only (produce file.asm)
//	goc -o dir -c file.c       write all outputs into dir/ instead of beside file.c
//	goc -target linux file.c   produce a Linux ELF64 instead
func main() {
	args := os.Args[1:]
	mode := "run" // run | compile | asm
	linux := false
	outDir := ""
	var files []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-c":
			mode = "compile"
		case a == "-S":
			mode = "asm"
		case a == "-o" || a == "-outdir":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "goc: -o needs a directory argument")
				os.Exit(1)
			}
			i++
			outDir = args[i]
		case a == "-target" || a == "-f" || a == "--format":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "goc: -target needs an argument (windows or linux)")
				os.Exit(1)
			}
			i++
			linux = args[i] == "linux" || args[i] == "elf"
		case strings.HasPrefix(a, "-target="):
			v := strings.TrimPrefix(a, "-target=")
			linux = v == "linux" || v == "elf"
		default:
			files = append(files, a)
		}
	}
	if len(files) != 1 {
		fmt.Fprintln(os.Stderr, "usage: goc [-c|-S] [-target linux] [-o <dir>] <file.c>")
		os.Exit(1)
	}
	srcPath := files[0]
	src, err := os.ReadFile(srcPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	toks, err := Preprocess(string(src), srcPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "preprocess error:", err)
		os.Exit(1)
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
	asm, err := Gen(prog, linux)
	if err != nil {
		fmt.Fprintln(os.Stderr, "codegen error:", err)
		os.Exit(1)
	}

	base := strings.TrimSuffix(srcPath, filepath.Ext(srcPath))
	if outDir != "" {
		// -o <dir>: all outputs (.asm/.exe/ELF) go into dir/, named after the
		// source file, instead of sitting next to the source.
		if err := os.MkdirAll(outDir, 0755); err != nil {
			fmt.Fprintln(os.Stderr, "goc:", err)
			os.Exit(1)
		}
		base = filepath.Join(outDir, filepath.Base(base))
	}
	sfile := base + ".asm"
	if err := os.WriteFile(sfile, []byte(asm), 0644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if mode == "asm" {
		fmt.Printf("assembly written to %s\n", sfile)
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

	out := base + ".exe"
	if linux {
		out = base // Linux executables carry no suffix
	}
	var cmd *exec.Cmd
	if linux {
		cmd = exec.Command(goa, "-f", "elf", sfile, out)
	} else {
		cmd = exec.Command(goa, sfile, out)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "goa failed:", err)
		os.Exit(1)
	}
	fmt.Printf("compiled %s -> %s\n", srcPath, out)

	if mode == "compile" {
		return
	}

	if linux {
		// The output cannot run here; elfcheck loads and interprets it so the
		// result is still verified rather than merely hoped for.
		fmt.Println("(ELF binary: run it on Linux)")
		return
	}

	abs, err := filepath.Abs(out)
	if err != nil {
		abs = out
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
