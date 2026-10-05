// gocld: turning object files into executables.
//
// The second half of the LLVM half of the toolchain. gocl lowers C to LLVM IR
// and libLLVM emits one object; gocld takes that object and produces the PE or
// ELF image, which means laying out sections, resolving symbols, applying
// relocations, building the import table and writing the headers.
//
// It is a separate module from goa because the two answer different questions.
// goa is an assembler: it knows what an x86 instruction looks like and how to
// read assembly text. gocld is a linker: it knows what a PE section header
// looks like and how to merge an object into an image. Neither needs the
// other's knowledge, and the split is what lets them be paired the way the
// toolchain is:
//
//	goc  + goa   C -> assembly text -> machine code -> PE
//	gocl + gocld C -> LLVM IR -> object       -> PE
//
// Both paths end in the same image format, because a program that behaves one
// way under one compiler has to behave the same way under the other.
//
// What gocld does NOT do is parse assembly text. A caller that has text (goc,
// via goa) assembles it first and hands over the result; a caller that has
// objects (gocl, via libLLVM) skips that step entirely. That is the whole
// interface.
module gocld

go 1.21
