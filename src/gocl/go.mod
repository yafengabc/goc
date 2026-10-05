// gocl: the C compiler with an LLVM back end.
//
// Same front end as src/goc (goc/frontend), same C library (goc/common), same
// assembler (goa) -- but the code generator is libLLVM instead of goc's own
// x86-64 emitter. The two compilers share a preprocessed, checked program and
// differ only in what they do with it, which is the arrangement this split
// exists to make possible: an optimisation improvement belongs in the shared
// front end and both compilers get it.
//
// Pipeline: lex -> parse -> check -> LLVM IR -> optimise -> COFF/ELF object ->
// goa links it into a PE or ELF. No gcc, no system libc.
//
// The division of labour is not arbitrary. goc owns the x86-64 encoding, the
// register allocation and its own optimisation passes; gocl owns the IR
// translation and leans on LLVM's optimiser, which is where its advantage
// comes from. Neither reaches into the other's files, and the only thing they
// share is a Program.
module gocl

go 1.21

require (
	goc/common v0.0.0
	goc/frontend v0.0.0
	goa v0.0.0
)

replace goc/common => ../common

replace goc/frontend => ../frontend

replace goa => ../goa
