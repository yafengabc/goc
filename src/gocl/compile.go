package gocl

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
	"goc/common"
	"goc/frontend"
	"os"
	"path/filepath"
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
func TranslateProgram(prog *frontend.Program, linux bool, opt int) (string, map[string]bool, []string, error) {
	lib := common.Store(linux)
	if lib == nil {
		libErr := common.Err
		if libErr == nil {
			libErr = fmt.Errorf("no C library was built for this target")
		}
		return "", nil, nil, fmt.Errorf("the built-in C library for this target "+
			"is unavailable: %w", libErr)
	}
	ir, claimed, externals, err := translateProgram(prog, lib, linux, opt)
	if err != nil {
		return "", nil, nil, fmt.Errorf("generating IR: %w", err)
	}
	return ir, claimed, externals, nil
}

// compileIR turns IR text into a COFF object with LLVM, ready to be merged into
// the image goa's assembler builds.
//
// The object goes to a temporary file because that is what LLVM's emitter takes;
// a missing library is reported as such rather than being allowed to look like
// a successful build.
func CompileIR(ir string, opt int, linux bool) ([]byte, error) {
	api, err := OpenLLVM()
	if err != nil {
		return nil, fmt.Errorf("gocl needs the LLVM shared library: %w", err)
	}
	dir, err := os.MkdirTemp("", "goc-llvm-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	obj := filepath.Join(dir, "llvm.obj")
	level := LLVMOptDefault
	if opt >= 3 {
		level = LLVMOptAggressive
	}
	if err := api.CompileToObject([]byte(ir), obj, level, irPasses(opt), linux); err != nil {
		return nil, fmt.Errorf("libLLVM: %w", err)
	}
	b, err := os.ReadFile(obj)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// emitIRAssembly lowers IR to native assembly text and writes it to outPath.
// Used by -dump-asm with the IR kept: the readable artifact is the
// AsmPrinter's output, not
// the entry stub that genWith emits once `claimed` covers every function.
func emitIRAssembly(ir string, outPath string, opt int, linux bool) error {
	api, err := OpenLLVM()
	if err != nil {
		return fmt.Errorf("gocl needs the LLVM shared library: %w", err)
	}
	level := LLVMOptDefault
	if opt >= 3 {
		level = LLVMOptAggressive
	}
	if err := api.CompileToAssembly([]byte(ir), outPath, level, irPasses(opt), linux); err != nil {
		return fmt.Errorf("libLLVM: %w", err)
	}
	return nil
}

// irPasses names the IR optimisation pipeline for a -O level:
//
//	opt 0  no -O      default<O1>   see below
//	opt 1  -O/-Og/-O1 default<O1>
//	opt 2  -Os/-Oz    default<Os>
//	opt 3  -O2        default<O2>
//	opt 4  -O3/-Ofast default<O3>
//
// Code generation alone does not optimise: LLVMTargetMachineEmitToFile lowers
// whatever module it is handed, so with no pipeline every local stays in its
// alloca and nothing is inlined however high -O is. The mapping also had an
// inversion to undo: -O1 used to select LLVMOptLess, which is *lower* than the
// LLVMOptDefault that no flag at all chose, so asking for optimisation made the
// code worse.
//
// opt 0 runs a pipeline too, which is not what its name suggests, and the
// reason is a correctness one rather than a speed one. With no pipeline LLVM
// lowers the call exactly as written and omits the Windows x64 rule that a
// floating-point argument to a variadic function is passed TWICE -- once in the
// XMM register and once in the matching integer register -- because that
// duplication is what lets the callee's va_start find the value in the register
// save area. goc's va_list is a flat eight-byte cursor over exactly that area,
// so without the duplicate a double handed to printf is silently lost:
//
//	printf("%f\n", 1.25)  ->  "d=0.000000" with no pipeline, "d=1.250000" with
//	                          any pipeline. Measured: the unoptimised call site
//	                          has no "movq %xmm1,%rdx"; O1/Os/O2 all do.
//
// A literal, unoptimised translation is therefore not merely slow here, it is
// wrong, so -O0 buys the smallest pipeline instead of none. That costs nothing
// in size either: the -O1 build of bench/bench2.c is 16896 bytes against 19968
// for the unoptimised one.
func irPasses(opt int) string {
	switch {
	case opt >= 4:
		return "default<O3>"
	case opt == 3:
		return "default<O2>"
	// -Os/-Oz ask for size. LLVM 21 removed the `Os` pipeline and replaced it
	// with the optsize attribute under an O2 pipeline, so asking for `Os` here
	// is not merely outdated -- libLLVM rejects the pipeline string outright:
	//
	//	The optimization level "Os" is no longer supported. Use O2 in
	//	conjunction with the optsize attribute instead.
	//
	// The attribute itself is emitted per function, from irMod.optSize.
	case opt == 2:
		return "default<O2>"
	}
	return "default<O1>"
}
