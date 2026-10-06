package compiler

// Tests for the multi-translation-unit build (src/multi.go).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeUnit drops a .c file into dir and returns its path.
func writeUnit(t *testing.T, dir, name, src string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

// buildUnits runs the whole pipeline over several .c files and returns the
// generated assembly next to the executable path.
func buildUnits(t *testing.T, files ...string) (asm string) {
	t.Helper()
	cfg := buildCfg{mode: "asm", inputs: files, outFile: filepath.Join(t.TempDir(), "out.s")}
	if _, err := buildProgram(cfg, true); err != nil {
		t.Fatalf("buildProgram: %v", err)
	}
	b, err := os.ReadFile(cfg.outFile)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Two files, each with its own static helper of the same name: both must
// survive under file-unique names and each call must reach its own file's
// copy. A shared external symbol stays shared.
func TestMultiUnitStaticIsolation(t *testing.T) {
	dir := t.TempDir()
	a := writeUnit(t, dir, "a.c", `
static int buf[4] = {1, 2, 3, 4};
static int twice(int x){ return x * 2; }
int shared_counter = 5;
int a_helper(int x){ return twice(x) + buf[0]; }
`)
	b := writeUnit(t, dir, "b.c", `
static int buf[4] = {9, 9, 9, 9};
static int twice(int x){ return x * 100; }
int a_helper(int x);
extern int shared_counter;
int main(void){ return a_helper(1) + twice(1) + buf[0] + shared_counter; }
`)
	asm := buildUnits(t, a, b)
	// Both statics are emitted, renamed apart, and each call site was
	// rewritten (a call names its callee as a string, not an frontend.Ident -- a
	// rename walk that misses that shape leaves a call to the wrong file's
	// function, or to a symbol that does not exist).
	for _, want := range []string{"twice__tu0:", "twice__tu1:", "call twice__tu0", "call twice__tu1"} {
		if !strings.Contains(asm, want) {
			t.Errorf("assembly is missing %q", want)
		}
	}
}

// A name defined twice with external linkage is an error, not a silent
// "last one wins". Which stage catches it depends on what was asked for:
//
//   - Compiling the sources as one program (the default) sees every definition,
//     so the front end reports it.
//   - Compiling them to objects (-c) sees one unit at a time and cannot: the
//     two definitions are in different files, and no single unit knows about
//     the other. The linker is the stage that sees both, and reports it there.
func TestMultiUnitDuplicateExternal(t *testing.T) {
	dir := t.TempDir()
	a := writeUnit(t, dir, "a.c", "int dup(int x){ return x; }\n")
	b := writeUnit(t, dir, "b.c", "int dup(int x){ return x + 1; }\nint main(void){ return 0; }\n")

	t.Run("whole program", func(t *testing.T) {
		cfg := buildCfg{mode: "asm", inputs: []string{a, b}, outFile: filepath.Join(dir, "app.s")}
		if _, err := buildProgram(cfg, true); err == nil {
			t.Fatal("expected a duplicate-definition error")
		} else if !strings.Contains(err.Error(), "duplicate definition of function \"dup\"") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("separate objects", func(t *testing.T) {
		// -c must not report it: neither unit can see the other. Each object is
		// written beside its source, named after it.
		var objs []string
		for _, src := range []string{a, b} {
			obj := filepath.Join(dir, filepath.Base(src)+".o")
			cfg := buildCfg{mode: "object", inputs: []string{src}, outFile: obj}
			if _, err := buildProgram(cfg, true); err != nil {
				t.Fatalf("compiling %s: %v", src, err)
			}
			objs = append(objs, obj)
		}
		cfg := buildCfg{mode: "run", inputs: objs, outFile: filepath.Join(dir, "app.exe")}
		out, err := linkObjects(cfg, true)
		if err == nil {
			os.Remove(out)
			t.Fatal("expected a duplicate-definition error from the link")
		}
		if !strings.Contains(err.Error(), "duplicate definition of symbol \"dup\"") {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

// Each file is its own translation unit: the same typedef name may mean
// different things in two files, and an enumerator declared in one file is
// still visible when the merged program is generated.
func TestMultiUnitSeparateTypeNamespaces(t *testing.T) {
	dir := t.TempDir()
	a := writeUnit(t, dir, "a.c", `
typedef struct { int x; } Pair;
int a_sum(Pair *p){ return p->x; }
`)
	b := writeUnit(t, dir, "b.c", `
typedef struct { int y; long z; } Pair;   /* a different Pair */
enum { K = 7 };
int a_sum(void *p);
int main(void){ Pair p; p.y = 1; p.z = 2; return a_sum(0) + p.y + K; }
`)
	asm := buildUnits(t, a, b)
	if !strings.Contains(asm, "a_sum:") {
		t.Error("a_sum was not emitted")
	}
	// K is an enumerator from the second unit; it must still resolve.
	if !strings.Contains(asm, "7") {
		t.Error("enumerator from the second unit did not reach codegen")
	}
}

// A header included by both files declares globals, typedefs and enumerators;
// the merged program must see exactly one of each (no redefinition error).
func TestMultiUnitSharedHeader(t *testing.T) {
	dir := t.TempDir()
	h := writeUnit(t, dir, "common.h", `
#ifndef COMMON_H
#define COMMON_H
typedef unsigned long size_t;
typedef struct { int a, b; } Pair;
enum Color { RED = 1, GREEN = 2 };
extern int shared_counter;
int shared_helper(int x);
#endif
`)
	_ = h
	a := writeUnit(t, dir, "a.c", `#include "common.h"
int shared_counter = 5;
int shared_helper(int x){ return x + RED; }
`)
	b := writeUnit(t, dir, "b.c", `#include "common.h"
int main(void){ Pair p; p.a = shared_helper(shared_counter); return p.a + GREEN; }
`)
	out := filepath.Join(dir, "app")
	cfg := buildCfg{mode: "link", inputs: []string{a, b, "-I" + dir}, outFile: out}
	cfg.incDirs = []string{dir}
	cfg.inputs = []string{a, b}
	if _, err := buildProgram(cfg, true); err != nil {
		t.Fatalf("buildProgram: %v", err)
	}
}
