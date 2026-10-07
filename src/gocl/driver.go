// The gocl driver: the command line, the build decision, and the pieces of the
// pipeline that turn one preprocessed, checked program into an executable.
//
// It lives in package gocl rather than in a main package so that the entry point
// can drive it from outside: src/gocl.go, built with `-tags gocl`, calls Main
// here after installing the embedded C library. Keeping one driver means the flag
// set and the build sequence have a single definition, and it mirrors how goc
// exposes compiler.Main from its own package.
//
// The pipeline has the same shape as goc's, with one owner for the whole
// program. Every C function -- the user's and the C runtime's alike -- becomes
// LLVM IR, libLLVM compiles that IR to a single object, and goa contributes the
// entry stub and lays out the image. One owner means there is never a question
// of which half defined a symbol, which is what let the two-generator design
// accumulate link errors over a global's name, over an undefined symbol, and
// over variadic calls.
//
// The parts worth reading are Compile (which decides the program's entry point
// and hands the layout to common/link) and Main (which reports what the build
// produced).
package gocl

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"goa"
	"goc/common"
	"goc/common/link"
	"goc/frontend"
	"gocld"
)

// Config is one build, as the command line describes it.
type Config struct {
	Linux  bool
	WinGUI bool // -mwindows: PE subsystem 2 (GUI), no console window
	Opt    int  // optimisation level, 0 = none
	// Arch is the instruction set to compile for: "x86_64" (default), "aarch64",
	// "arm" (armv7 hard-float), "armel" (armv7 soft-float), "riscv64",
	// "riscv32". It names the LLVM target triple and
	// data layout the IR is lowered to, and on a Linux target decides whether
	// the program links through gocld alone or through goa's assembler.
	Arch string
	// DumpIR keeps the LLVM IR beside the output, so a failing build can be
	// read at the level LLVM actually saw.
	DumpIR bool
	// DumpAsm writes the entry stub and image layout instead of linking. The
	// stub is goa's assembly for the whole program under this back end -- the
	// bodies are in the object -- so it is small, and it is the level at which
	// "where did this symbol come from" is answerable. It is a debugging aid
	// for the assembler path, distinct from EmitLLVMAsm (-S), which writes the
	// AsmPrinter's .s for the C bodies themselves.
	DumpAsm bool
	// EmitLLVMAsm lowers the program to native assembly text via LLVM's
	// AsmPrinter and stops -- the `-S` behaviour. The artifact is the .s a
	// `gcc -S` would write: the C function bodies in AT&T syntax.
	EmitLLVMAsm bool
	// PreprocessOnly is -E: emit the preprocessed source and stop.
	PreprocessOnly bool
	OutFile        string
	Defines        []string
	IncDirs        []string
	Inputs         []string
}

// Main runs one build and returns the process exit code.
//
// The exit code is returned rather than taken with os.Exit so that a caller
// embedding this compiler can decide what to do about a failure -- a build
// tool driving several compiles wants a diagnostic and a continue.
func Main(args []string) int {
	cfg, err := parseArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gocl:", err)
		return 2
	}
	if cfg == nil {
		return 0 // --help or --version already printed
	}
	// The C library has to be compiled before anything is preprocessed: the
	// preprocessor resolves `#include <stdio.h>` out of it, so reaching that
	// with no library is a nil dereference inside a header lookup rather than
	// a diagnostic. The load itself is lazy (see common.Ensure) so that an
	// embedding program can install its own library first.
	common.Ensure()
	if common.Err != nil {
		fmt.Fprintln(os.Stderr, "gocl:", common.Err)
		return 1
	}
	out, err := Compile(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gocl:", err)
		return 1
	}
	if out != "" {
		fmt.Println(out)
	}
	return 0
}

// Compile turns a Config into an executable, and returns a message rather than
// an empty string when the build produced a file the caller should know about
// (a Linux target, which cannot be run here).
func Compile(cfg *Config) (string, error) {
	if len(cfg.Inputs) == 0 {
		return "", fmt.Errorf("no input files")
	}
	if cfg.PreprocessOnly {
		return "", preprocessOnly(cfg)
	}
	prog, err := common.Translate(cfg.Inputs, cfg.Defines, cfg.Linux, cfg.IncDirs...)
	if err != nil {
		return "", err
	}
	// Check reports every semantic error it found rather than stopping at the
	// first, so one compile tells the user everything that is wrong.
	if errs := frontend.Check(prog); len(errs) > 0 {
		var b strings.Builder
		b.WriteString("type error(s):")
		for _, e := range errs {
			fmt.Fprintf(&b, "\n  %s", e.Error())
		}
		return "", errors.New(b.String())
	}

	// One owner: the whole program, runtime included, goes down the IR path.
	ir, claimed, externals, err := TranslateProgram(prog, cfg.Linux, cfg.Opt, cfg.Arch)
	if err != nil {
		return "", err
	}
	if cfg.DumpIR {
		if err := os.WriteFile(dumpIRPath(cfg), []byte(ir), 0644); err != nil {
			return "", err
		}
	}
	if cfg.EmitLLVMAsm {
		// -S / -c: lower to native assembly text via the AsmPrinter and stop.
		// The artifact is the .s a `gcc -S` would write: the C function bodies
		// in AT&T syntax; the entry stub is not part of it, just as CRT startup
		// is absent from a gcc -S .s. -o names the file directly; without it
		// the .s lands next to the source, like the .exe would.
		asmPath := cfg.OutFile
		if asmPath == "" {
			asmPath = strings.TrimSuffix(outputPath(cfg), ".exe") + ".s"
		}
		if err := EmitIRAssembly(ir, asmPath, cfg.Opt, cfg.Linux); err != nil {
			return "", err
		}
		return "", nil
	}
	obj, err := CompileIR(ir, cfg.Opt, cfg.Linux)
	if cfg.DumpIR && os.Getenv("GOC_DUMP_OBJ") != "" {
		os.WriteFile(os.Getenv("GOC_DUMP_OBJ"), obj, 0644)
	}
	if err != nil {
		return "", err
	}

	// The linker decision is the same one the IR generator used, so the two
	// halves cannot disagree about who owns `_start`. Where this back end owns
	// the entry point and the syscalls outright (every non-x86_64 Linux
	// target, and x86-64 Linux without TLS), the object is linked straight by
	// gocld with no assembler in the loop; everywhere else the goa path lays
	// out the image and supplies the entry stub.
	if UsesNativeLink(cfg.Linux, cfg.Arch, prog, common.Store(cfg.Linux)) {
		outPath := outputPath(cfg)
		if err := linkNativeELF(obj, outPath); err != nil {
			return "", err
		}
		return fmt.Sprintf("(ELF binary: run it on Linux/%s): %s", cfg.Arch, outPath), nil
	}

	d, err := linkData(prog, cfg, claimed, externals, obj)
	if err != nil {
		return "", err
	}
	asm, err := link.Emit(d)
	if err != nil {
		return "", err
	}
	outPath := outputPath(cfg)
	if cfg.DumpAsm {
		// -S / -dump-asm: write the assembly gocl emits (the entry stub goa
		// assembles) and stop before linking. A -o path is used verbatim, so
		// `gocl -S f.c -o f.asm` lands there; without -o the file is named
		// after the source as <source>.stub.asm, kept separate from any .exe.
		asmPath := cfg.OutFile
		if asmPath == "" {
			asmPath = strings.TrimSuffix(outPath, ".exe") + ".stub.asm"
		}
		if err := os.WriteFile(asmPath, []byte(asm), 0644); err != nil {
			return "", err
		}
		return "", nil
	}
	if _, err := goaAssemble(asm, obj, outPath, cfg.Linux); err != nil {
		return "", err
	}
	if cfg.Linux {
		return fmt.Sprintf("(ELF binary: run it on Linux): %s", outPath), nil
	}
	return outPath, nil
}

// linkData assembles what the linker needs to know.
//
// Almost everything is empty, and that is the point: the IR defines every
// function and every global, so this half contributes no bodies and no .data.
// What it does contribute is the startup work LLVM cannot do -- binding the
// address of a global into a pointer initialiser, which in IR is a relative
// constant and in the image has to be a `lea`.
func linkData(prog *frontend.Program, cfg *Config, claimed map[string]bool, externals []string, obj []byte) (*link.Data, error) {
	funcs := map[string]*frontend.FuncDecl{}
	for _, f := range prog.Funcs {
		funcs[f.Name] = f
	}
	entry, err := link.ResolveEntry(funcs, cfg.Linux)
	if err != nil {
		return nil, err
	}
	// The C library's exit pulls in the stdio flush chain. It is used whenever
	// the program can reach an exit at all, which a program with an entry point
	// always can, so there is nothing to detect here -- the tree walk that
	// decided reachability in goc was a cost the split removes.
	if common.Store(cfg.Linux) != nil {
		if _, ok := common.Store(cfg.Linux).Funcs["__goclib_exit"]; ok {
			entry.Exit = "__goclib_exit"
		}
	}
	// Globals maps a C name to the symbol the object actually defines. The IR
	// front end prefixes every global with G_, and under this mode the symbol
	// is also its address -- there is no separate label to bind.
	globals := map[string]string{}
	for _, g := range prog.Globals {
		if g.IsTLS {
			// A thread-local global is not in the object's global namespace at
			// all; the .tls layout is the linker's business and the label comes
			// from Data.TLSVars.
			continue
		}
		globals[g.Name] = "G_" + g.Name
	}
	d := &link.Data{
		Program: prog,
		Linux:   cfg.Linux,
		WinGUI:  cfg.WinGUI,
		Opt:     cfg.Opt,
		Entry:   entry,
		// Every external call the program made is now a symbol the LLVM object
		// references; the stub has to declare the ones that come from the OS
		// rather than from the object.
		Imports: externalImports(prog, cfg.Linux, obj),
		Globals: globals,
		// Thread-local globals are reached through the linker-provided slot
		// helper, so they are absent from Globals above. The linker still has to
		// lay them out in the .tls section and build the image's TLS directory
		// (or set the Linux fs base); this is the same layout the IR emitter
		// used, so the access code and the section storage agree on every
		// offset.
		TLSVars: ComputeTLSLayout(prog, common.Store(cfg.Linux), cfg.Linux),
		// FuncAddr resolves a function designator in a static initialiser. In a
		// full-LLVM build the function's symbol *is* its address, so this only
		// has to confirm the object defines it.
		FuncAddr: func(name string) (string, bool) {
			return name, claimed[name]
		},
		// The object defines most globals, so this half must not emit a second
		// image of them. The exception is a global whose address a static
		// initialiser takes: the IR front end emits those as `external` with no
		// storage, so its .data image is still this half's job. The predicate is
		// asked per label, which is why answering needs the prefix map rather
		// than a mode flag.
		SymbolInObject: func(label string) bool { return claimed[label] },
		// Only the globals the IR left as `external` need storage here; the
		// rest already have an image in the object.
		NeedsSlotBinding: func(label string) bool {
			for _, e := range externals {
				if e == label {
					return true
				}
			}
			return false
		},
		// A string literal's address LLVM resolves itself -- it emits the
		// getelementptr as the initialiser -- so the stub has nothing to bind
		// here and an empty table is the honest answer, not a missing one.
		// The map has to exist regardless: the initialiser walk assigns into it
		// whenever it meets a literal the object did not already carry.
		StrLabs: map[*frontend.StrLit]string{},
	}
	return d, nil
}

// externalImports lists the extern declarations the entry stub needs: the OS
// entry points the program actually reaches, and therefore the only symbols the
// LLVM object leaves undefined. Declaring every prototype a header declares --
// <windows.h> names hundreds of Win32 functions -- would import all of them and
// inflate the import table by ~10 KB for a program that calls one (a MessageBoxA
// "Hello World" is the classic case). The linker resolves each undefined object
// symbol against this set, so anything the code or its C library reaches must be
// here; everything else is dead weight.
func externalImports(prog *frontend.Program, linux bool, obj []byte) []string {
	seen := map[string]bool{}
	// Each import is stored newline-terminated, matching the convention native
	// goc uses (codegen.go emits "extern %s, %s\n"). emit.go writes the strings
	// verbatim, one per line; without the trailing newline two externs would
	// collapse onto one line and goa's assembler -- which splits on '\n' and
	// only reads an `extern` that starts a line -- would drop all but the
	// first, leaving the others undefined at link time.
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		seen[s+"\n"] = true
	}
	if linux && len(obj) > 0 {
		// Declare a syscall stub for every undefined symbol the LLVM object
		// references that goa knows as a raw Linux syscall. The library's own
		// functions (read, write, exit_group, the __goclib_* aliases, ...) are
		// undefined in the object and must resolve against a goa stub; anything
		// else (memcpy, the library's own globals, ...) is defined in the
		// object and needs no extern. goclib's __goclib_exit is one of those
		// defined symbols, so it is not declared here either -- the stub calls
		// it and the object satisfies the reference.
		if syms, err := gocld.UndefinedELFSymbols(obj); err == nil {
			for _, name := range syms {
				if goa.IsLinuxSyscall(name) {
					add("extern " + name + ", ;")
				}
			}
		}
		return externalImportsOut(seen)
	}
	// Windows: build the name -> DLL map from the prototypes the program
	// declared, then declare only the ones the object actually leaves
	// undefined. name -> DLL is a lookup, not an iteration, so a program that
	// never calls a function never imports it.
	dllOf := map[string]string{}
	// The library's prototypes first, then the program's. The order matters for
	// a program that uses sockets: `accept', `bind', `WSAStartup' and the rest
	// are declared with their `, ws2_32' annotation in goclib's winsock2.h,
	// which the program never includes -- it includes <socket.h>, whose names
	// are goclib's own wrappers. The annotation is therefore only visible to the
	// library build, and a map seeded from the program alone leaves every one of
	// them without a DLL, which shows up as a link error listing exactly the
	// calls the program made (WSAGetLastError, WSAStartup, accept, bind, ...)
	// rather than as anything to do with imports.
	//
	// common.DLLNames is that library-side view: common.Build records every
	// prototype's DLL while it compiles goclib, and goc's own back end reads
	// the same map.
	for name, dll := range common.DLLNames {
		dllOf[name] = dll
	}
	for _, f := range prog.Prototypes {
		if f.DLL != "" {
			dllOf[f.Name] = f.DLL
		}
	}
	undefs, err := gocld.UndefinedSymbols(obj)
	if err != nil {
		// A parser hiccup must never masquerade as a missing import: fall back
		// to declaring every known DLL prototype (the pre-fix behaviour) so the
		// link still succeeds. The merged map, for the same reason as above.
		for name, dll := range dllOf {
			add("extern " + name + ", " + dll)
		}
	} else {
		for _, name := range undefs {
			if dll, ok := dllOf[name]; ok {
				add("extern " + name + ", " + dll)
			}
		}
	}
	return externalImportsOut(seen)
}

func externalImportsOut(seen map[string]bool) []string {
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sortStrings(out)
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// preprocessOnly handles -E: the preprocessed source, no parsing.
// goaAssemble links the stub assembly with the LLVM object into one image.
//
// The object comes first to goa and the assembly second, which reads oddly
// but is the whole point of the split: goa's assembler contributes the entry
// stub and the image layout, and every C function already lives in the object.
func goaAssemble(asm string, obj []byte, outPath string, linux bool) (int64, error) {
	return goa.AssembleWithObject(asm, obj, outPath, linux)
}

// linkNativeELF links the IR object into a runnable ELF with gocld, with no
// assembler anywhere in the loop.
//
// The three steps are the ones the native back end already takes for x86-64
// Linux (src/goc/main.go): read the object, settle the symbol table, write the
// image with an entry named. What the native path additionally has is the
// assembly entry stub; here `_start` came out of the IR as an ordinary defined
// function, so pickEntry finds it in the object it just read and the linker has
// nothing left to synthesise.
func linkNativeELF(obj []byte, outPath string) error {
	img := gocld.NewImage(gocld.TargetPE)
	img.Target = gocld.TargetELF
	// Only one object goes in, but deferring the undefined-symbol report is
	// still the right call: it makes the check happen once, in Resolve, after
	// everything has been read, rather than partway through ingest.
	img.DeferUndefined(true)
	if err := img.IngestELFBytes(obj); err != nil {
		return err
	}
	if err := img.Resolve(); err != nil {
		return err
	}
	img.Entry = pickEntry(img)
	_, err := gocld.LinkObject(img, nil, outPath, true)
	return err
}

// pickEntry names the symbol the loader jumps to.
//
// An ELF relocatable object has no entry field -- that belongs to the linked
// image -- so the entry is recovered by name. `_start` leads the list, which is
// the one the generated entry stub defines; `main` is the fallback, so an
// object whose startup was dropped still links and says which symbol it wanted.
func pickEntry(img *gocld.Image) string {
	for _, name := range []string{"_start", "__goc_start", "main"} {
		if _, ok := img.Syms[name]; ok {
			return name
		}
	}
	return ""
}

func preprocessOnly(cfg *Config) error {
	var b strings.Builder
	for _, path := range cfg.Inputs {
		toks, err := common.PreprocessFile(path, cfg.Defines, cfg.Linux, cfg.IncDirs...)
		if err != nil {
			return err
		}
		b.WriteString(common.SerializeTokens(toks))
		b.WriteString("\n")
	}
	if cfg.OutFile == "" {
		fmt.Print(b.String())
		return nil
	}
	return os.WriteFile(cfg.OutFile, []byte(b.String()), 0644)
}

func dumpIRPath(cfg *Config) string {
	if cfg.OutFile != "" {
		return strings.TrimSuffix(cfg.OutFile, filepath.Ext(cfg.OutFile)) + ".ll"
	}
	return strings.TrimSuffix(cfg.Inputs[0], filepath.Ext(cfg.Inputs[0])) + ".ll"
}

// outputPath decides the executable's name. A directory (existing) receives a
// file named after the source; anything else is the name itself, with the
// platform's executable suffix added when it has none.
func outputPath(cfg *Config) string {
	name := cfg.OutFile
	if name == "" {
		name = strings.TrimSuffix(cfg.Inputs[0], filepath.Ext(cfg.Inputs[0]))
	}
	if fi, err := os.Stat(name); err == nil && fi.IsDir() {
		base := filepath.Base(strings.TrimSuffix(cfg.Inputs[0], filepath.Ext(cfg.Inputs[0])))
		if cfg.Linux {
			return filepath.Join(name, base)
		}
		return filepath.Join(name, base+".exe")
	}
	if cfg.Linux {
		return name
	}
	if strings.EqualFold(filepath.Ext(name), ".exe") {
		return name
	}
	return name + ".exe"
}

// parseArgs reads the command line. It returns a nil Config for --help and
// --version, having already printed what was asked for.
//
// The flags are the ones goc accepts, minus -fllvm: this compiler has no other
// back end, so the switch that selected one has nothing to select.
func parseArgs(args []string) (*Config, error) {
	cfg := &Config{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		next := func(flag string) (string, error) {
			// Both spellings are accepted: "-I dir" and "-Idir", and likewise
			// for -D. The joined form is the one build systems actually use.
			if strings.HasPrefix(a, flag) && len(a) > len(flag) {
				return a[len(flag):], nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s needs an argument", flag)
			}
			i++
			return args[i], nil
		}
		switch {
		case a == "--help" || a == "-h":
			// Fprint, not Println: usage already ends in a newline, and adding
			// a second one leaves a blank line before the shell prompt.
			fmt.Fprint(os.Stdout, usage)
			return nil, nil
		case a == "--version":
			fmt.Println("gocl", version)
			return nil, nil
		case a == "-o":
			v, err := next("-o")
			if err != nil {
				return nil, err
			}
			cfg.OutFile = v
		case strings.HasPrefix(a, "-o"):
			cfg.OutFile = a[2:]
		case strings.HasPrefix(a, "-D"):
			v, err := next("-D")
			if err != nil {
				return nil, err
			}
			cfg.Defines = append(cfg.Defines, v)
		case strings.HasPrefix(a, "-I"):
			v, err := next("-I")
			if err != nil {
				return nil, err
			}
			cfg.IncDirs = append(cfg.IncDirs, v)
		case strings.HasPrefix(a, "-O"):
			cfg.Opt = optFromSuffix(a)
		case a == "-target" || a == "--target":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("%s needs an argument", a)
			}
			i++
			switch args[i] {
			case "linux":
				cfg.Linux = true
			case "windows", "win64":
			default:
				return nil, fmt.Errorf("unknown target %q (want linux or windows)", args[i])
			}
		case a == "-arch" || a == "--arch":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("%s needs an argument", a)
			}
			i++
			switch args[i] {
			case "x86_64", "aarch64", "arm", "armel", "riscv64", "riscv32":
				cfg.Arch = args[i]
			default:
				return nil, fmt.Errorf("unknown arch %q (want x86_64, aarch64, arm, armel, riscv64, riscv32)", args[i])
			}
		case strings.HasPrefix(a, "-m") && strings.Contains(a, "windows"):
			cfg.WinGUI = true
		case a == "-dump-ir":
			cfg.DumpIR = true
		case a == "-dump-asm":
			cfg.DumpAsm = true
		case a == "-E":
			cfg.PreprocessOnly = true
		case a == "-S", a == "-c":
			// "Stop before the executable" -- lower the program to native
			// assembly text with LLVM's AsmPrinter and write it, the way
			// `gcc -S` does. gocl's C code goes through LLVM into a COFF
			// object, so the assembly worth showing is the AsmPrinter's .s of
			// the C bodies, not the entry stub goa assembles (that one is the
			// -dump-asm debugging aid).
			cfg.EmitLLVMAsm = true
		case strings.HasPrefix(a, "-"):
			// An unrecognised flag is ignored rather than rejected. Build
			// scripts pass gcc's whole vocabulary to the compiler driver, and
			// failing on -Wall or -pthread would make gocl unusable in one.
		default:
			cfg.Inputs = append(cfg.Inputs, a)
		}
	}
	if len(cfg.Inputs) == 0 {
		return nil, fmt.Errorf("no input files")
	}
	return cfg, nil
}

const usage = `usage: gocl [options] file.c [file2.c ...]

  -o <file>        write the executable here (a directory receives a file
                   named after the source)
  -Dname[=value]   define a macro; without a value it is 1
  -Idir            add dir to the header search path
  -O, -O1..-O3, -Os, -Oz, -Ofast
                   optimisation level
  -target linux    emit an ELF binary instead of a PE
  -arch <isa>      instruction set: x86_64 (default), aarch64, arm, riscv64, riscv32
  -mwindows        PE GUI subsystem (pairs with wWinMain or WinMain)
  -dump-ir         keep the LLVM IR beside the output
  -S, -c           stop before linking: write the AsmPrinter's .s (the C
                   bodies in AT&T syntax) to <file>.s or <source>.s
  -dump-asm        debugging aid: write goa's entry stub instead of linking
  -E               preprocess only
  --help, --version
`

// version is stamped by the build.
var version = "dev"

// optFromSuffix maps an -O flag's suffix to a level.
//
//	1 = -O/-Og/-O1 : the full pass set
//	2 = -Os/-Oz    : size-first, no inlining
//	3 = -O2        : level 1 plus constant-folding enhancements
//	4 = -O3/-Ofast : level 3 plus strength reduction
func optFromSuffix(a string) int {
	switch strings.TrimPrefix(a, "-O") {
	case "", "1", "g":
		return 1
	case "s", "z":
		return 2
	case "2":
		return 3
	case "3", "fast":
		return 4
	}
	return 1
}
