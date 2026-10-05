// Command goc is the C compiler.
//
// It is a thin shell around package compiler, which holds the pipeline: the
// front end is src/frontend (module goc/frontend), the code generator and the
// assembler driver are in the package this calls. The split is what lets a
// second compiler -- one that emits LLVM IR instead of assembly, or one that
// embeds the C library to ship a self-contained binary -- reuse the pipeline
// without duplicating it.
//
//	goc file.c                 compile only (emit file.exe)
//	goc a.c b.c                compile several translation units into one exe
//	goc run file.c [args...]   compile to a temp dir, run with args
//	goc -c file.c              compile only
//	goc -S file.c              emit assembly only (file.asm)
//	goc -o app file.c          write the executable to app(..exe)
//	goc -target linux file.c   produce a Linux ELF64
//
// gcc/clang-compatible options (-O*, -Wall, -std=*, -D*, -I*, -l*, ...) are
// accepted; see package compiler for the full subset and for the personality
// that turns on when the binary is named cc.
package main

import (
	"os"

	compiler "goc"
)

func main() {
	os.Exit(compiler.Main(os.Args[1:]))
}
