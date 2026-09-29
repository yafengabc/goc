package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestParseArgsGccCompat checks that goc tolerates the gcc/clang flag soup it
// is likely to meet in real build scripts: optimisation levels, warning flags,
// standard selection, machine/linker/feature flags are all accepted and
// ignored, while the handful of meaningful options are captured.
func TestParseArgsGccCompat(t *testing.T) {
	// Force the goc (not cc) personality for a deterministic default mode.
	old := os.Args
	defer func() { os.Args = old }()
	os.Args = []string{"goc"}

	cfg, isCC := parseArgs([]string{
		"-c", "-O2", "-Wall", "-Werror", "-Wshadow",
		"-std=c11", "-m64", "-g", "-static", "-pthread",
		"-fno-stack-protector", "-s", "-pipe", "-v",
		"-DFOO=7", "-DBAR", "-Iinc", "-Llib", "-lm",
		"-Wl,--as-needed", "-o", "out", "-target", "linux",
		"src/examples/hello.c",
	})
	if isCC {
		t.Fatal("invoked as goc but detected as cc")
	}
	if cfg.mode != "compile" {
		t.Errorf("mode = %q, want compile", cfg.mode)
	}
	if !cfg.linux {
		t.Error("target linux not captured")
	}
	if cfg.outFile != "out" {
		t.Errorf("outFile = %q, want out", cfg.outFile)
	}
	if len(cfg.defines) != 2 || cfg.defines[0] != "FOO=7" || cfg.defines[1] != "BAR" {
		t.Errorf("defines = %v, want [FOO=7 BAR]", cfg.defines)
	}
	if len(cfg.incDirs) != 1 || cfg.incDirs[0] != "inc" {
		t.Errorf("incDirs = %v, want [inc]", cfg.incDirs)
	}
	if len(cfg.inputs) != 1 || cfg.inputs[0] != "src/examples/hello.c" {
		t.Errorf("inputs = %v, want [src/examples/hello.c]", cfg.inputs)
	}
}

// TestParseArgsAttached forms verify the gcc-style glued flags (-ofile, -Dx,
// -Ipath, -lfoo).
func TestParseArgsAttached(t *testing.T) {
	old := os.Args
	defer func() { os.Args = old }()
	os.Args = []string{"goc"}

	cfg, _ := parseArgs([]string{"-c", "-oapp.exe", "-DVER=3", "-Iheaders", "-lsqlite3", "a.c"})
	if cfg.outFile != "app.exe" {
		t.Errorf("outFile = %q, want app.exe", cfg.outFile)
	}
	if len(cfg.defines) != 1 || cfg.defines[0] != "VER=3" {
		t.Errorf("defines = %v, want [VER=3]", cfg.defines)
	}
	if len(cfg.incDirs) != 1 || cfg.incDirs[0] != "headers" {
		t.Errorf("incDirs = %v, want [headers]", cfg.incDirs)
	}
	if len(cfg.inputs) != 1 || cfg.inputs[0] != "a.c" {
		t.Errorf("inputs = %v, want [a.c]", cfg.inputs)
	}
}

// TestParseArgsOptLevels locks how -O variants map to the numeric level that
// selects the optimisation pipeline.
func TestParseArgsOptLevels(t *testing.T) {
	old := os.Args
	defer func() { os.Args = old }()
	os.Args = []string{"goc"}

	cases := []struct {
		flag string
		want int
	}{
		{"-O0", 0}, {"-O1", 1}, {"-O2", 3}, {"-O3", 3},
		{"-O", 1}, {"-Og", 1}, {"-Os", 2}, {"-Oz", 2}, {"-Ofast", 3},
	}
	for _, tc := range cases {
		cfg, _ := parseArgs([]string{"-c", tc.flag, "a.c"})
		if cfg.opt != tc.want {
			t.Errorf("%s: opt = %d, want %d", tc.flag, cfg.opt, tc.want)
		}
	}
	// Unknown suffixes stay accepted-and-ignored (level untouched, i.e. 0).
	cfg, _ := parseArgs([]string{"-c", "-Owebsite", "a.c"})
	if cfg.opt != 0 {
		t.Errorf("-Owebsite: opt = %d, want 0 (ignored)", cfg.opt)
	}
}

// TestInjectDefines confirms -DNAME defaults to 1 and -DNAME=val keeps the val.
func TestInjectDefines(t *testing.T) {
	got := injectDefines("int x;\n", nil)
	if got != "int x;\n" {
		t.Errorf("injectDefines with no defines changed source: %q", got)
	}
	got = injectDefines("int x;\n", []string{"FLAG", "N=42"})
	want := "#define FLAG 1\n#define N 42\nint x;\n"
	if got != want {
		t.Errorf("injectDefines =\n%q\nwant\n%q", got, want)
	}
}

// TestOutputPaths covers the -o directory / -o file / -S -o file distinctions.
func TestOutputPaths(t *testing.T) {
	// -o empty: beside the source.
	asm, out := outputPaths("src/examples/hello.c", "", false, false)
	if asm != "src/examples/hello.asm" || out != "src/examples/hello.exe" {
		t.Errorf("empty: asm=%q out=%q", asm, out)
	}
	asm, out = outputPaths("src/examples/hello.c", "", true, false)
	if asm != "src/examples/hello.asm" || out != "src/examples/hello" {
		t.Errorf("empty linux: asm=%q out=%q", asm, out)
	}
	// -o a/b directory (does not exist here, but the rule keys off the suffix).
	asm, out = outputPaths("src/examples/hello.c", "a/b/"+string(os.PathSeparator), false, false)
	if filepath.Base(asm) != "hello.asm" || filepath.Base(out) != "hello.exe" {
		t.Errorf("dir: asm=%q out=%q", asm, out)
	}
	// -o app (gcc file semantics): foo.exe on Windows.
	asm, out = outputPaths("src/examples/hello.c", "app", false, false)
	if asm != "app.asm" || out != "app.exe" {
		t.Errorf("file: asm=%q out=%q", asm, out)
	}
	// -S -o app.s: the file is the assembly itself.
	asm, out = outputPaths("src/examples/hello.c", "app.s", false, true)
	if asm != "app.s" || out != "" {
		t.Errorf("asm file: asm=%q out=%q", asm, out)
	}
}
