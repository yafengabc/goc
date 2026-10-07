// Command goc is the whole C toolchain in one executable: the front end, the
// code generator, the assembler, and the C library it compiles against.
//
// Why this binary exists when src/goc/cmd/goc builds a working compiler: this
// one is self-contained. That compiler reads goclib/ from disk, so it is
// usable only next to a goclib/ directory -- which is the right trade for
// development, where editing the library and rebuilding the compiler would
// otherwise be two steps. This entry point embeds the library instead, so the
// resulting binary needs nothing beside it: carry the exe and it compiles C.
//
// The two are the same compiler. This one calls package compiler directly --
// no exec, no path search, no second copy of the pipeline -- and differs only
// in where the C library comes from. A fix to the front end or the code
// generator lands in both binaries at once, because there is only one of each.
//
// Usage:
//
//	goc file.c ...                compile C (same flags as src/goc)
//	goc run file.c [args...]      compile to a temp dir, then run
//	goc -S file.c                 emit assembly only
//	goc -c -o out.exe file.c      write the executable elsewhere
//	goc -target linux file.c      produce a static Linux ELF64
//
// gcc/clang-compatible flags (-O*, -Wall, -std=*, -D*, -I*, -l*, ...) are
// accepted; see package compiler for the full subset.
package main

import (
	"embed"
	"fmt"
	"os"
	"sort"

	compiler "goc"
)

// The C library. goc links no system libc on either target, so this is the
// whole runtime: sixteen .c files and thirty-eight .h files covering stdio,
// stdlib, string, ctype, math, and the Win32 declarations a GUI program needs.
//
// The pattern is relative to this file's directory and cannot escape it, which
// is the entire reason this entry point lives at src/ rather than in
// src/goc/cmd/: go:embed needs goclib/ to be a sibling of the .go file that
// embeds it.
//
//go:embed goclib/*
var libFS embed.FS

func main() {
	compiler.SetLibrary(embeddedLib{})
	os.Exit(compiler.Main(os.Args[1:]))
}

// embeddedLib adapts the embedded library to compiler's libSource interface.
//
// The adaptation is one method. compiler asks for a file by name and for the
// list of library files; embed.FS answers the first directly, and the second
// through a directory listing that this filters and sorts. The sort matters
// for the same reason it does on disk: the library's sources are compiled in
// list order, so that order decides when its definitions reach the symbol
// table, and an unstable one would make the output depend on the toolchain's
// mood.
type embeddedLib struct{}

func (embeddedLib) ReadFile(name string) ([]byte, error) {
	return libFS.ReadFile(name)
}

func (embeddedLib) ReadDir(name string) ([]string, error) {
	ents, err := libFS.ReadDir(name)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if len(n) > 2 && (n[len(n)-2:] == ".c" || n[len(n)-2:] == ".h") {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil, fmt.Errorf("no library sources under %s in the embedded FS", name)
	}
	return out, nil
}
