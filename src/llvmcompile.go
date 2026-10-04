package main

// Driving the LLVM back end from a build.
//
// One owner, not two. The whole program -- the user's functions and the C
// runtime alike -- is lowered to LLVM IR and compiled by libLLVM into a single
// COFF object. The names that object defines are reported back so the assembly
// half knows to stay away from them.
//
// What goa's own assembler still contributes is the entry stub, which is not C:
// it sets up the process stack per the platform ABI before main runs, and it
// links the LLVM object into the final image.

import (
	"fmt"
	"os"
	"path/filepath"

	"goa"
)

// genLLVMProgram produces the IR for a whole program -- the user's functions
// AND the C runtime -- and reports the source names it defined.
//
// Owning everything is deliberate. Splitting the program between two generators
// means both need a symbol table, and they will eventually disagree about one:
// over a global's name, over whether an undefined symbol is an import or
// something the other half already defines, over a variadic call the native
// path rewrites by inspecting a format string. Each of those disagreements
// produced a link error or a wrong answer. With one owner there is nothing to
// disagree about.
//
// What goa's own assembler still contributes is the entry stub, which is not C:
// it sets up the process stack per the platform ABI before main runs.
func genLLVMProgram(prog *Program, linux bool, opt int) (string, map[string]bool, error) {
	lib := clibCStore(linux)
	if lib == nil {
		return "", nil, fmt.Errorf("-fllvm: the built-in C library for this target " +
			"is unavailable; run without -fllvm to see why")
	}
	ir, claimed, err := translateProgram(prog, lib, linux)
	if err != nil {
		return "", nil, fmt.Errorf("-fllvm: generating IR: %w", err)
	}
	return ir, claimed, nil
}

// compileIR turns IR text into a COFF object with LLVM, ready to be merged into
// the image goa's assembler builds.
//
// The object goes to a temporary file because that is what LLVM's emitter takes;
// a missing library is reported as such rather than being allowed to look like
// a successful build.
func compileIR(ir string, opt int, linux bool) ([]byte, error) {
	api, err := goa.OpenLLVM()
	if err != nil {
		return nil, fmt.Errorf("-fllvm needs the LLVM shared library: %w", err)
	}
	if linux {
		// The object format follows the target LLVM reports, and the linker
		// half has to agree; an ELF build reaches this only once the ELF object
		// path exists, so say so plainly instead of producing a PE-shaped
		// object for a Linux image.
		return nil, fmt.Errorf("-fllvm is not implemented for the Linux target yet")
	}
	dir, err := os.MkdirTemp("", "goc-llvm-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	obj := filepath.Join(dir, "llvm.obj")
	level := goa.LLVMOptDefault
	if opt >= 3 {
		level = goa.LLVMOptAggressive
	} else if opt >= 1 {
		level = goa.LLVMOptLess
	}
	if err := api.CompileToObject([]byte(ir), obj, level); err != nil {
		return nil, fmt.Errorf("-fllvm: %w", err)
	}
	b, err := os.ReadFile(obj)
	if err != nil {
		return nil, err
	}
	return b, nil
}
