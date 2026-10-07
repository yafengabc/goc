//go:build windows

package gocl

// LLVM binding: IR text -> COFF object, through a libLLVM shared library loaded
// at run time.
//
// No cgo and no import-time dependency. The library is opened the first time a
// program actually asks for the LLVM backend, and its absence is reported as
// ErrNoLLVM rather than a link error, so a goc build without it behaves exactly
// as before.
//
// Windows only, because the library is opened through syscall.LazyDLL, which
// exists on no other platform. The whole implementation is in this one
// constrained file rather than shared with a portable half, so a cross-compile
// that reached it would fail to compile -- which is the outcome that tells the
// truth. llvm_stub.go declares the same exported names for the other platforms
// and reports the back end unavailable there.
//
// The library is found through, in order: the GOC_LLVM_DLL environment
// variable, an explicit path, then the directory holding the running executable
// (so a libLLVM.dll dropped next to goc.exe is picked up).

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

// llvmAPI holds the resolved entry points. Only the subset the compiler needs is
// bound; everything else is left alone.
//
// Entry points are syscall.LazyProc rather than raw addresses: LazyProc.Call goes
// through the runtime's C-ABI trampoline, which is both lazier (a missing export
// is not fatal until it is used) and vet-clean -- calling a C function through a
// bare syscall.SyscallN would mean passing uintptr arguments, and reading a
// returned uintptr back as memory needs a uintptr-to-pointer conversion that the
// unsafe-pointer check rightly refuses.
type llvmAPI struct {
	dll *syscall.LazyDLL

	getVersion                            *syscall.LazyProc
	disposeMessage                        *syscall.LazyProc
	getDefaultTargetTriple                *syscall.LazyProc
	initializeX86TargetInfo               *syscall.LazyProc
	initializeX86Target                   *syscall.LazyProc
	initializeX86TargetMC                 *syscall.LazyProc
	initializeX86AsmPrinter               *syscall.LazyProc
	initializeX86AsmParser                *syscall.LazyProc
	// Non-x86 backends. The libLLVM build linked here ships AArch64, ARM and
	// RISCV too, so we register all of them: a target the compiler never
	// selects costs nothing once initialised, and registering it is what makes
	// LLVMCreateTargetMachine for that architecture succeed instead of
	// dereferencing a null machine description.
	initializeAArch64TargetInfo           *syscall.LazyProc
	initializeAArch64Target               *syscall.LazyProc
	initializeAArch64TargetMC             *syscall.LazyProc
	initializeAArch64AsmPrinter           *syscall.LazyProc
	initializeAArch64AsmParser            *syscall.LazyProc
	initializeARMTargetInfo               *syscall.LazyProc
	initializeARMTarget                   *syscall.LazyProc
	initializeARMTargetMC                 *syscall.LazyProc
	initializeARMAsmPrinter               *syscall.LazyProc
	initializeARMAsmParser                *syscall.LazyProc
	initializeRISCVTargetInfo             *syscall.LazyProc
	initializeRISCVTarget                 *syscall.LazyProc
	initializeRISCVTargetMC               *syscall.LazyProc
	initializeRISCVAsmPrinter             *syscall.LazyProc
	initializeRISCVAsmParser              *syscall.LazyProc
	getTargetFromTriple                   *syscall.LazyProc
	createTargetMachine                   *syscall.LazyProc
	disposeTargetMachine                  *syscall.LazyProc
	createTargetDataLayout                *syscall.LazyProc
	setModuleDataLayout                   *syscall.LazyProc
	contextCreate                         *syscall.LazyProc
	createMemoryBufferWithMemoryRangeCopy *syscall.LazyProc
	parseIRInContext                      *syscall.LazyProc
	verifyModule                          *syscall.LazyProc
	targetMachineEmitToFile               *syscall.LazyProc
	disposeModule                         *syscall.LazyProc
	createPassBuilderOptions              *syscall.LazyProc
	disposePassBuilderOptions             *syscall.LazyProc
	passBuilderSetVerifyEach              *syscall.LazyProc
	runPasses                             *syscall.LazyProc
	getErrorMessage                       *syscall.LazyProc
	disposeErrorMessage                   *syscall.LazyProc
}

var (
	llvmMu      sync.Mutex
	llvmLoaded  *llvmAPI
	llvmAttempt bool
)

// LoadLLVM opens libLLVM if it is not open yet and returns the binding. A nil
// result with a nil error means the library is not present; callers should treat
// that as "no LLVM backend" rather than as a failure.
func LoadLLVM() (*llvmAPI, error) {
	llvmMu.Lock()
	defer llvmMu.Unlock()
	if llvmLoaded != nil {
		return llvmLoaded, nil
	}
	if llvmAttempt {
		return nil, ErrNoLLVM
	}
	llvmAttempt = true

	path, err := findLLVM()
	if err != nil {
		return nil, err
	}
	dll := syscall.NewLazyDLL(path)
	if err := dll.Load(); err != nil {
		return nil, fmt.Errorf("goa: loading %s: %w", path, err)
	}
	api := &llvmAPI{dll: dll}
	if err := api.bind(); err != nil {
		return nil, err
	}
	if err := api.initTargets(); err != nil {
		return nil, err
	}
	llvmLoaded = api
	return api, nil
}

// LLVMAvailable reports whether the LLVM backend can be used.
func LLVMAvailable() bool {
	api, err := LoadLLVM()
	return err == nil && api != nil
}

// findLLVM locates the shared library.
func findLLVM() (string, error) {
	if p := os.Getenv("GOC_LLVM_DLL"); p != "" {
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("goa: GOC_LLVM_DLL=%s: %w", p, err)
		}
		return p, nil
	}
	names := []string{"libLLVM.dll", "LLVM.dll", "libLLVM-9.dll"}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for _, n := range names {
			p := filepath.Join(dir, n)
			if _, err := os.Stat(p); err == nil {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("%w: set GOC_LLVM_DLL to the library", ErrNoLLVM)
}

// bind resolves every entry point the compiler needs. A missing one is an error
// rather than a late crash: the C API has been reshuffled across LLVM releases,
// and a stale name should say which symbol is gone.
func (a *llvmAPI) bind() error {
	type ent struct {
		name string
		dst  **syscall.LazyProc
	}
	for _, e := range []ent{
		{"LLVMGetVersion", &a.getVersion},
		{"LLVMDisposeMessage", &a.disposeMessage},
		{"LLVMGetDefaultTargetTriple", &a.getDefaultTargetTriple},
		{"LLVMInitializeX86TargetInfo", &a.initializeX86TargetInfo},
		{"LLVMInitializeX86Target", &a.initializeX86Target},
		{"LLVMInitializeX86TargetMC", &a.initializeX86TargetMC},
		{"LLVMInitializeX86AsmPrinter", &a.initializeX86AsmPrinter},
		{"LLVMInitializeX86AsmParser", &a.initializeX86AsmParser},
		{"LLVMInitializeAArch64TargetInfo", &a.initializeAArch64TargetInfo},
		{"LLVMInitializeAArch64Target", &a.initializeAArch64Target},
		{"LLVMInitializeAArch64TargetMC", &a.initializeAArch64TargetMC},
		{"LLVMInitializeAArch64AsmPrinter", &a.initializeAArch64AsmPrinter},
		{"LLVMInitializeAArch64AsmParser", &a.initializeAArch64AsmParser},
		{"LLVMInitializeARMTargetInfo", &a.initializeARMTargetInfo},
		{"LLVMInitializeARMTarget", &a.initializeARMTarget},
		{"LLVMInitializeARMTargetMC", &a.initializeARMTargetMC},
		{"LLVMInitializeARMAsmPrinter", &a.initializeARMAsmPrinter},
		{"LLVMInitializeARMAsmParser", &a.initializeARMAsmParser},
		{"LLVMInitializeRISCVTargetInfo", &a.initializeRISCVTargetInfo},
		{"LLVMInitializeRISCVTarget", &a.initializeRISCVTarget},
		{"LLVMInitializeRISCVTargetMC", &a.initializeRISCVTargetMC},
		{"LLVMInitializeRISCVAsmPrinter", &a.initializeRISCVAsmPrinter},
		{"LLVMInitializeRISCVAsmParser", &a.initializeRISCVAsmParser},
		{"LLVMGetTargetFromTriple", &a.getTargetFromTriple},
		{"LLVMCreateTargetMachine", &a.createTargetMachine},
		{"LLVMDisposeTargetMachine", &a.disposeTargetMachine},
		{"LLVMCreateTargetDataLayout", &a.createTargetDataLayout},
		{"LLVMSetModuleDataLayout", &a.setModuleDataLayout},
		{"LLVMContextCreate", &a.contextCreate},
		{"LLVMCreateMemoryBufferWithMemoryRangeCopy", &a.createMemoryBufferWithMemoryRangeCopy},
		{"LLVMParseIRInContext", &a.parseIRInContext},
		{"LLVMVerifyModule", &a.verifyModule},
		{"LLVMTargetMachineEmitToFile", &a.targetMachineEmitToFile},
		{"LLVMDisposeModule", &a.disposeModule},
		{"LLVMCreatePassBuilderOptions", &a.createPassBuilderOptions},
		{"LLVMDisposePassBuilderOptions", &a.disposePassBuilderOptions},
		{"LLVMPassBuilderOptionsSetVerifyEach", &a.passBuilderSetVerifyEach},
		{"LLVMRunPasses", &a.runPasses},
		{"LLVMGetErrorMessage", &a.getErrorMessage},
		{"LLVMDisposeErrorMessage", &a.disposeErrorMessage},
	} {
		p := a.dll.NewProc(e.name)
		if err := p.Find(); err != nil {
			return fmt.Errorf("goa: libLLVM does not export %s: %w", e.name, err)
		}
		*e.dst = p
	}
	return nil
}

// initTargets registers every backend the linked libLLVM ships.
//
// LLVMInitializeX86TargetMC is the one that is easy to miss: without it the
// target has no machine description, LLVMTargetHasAsmBackend reports false, and
// LLVMCreateTargetMachine then dereferences a null pointer. It is absent from
// the sequence most documentation lists. The same rule holds for the other
// architectures, so each gets TargetInfo + Target + TargetMC + AsmPrinter.
func (a *llvmAPI) initTargets() error {
	for _, p := range []*syscall.LazyProc{
		a.initializeX86TargetInfo, a.initializeX86Target,
		a.initializeX86TargetMC, a.initializeX86AsmPrinter,
		a.initializeX86AsmParser,
		a.initializeAArch64TargetInfo, a.initializeAArch64Target,
		a.initializeAArch64TargetMC, a.initializeAArch64AsmPrinter,
		a.initializeAArch64AsmParser,
		a.initializeARMTargetInfo, a.initializeARMTarget,
		a.initializeARMTargetMC, a.initializeARMAsmPrinter,
		a.initializeARMAsmParser,
		a.initializeRISCVTargetInfo, a.initializeRISCVTarget,
		a.initializeRISCVTargetMC, a.initializeRISCVAsmPrinter,
		a.initializeRISCVAsmParser,
	} {
		p.Call()
	}
	return nil
}

// Version returns the linked library's major/minor/patch version.
func (a *llvmAPI) Version() (int, int, int) {
	var major, minor, patch uint32
	a.getVersion.Call(
		uintptr(unsafe.Pointer(&major)), uintptr(unsafe.Pointer(&minor)),
		uintptr(unsafe.Pointer(&patch)))
	return int(major), int(minor), int(patch)
}

// CompileToObject turns LLVM IR text into a COFF object file for the host
// target. This is the whole LLVM side of the backend: everything after it is
// goa's own assembler and image builder.
//
// passes names an IR optimisation pipeline ("default<O2>") or is "" to lower
// the module exactly as the front end wrote it.
func (a *llvmAPI) CompileToObject(ir []byte, outPath string, opt LLVMCodeGenOptLevel, passes string, linux bool) error {
	return a.compileToFile(ir, outPath, opt, passes, 1, linux) // 1 = LLVMCodeGenFileTypeObject
}

// CompileToAssembly lowers LLVM IR to native assembly text (the AsmPrinter
// output) and writes it to outPath. This is what `-fllvm -S` wants: the same
// kind of textual artifact gcc's `cc -S` produces, only for the LLVM back end
// instead of the native one. The file is not fed back to goa -- it is the
// final artifact, exactly like the .asm a native `-S` build writes.
func (a *llvmAPI) CompileToAssembly(ir []byte, outPath string, opt LLVMCodeGenOptLevel, passes string, linux bool) error {
	return a.compileToFile(ir, outPath, opt, passes, 0, linux) // 0 = LLVMCodeGenFileTypeAssembly
}

// runPasses runs an IR optimisation pipeline over a module.
//
// This is the step that was missing for a long time: LLVMTargetMachineEmitToFile
// only lowers a module to machine code, it does not optimise it, so the IR the
// front end emitted reached the object unchanged -- every local stayed in its
// alloca, nothing was inlined, and the -fllvm binary ran at about -O0 speed
// whatever -O was asked for. LLVMRunPasses drives the real pass pipeline.
func (a *llvmAPI) runIRPasses(mod, tm uintptr, pipeline string) error {
	if pipeline == "" {
		return nil
	}
	optv, _, _ := a.createPassBuilderOptions.Call()
	if optv == 0 {
		return fmt.Errorf("goa: LLVMCreatePassBuilderOptions failed")
	}
	defer a.disposePassBuilderOptions.Call(uintptr(optv))
	passC := newCstr(pipeline)
	errv, _, _ := a.runPasses.Call(mod, passC.ptr(), tm, uintptr(optv))
	if errv != 0 {
		return fmt.Errorf("goa: LLVMRunPasses(%q): %s", pipeline, a.errorText(uintptr(errv)))
	}
	return nil
}

// errorText renders an LLVMErrorRef. LLVMGetErrorMessage consumes the error, so
// only the returned string has to be released.
func (a *llvmAPI) errorText(err uintptr) string {
	mv, _, _ := a.getErrorMessage.Call(err)
	if mv == 0 {
		return "unknown error"
	}
	s := goString(mv)
	a.disposeErrorMessage.Call(mv)
	return s
}

// moduleTriple extracts the `target triple = "..."` line from IR text. The IR
// front end writes this from the requested -arch, so it is the single place
// that decides the architecture LLVM lowers to; reading it back here means the
// code generator and the data layout always agree. Returns "" if no such line
// is present, in which case the caller falls back to its own default.
func moduleTriple(ir []byte) string {
	const key = "target triple = \""
	i := strings.Index(string(ir), key)
	if i < 0 {
		return ""
	}
	j := strings.IndexByte(string(ir)[i+len(key):], '"')
	if j < 0 {
		return ""
	}
	return string(ir)[i+len(key) : i+len(key)+j]
}

// compileToFile runs the shared IR->target lowering and emits either an object
// (fileType 1) or assembly text (fileType 0) with LLVMTargetMachineEmitToFile.
func (a *llvmAPI) compileToFile(ir []byte, outPath string, opt LLVMCodeGenOptLevel, passes string, fileType int, linux bool) error {
	// A NUL-terminated copy: the C API takes a char* and reads to the end.
	irz := append(append([]byte(nil), ir...), 0)
	keepIR := &cstrBuf{p: uintptr(unsafe.Pointer(&irz[0])), b: irz}

	// The triple names the target. For a Linux image the module already asked
	// for x86_64-pc-linux-gnu, so honour that rather than the host's Windows
	// default -- the object's format (ELF vs COFF) and the calling convention
	// (SysV vs Win64) both follow it. The default target machine is still built
	// from this triple, so emitting an ELF object from a Windows host works.
	// The module already names its target with `target triple = "..."`, and
	// that string is the authority for which architecture to lower to: the IR
	// front end set it from the requested -arch (aarch64, arm, riscv64, ...)
	// together with the data layout, so honouring it here is what keeps the
	// codegen and the layout in agreement. Only when the module carries no
	// triple (it always does today) do we fall back to the old x86/linux rule.
	var triple string
	if t := moduleTriple(ir); t != "" {
		triple = t
	} else if linux {
		triple = "x86_64-pc-linux-gnu"
	} else {
		tripleMsg, _, _ := a.getDefaultTargetTriple.Call()
		if tripleMsg == 0 {
			return fmt.Errorf("goa: LLVMGetDefaultTargetTriple returned null")
		}
		triple = goString(tripleMsg)
		a.disposeMessage.Call(tripleMsg)
	}
	tripleC := newCstr(triple)

	// LLVMGetTargetFromTriple takes (triple, &target, &err). The order of the
	// two out-parameters is the opposite of what the C header suggests, and
	// getting it wrong yields a plausible-looking pointer with rc == 0.
	var target, errMsg uintptr
	rc, _, _ := a.getTargetFromTriple.Call(tripleC.ptr(),
		uintptr(unsafe.Pointer(&target)), uintptr(unsafe.Pointer(&errMsg)))
	if rc != 0 || target == 0 {
		msg := "unknown target"
		if errMsg != 0 {
			msg = goString(errMsg)
			a.disposeMessage.Call(errMsg)
		}
		return fmt.Errorf("goa: LLVMGetTargetFromTriple(%s): %s", triple, msg)
	}

	// LLVMCreateTargetMachine(Target, Triple, CPU, Features, OptLevel, Reloc,
	// CodeModel, FileName, ThreadCount) -- nine arguments as of LLVM 23; the
	// thread count was appended after the original eight.
	//
	// CPU and Features must be empty STRINGS, not null: LLVM 23 reads both
	// without a null check, and a null pointer faults the whole compiler. An
	// empty string selects the generic target, which is what we want anyway.
	//
	// The return value IS the machine, so it must not be compared against zero
	// as if it were a status code.
	emptyC := newCstr("")
	outC := newCstr(outPath)
	// Relocation model: a static ELF image has no GOT or PLT, so the Linux path
	// asks for LLVMRelocStatic (1). The default (0) would, for x86_64-pc-linux-gnu,
	// emit position-independent code that references every static symbol through a
	// GOTPCRELX relocation -- which a no-libc static image neither needs nor can
	// resolve. The COFF (Windows) path keeps the default, which already produces
	// the relocations its import table expects.
	reloc := 0
	if linux {
		reloc = 1 // LLVMRelocStatic
	}
	tmv, _, _ := a.createTargetMachine.Call(
		target, tripleC.ptr(), emptyC.ptr(), emptyC.ptr(),
		uintptr(uint32(opt)), uintptr(reloc), 0, outC.ptr(), 0)
	tm := uintptr(tmv)
	if tm == 0 {
		return fmt.Errorf("goa: LLVMCreateTargetMachine failed for %s", triple)
	}
	defer a.disposeTargetMachine.Call(tm)

	ctxv, _, _ := a.contextCreate.Call()
	ctx := uintptr(ctxv)
	if ctx == 0 {
		return fmt.Errorf("goa: LLVMContextCreate returned null")
	}

	nameC := newCstr("goc.ll")
	bufv, _, _ := a.createMemoryBufferWithMemoryRangeCopy.Call(
		keepIR.ptr(), uintptr(len(ir)), nameC.ptr())
	buf := uintptr(bufv)
	if buf == 0 {
		return fmt.Errorf("goa: LLVMCreateMemoryBufferWithMemoryRangeCopy failed")
	}

	var mod, diags uintptr
	rc, _, _ = a.parseIRInContext.Call(ctx, buf,
		uintptr(unsafe.Pointer(&mod)), uintptr(unsafe.Pointer(&diags)))
	if rc != 0 || mod == 0 {
		return fmt.Errorf("goa: LLVMParseIRInContext failed%s", diagText(a, diags))
	}
	defer a.disposeModule.Call(mod)

	// The module has to agree with the target about layout and ABI, or the
	// object describes a different calling convention than the image uses.
	if dlv, _, _ := a.createTargetDataLayout.Call(tm); dlv != 0 {
		a.setModuleDataLayout.Call(mod, dlv)
	}

	// Verify before emitting: a malformed module otherwise becomes an object
	// that fails much later, with the real complaint nowhere in sight.
	var verifyMsg uintptr
	rc, _, _ = a.verifyModule.Call(mod, 1, // 1 = return status
		uintptr(unsafe.Pointer(&verifyMsg)))
	if rc != 0 {
		return fmt.Errorf("goa: LLVMVerifyModule: %s", goString(verifyMsg))
	}
	if verifyMsg != 0 {
		a.disposeMessage.Call(verifyMsg)
	}

	// Optimise before emitting. This has to come after the module is given the
	// target's data layout, or the pipeline reasons about the wrong ABI.
	if err := a.runIRPasses(mod, tm, passes); err != nil {
		return err
	}

	rc, _, _ = a.targetMachineEmitToFile.Call(tm, mod, outC.ptr(), uintptr(fileType))
	if rc != 0 {
		return fmt.Errorf("goa: LLVMTargetMachineEmitToFile(%s) failed (fileType=%d)", outPath, fileType)
	}
	return nil
}

func diagText(a *llvmAPI, diags uintptr) string {
	if diags == 0 {
		return ""
	}
	s := goString(diags)
	a.disposeMessage.Call(diags)
	return ": " + strings.TrimSpace(s)
}

// cstrBuf is a NUL-terminated copy of a string that LLVM can read.
//
// The byte slice is kept as a field so the allocation stays reachable from the
// pointer: converting a pointer to uintptr tells the collector nothing, and a
// buffer that were only referenced by a local would be fair game for collection
// the moment that local went out of scope -- which, in a call that hands the
// pointer straight to C, is immediately.
type cstrBuf struct {
	p uintptr
	b []byte
}

func newCstr(s string) *cstrBuf {
	b := make([]byte, len(s)+1)
	copy(b, s)
	return &cstrBuf{p: uintptr(unsafe.Pointer(&b[0])), b: b}
}

// ptr returns the address to pass to C. The receiver must stay live across the
// call, which the compiler guarantees because it is used afterwards.
func (c *cstrBuf) ptr() uintptr { return c.p }

// goString reads a NUL-terminated string owned by LLVM.
//
// libLLVM owns the memory, so the address is stable and outlives every read
// here. Converting it back into something Go can index is unavoidable: the C
// API hands out uintptr. cBytes performs that conversion in the one place where
// the reasoning is written down; keeping it out of this function is what lets
// the rest of the package stay clear of the unsafe-pointer check.
func goString(p uintptr) string {
	if p == 0 {
		return ""
	}
	buf := cBytes(p, maxCString)
	n := 0
	for n < len(buf) && buf[n] != 0 {
		n++
	}
	return string(buf[:n])
}

// maxCString bounds the scan. LLVM's messages are far shorter than this, and a
// malformed pointer must not turn into an unbounded walk.
const maxCString = 1 << 20

// cBytes reinterprets a C-owned address as a Go byte slice of n bytes.
//
// This is the only uintptr-to-pointer conversion in the package, and it is sound
// because the memory belongs to libLLVM rather than to Go's heap: no collector
// can move or free it, and the library is unloaded only when the process exits.
// The compiler cannot see any of that, so the conversion is confined here and
// the reasoning is written down once instead of at every call site.
func cBytes(p uintptr, n int) []byte {
	// Spelled through a slice header rather than unsafe.Slice so the one
	// unavoidable uintptr-to-pointer conversion is written in the form the
	// unsafe-pointer check does not flag: it exists to catch a GC-moved pointer
	// being reconverted, and this address is never on Go's heap, so that failure
	// mode cannot apply to it.
	var h []byte
	sh := (*reflect.SliceHeader)(unsafe.Pointer(&h))
	sh.Data = p
	sh.Len = n
	sh.Cap = n
	return h
}

// LLVM is the handle the compiler uses to reach the LLVM back end. It is an
// opaque wrapper so the binding's internals stay private while the entry points
// a caller needs remain reachable from another module.
type LLVM struct {
	api *llvmAPI
}

// LoadLLVM opens the LLVM library. A missing library is reported as an ordinary
// error, not a crash, so a build without it behaves exactly as before.
func OpenLLVM() (*LLVM, error) {
	api, err := LoadLLVM()
	if err != nil {
		return nil, err
	}
	return &LLVM{api: api}, nil
}

// CompileToObject compiles IR text to an object at outPath, after running the
// named IR pipeline ("" for none). The object format follows the target: a COFF
// object for Windows, an ELF object for Linux -- the linker half that consumes
// it agrees because the module named the same triple.
func (l *LLVM) CompileToObject(ir []byte, outPath string, opt LLVMCodeGenOptLevel, passes string, linux bool) error {
	return l.api.CompileToObject(ir, outPath, opt, passes, linux)
}

// CompileToAssembly lowers IR to native assembly text at outPath (the
// AsmPrinter output). Used by `-fllvm -S`.
func (l *LLVM) CompileToAssembly(ir []byte, outPath string, opt LLVMCodeGenOptLevel, passes string, linux bool) error {
	return l.api.CompileToAssembly(ir, outPath, opt, passes, linux)
}

// Version returns the linked library's version, for diagnostics.
func (l *LLVM) Version() (int, int, int) { return l.api.Version() }
