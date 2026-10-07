//go:build goc

// Command goc is the whole C toolchain in one executable: the front end, the
// code generator, the assembler, and the C library it compiles against.
//
// The C library is embedded, so the binary needs nothing beside it: carry the
// exe and it compiles C. An earlier build read goclib/ from disk instead, which
// is the right trade during development -- editing the library does not mean
// rebuilding the compiler -- and the wrong one for anything shipped. That build
// is gone; this entry point is the only way to build goc.
//
// Build it with `-tags goc`. The sibling file gocl.go, built with `-tags gocl`,
// is the same compiler with LLVM as the code generator.
//
// The trade the removed build made is worth stating, because it is the one
// thing this loses: editing src/goclib/*.c now requires rebuilding the compiler
// before a program picks the change up.

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
	"os"

	compiler "goc"
)

func main() {
	compiler.SetLibrary(embeddedLib{})
	os.Exit(compiler.Main(os.Args[1:]))
}
