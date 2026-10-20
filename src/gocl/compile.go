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
func TranslateProgram(prog *frontend.Program, linux bool, opt int, arch string) (string, map[string]bool, []string, error) {
	lib := common.Store(linux)
	if lib == nil {
		libErr := common.Err
		if libErr == nil {
			libErr = fmt.Errorf("no C library was built for this target")
		}
		return "", nil, nil, fmt.Errorf("the built-in C library for this target "+
			"is unavailable: %w", libErr)
	}
	// The entry point is a C function on every target this back end owns
	// outright; see injectEntryStub. It has to be decided before any IR is
	// generated and must match the linker's own decision exactly, so both ask
	// the same function.
	if UsesNativeLink(linux, arch, prog, lib) {
		if err := injectEntryStub(prog); err != nil {
			return "", nil, nil, fmt.Errorf("entry point: %w", err)
		}
	}
	ir, claimed, externals, err := translateProgram(prog, lib, linux, opt, arch)
	if err != nil {
		return "", nil, nil, fmt.Errorf("generating IR: %w", err)
	}
	return ir, claimed, externals, nil
}

// UsesNativeLink reports whether a build links through gocld with no assembler
// in the loop -- the arrangement where this module owns the entry point and
// every raw syscall, and gocld only lays out the object.
//
// It is one function on purpose: the IR generator asks it to decide whether to
// inject the C entry stub and to define the syscall stubs, and the driver asks
// it to decide how to link. Two answers to the same question is exactly how a
// module ends up defining a `_start` that goa also defines.
//
// The one program shape it still declines is x86-64 with a thread-local. The
// IR reaches TLS through `__goc_tls_slot`, and both that helper and the .tls
// storage come from goa's entry stub; until the IR carries .tls itself, such a
// program stays on the goa path so it does not lose working TLS. Every other
// architecture is unaffected -- the alternative back ends have no TLS support
// to lose.
func UsesNativeLink(linux bool, arch string, prog *frontend.Program, lib *common.Program) bool {
	if !linux {
		return false // Windows still needs goa for the PE import table
	}
	if arch == "" {
		arch = "x86_64"
	}
	if arch == "x86_64" {
		return len(ComputeTLSLayout(prog, lib, true)) == 0
	}
	return true
}

// entryStubC is the program's entry point, written as C and compiled by this
// back end like any other function.
//
// The alternative -- the arrangement goa used -- is for the assembler half to
// synthesise `_start` in assembly: set up the process stack, call main, then
// terminate. That is assembly nobody type-checks, and it is the reason the
// linker needed goa at all on the x86-64 target. Writing the same three steps
// as C moves the entry point under the same owner as every other function, so
// the whole program is one LLVM module and the linker only has to lay out an
// object it already understands.
//
// The declarations are here so this translation unit parses on its own; both
// names are defined by the program (main) and by the C library
// (__goclib_exit), and only _start itself is carried into the program -- the
// declarations are dropped so they cannot collide with the real prototypes.
//
// __goclib_exit is the right terminator for almost every program: it is the
// bare exit_group, and the C library's std streams are unbuffered precisely so
// that nothing is lost when the stub skips the flush (see the stdout/stderr
// setup in goclib/file.c). A program that needs the full C teardown -- an
// atexit handler above all -- is upgraded after the fact by setStubExit.
const entryStubC = `extern int main(void);
extern void __goclib_exit(long code);

void _start(void)
{
	__goclib_exit(main());
}
`

// injectEntryStub adds the C entry point to prog.
//
// It runs only where nothing else supplies one: a Linux target whose entry
// point and syscalls are both generated by this back end. The x86-64 path
// still hands _start to goa, because goa's version also establishes the
// thread-pointer base (arch_prctl) for a program that uses __thread, and that
// is not modelled here yet -- replacing it would silently break TLS.
func injectEntryStub(prog *frontend.Program) error {
	// frontend.Parse resets the per-translation-unit tables on entry --
	// typedefs, structs, the enumerators -- because every TU that includes
	// a header needs them fresh (see frontend.Parse). This stub is parsed
	// AFTER the program's own units were, so that reset would wipe tables
	// the rest of the build still reads:
	//
	//   - frontend.EnumConsts, which an enumerator used as a global
	//     initialiser folds through ("enum Color g = GREEN"), in the IR
	//     emitter and again in the linker's foldConstInit;
	//   - frontend.Structs, which bit-field eligibility searches when
	//     deciding whether a function may take the IR path at all.
	//
	// The stub declares nothing in either table -- it is two prototypes
	// and one call -- so snapshotting them and putting them back after
	// the parse leaves the tables exactly as they were. It is the same
	// dance common.Translate does when it merges several units, and
	// skipping it is what left every enum-initialised global on the
	// native-link path undefined at link time.
	savedEnums := make(map[string]int64, len(frontend.EnumConsts))
	for k, v := range frontend.EnumConsts {
		savedEnums[k] = v
	}
	savedStructs := make(map[string]*frontend.Type, len(frontend.Structs()))
	for k, v := range frontend.Structs() {
		savedStructs[k] = v
	}
	toks, err := frontend.Lex(entryStubC)
	if err != nil {
		return err
	}
	stub, err := frontend.Parse(toks)
	if err != nil {
		return err
	}
	restoreFrontendTables(savedEnums, savedStructs)
	// Only the definition moves into the program. The two extern declarations
	// above are scaffolding for the parser; the real main and __goclib_exit
	// come from the program and the C library, and adding a second,
	// body-less copy of either would put two entries of one name in the
	// function table.
	for _, f := range stub.Funcs {
		if f.Body != nil {
			prog.Funcs = append(prog.Funcs, f)
		}
	}
	return nil
}

// restoreFrontendTables puts the enumerator and struct tables back the way
// frontend.Parse found them. The stub's parse is expected to have left both
// empty, so this is a straight re-install rather than a merge; a merge would
// keep a name the program does not declare, which is the same table lying
// about what exists.
func restoreFrontendTables(enums map[string]int64, structs map[string]*frontend.Type) {
	for k := range frontend.EnumConsts {
		delete(frontend.EnumConsts, k)
	}
	for k, v := range enums {
		frontend.EnumConsts[k] = v
	}
	live := frontend.Structs()
	for k := range live {
		delete(live, k)
	}
	for k, v := range structs {
		live[k] = v
	}
}

// setStubExit rewrites the entry stub's terminator to name.
//
// It runs after the reachability walk, not before, because whether a program
// needs the full exit is a property of the whole program -- the user's code,
// the printf rewrite and the runtime's own bodies alike -- and the walk is
// the first thing that has seen all three. Deciding it up front from the
// source alone is what the native generator does not do, and it would miss
// exactly the libmisc case: a program whose only sign is atexit(), called
// from main, reaches the runtime's exit through nothing but the stub.
//
// The rewrite is the whole change. The walk has already pulled exit -- and
// through it the flush chain -- into the object on the same evidence
// (llvmRoots step 4), so there is nothing to add and nothing to remove; only
// the stub's own callee was still naming the bare syscall.
//
// A no-op where this back end does not own the entry point: the goa path
// builds the stub in assembly and reads the same decision off the object in
// linkData, so there is no _start here to rewrite.
func setStubExit(prog *frontend.Program, name string) {
	for _, f := range prog.Funcs {
		if f.Name != "_start" || f.Body == nil {
			continue
		}
		frontend.WalkStmts(f.Body, func(s frontend.Stmt) {
			for _, e := range stmtExprs(s) {
				walkExpr(e, func(n frontend.Expr) {
					if c, ok := n.(*frontend.Call); ok && c.Name == "__goclib_exit" {
						c.Name = name
					}
				})
			}
		})
	}
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
// Used by -S (EmitLLVMAsm): lowers IR to native assembly text via the
// AsmPrinter, the way `gcc -S` does. The readable artifact is the C bodies'
// assembly, not the entry stub that genWith emits once `claimed` covers every
// function -- that stub is the -dump-asm debugging aid, handled separately.
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

// EmitIRAssembly lowers IR to native assembly text -- the AsmPrinter's output --
// and writes it to outPath. This is the LLVM back end's answer to `gcc -S`: the
// readable artifact is the assembly the C bodies became, not the entry stub goa
// assembles. The file is not fed back to goa; it is the final artifact, exactly
// like the .s a native `-S` build writes.
func EmitIRAssembly(ir string, outPath string, opt int, linux bool) error {
	return emitIRAssembly(ir, outPath, opt, linux)
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
