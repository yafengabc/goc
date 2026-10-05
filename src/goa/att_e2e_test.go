package goa

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The .s files under testdata/att are genuine LLVM AsmPrinter dumps of goc's
// own example programs -- the exact input this front end has to eat in
// production. They are what makes the tests worth having: a hand-picked list of
// instructions would never have surfaced the operand-order rule for
// `addq %r14, 64(%r15)`, nor the fact that `.str.0` is file-scoped while
// `.LBB0_3` is not, nor that a memory operand needs the mnemonic's size suffix
// to know whether it is 32 or 64 bits wide.
//
// Only a handful are checked in, to keep the repository small;
// tools/gen-att-samples.sh regenerates the full corpus (one file per example in
// src/examples, ~70 of them) and is what to run when changing the front end.
// Both the unit tests and the breadth test below skip cleanly if the directory
// is empty, so a tree without the samples still builds.

// TestATTRealLLVMOutputParses is the breadth check: the whole corpus must
// assemble cleanly. A single regression here is a real gap, so the test fails
// on the first one rather than counting.
func TestATTRealLLVMOutputParses(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "att", "*.s"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(files) == 0 {
		t.Skip("no AT&T samples -- run: bash tools/gen-att-samples.sh")
	}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		a := NewAssembler()
		if err := attAssemble(a, string(src)); err != nil {
			t.Errorf("%s: %v", filepath.Base(f), err)
			continue
		}
		// A parsed file must have actually produced code -- an assembler that
		// silently dropped every instruction would sail through the check
		// above while emitting an empty image.
		if text := a.sectionByName(".text"); text == nil || len(text.Data) == 0 {
			t.Errorf("%s: parsed but emitted no code", filepath.Base(f))
		}
	}
}

// The entry stub mirrors what goc's codegen emits: capture the entry stack,
// align it, call main, hand the result to ExitProcess. `_start` is the PE
// entry point; `global` names it.
//
// The body is written in AT&T because the whole buffer goes through the AT&T
// front end. A goa-syntax stub would have its operands reversed on the way in,
// turning `lea r13, [rsp+8]` into `lea [rsp+8], r13` -- and an lea with a
// memory destination has no encoding at all, so the failure would surface as a
// confusing encoder error rather than as "you wrote the wrong dialect".
const attEntryStub = `
extern ExitProcess, kernel32
global _start
section .text
_start:
	movq	(%rsp), %r12
	leaq	8(%rsp), %r13
	andq	$-16, %rsp
	subq	$48, %rsp
	call	main
	movq	%rax, %rcx
	call	ExitProcess
`

// TestATTRealLLVMOutputLinksWithExterns is the narrow half: a whole
// LLVM-generated translation unit must link into a runnable PE, once the two
// things it is not expected to contain are supplied -- the Win32 entry points
// it calls, and the globals the C front end owns. That is what proves the front
// end's symbols, sections and relocations line up; a mis-scaled displacement or
// a mis-qualified label shows up here as a link failure even when parsing was
// clean.
//
// Neither half of the supply is hard-coded. The imports come from the goclib
// headers, so a symbol cannot drift onto the wrong DLL without the test
// noticing, and the globals are discovered from the file. A fixed list would
// rot: the first example to use a new API or a new global would report a link
// failure that says nothing about the front end, and that failure is easy to
// misread as a regression.
func TestATTRealLLVMOutputLinksWithExterns(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "att", "*.s"))
	if err != nil || len(files) == 0 {
		t.Skip("no AT&T samples -- run: bash tools/gen-att-samples.sh")
	}
	var linked int
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		var b strings.Builder
		// A translation unit is not a program: the Win32 entry points it
		// calls and the globals it was handed have to come from outside. Which
		// ones is read out of the file rather than hard-coded, because a fixed
		// list silently stops covering a newly-exercised example -- the test
		// would report a link failure that has nothing to do with the front end.
		for sym, dll := range attUndeclaredExterns(src) {
			b.WriteString("extern " + sym + ", " + dll + "\n")
		}
		// LLVM's .s has no entry point; the stub supplies one, mirroring what
		// goc's own codegen synthesises.
		b.WriteString(attEntryStub)
		b.Write(src)
		// Globals belong to the C front end, not to LLVM: for a C global `x`
		// the .s refers to `G_x` and never defines it, and what it does define
		// is `.refptr.G_x`, an eight-byte slot holding the address. This is the
		// split the LLVM back end is built on (see moduleBuilder.noteExternGlobal
		// and its "G_" + name), so the test has to supply the other half --
		// otherwise a perfectly correct translation unit "fails to link" over
		// something it was never supposed to define.
		for _, g := range attExternGlobals(src) {
			b.WriteString(g)
		}
		out := filepath.Join(t.TempDir(), "out.exe")
		if _, err := AssembleATT(b.String(), out, false); err != nil {
			t.Errorf("%s: link: %v", filepath.Base(f), err)
			continue
		}
		fi, err := os.Stat(out)
		if err != nil || fi.Size() == 0 {
			t.Errorf("%s: no image written (err=%v)", filepath.Base(f), err)
			continue
		}
		linked++
	}
	t.Logf("linked %d files", linked)
	if linked == 0 {
		t.Fatal("nothing linked at all -- the pipeline is broken")
	}
}

// attUndeclaredExterns returns the symbols a translation unit calls but does
// not define -- the imports the real link would have to satisfy.
//
// It is derived from the file rather than listed, because a hand-kept list
// drifts: the moment an example starts calling a new Win32 entry point the
// test would report a link failure that says nothing about the front end, and
// the failure would be easy to misread as a regression. A symbol counts as
// defined if the file carries a label for it, which covers both plain labels
// and the `.globl`-declared dot-labels LLVM emits.
//
// Everything found is bound to kernel32. That is right for the Win32 entry
// points these files use, and a symbol from another library would need the
// caller's real binding -- which is exactly what goc's C front end supplies in
// production.
func attUndeclaredExterns(src []byte) map[string]string {
	defined := map[string]bool{}
	referenced := map[string]bool{}
	for _, raw := range strings.Split(string(src), "\n") {
		line := strings.TrimSpace(stripATTComment(raw))
		if line == "" {
			continue
		}
		// `name:` and `.globl name` both define; a label can sit on the same
		// line as the instruction it labels.
		if label, _, ok := splitLabel(line); ok && label != "" {
			defined[label] = true
			defined[attStripCall(label)] = true
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && (fields[0] == ".globl" || fields[0] == ".global") {
			defined[fields[1]] = true
		}
		// Any branch out of the unit needs a resolved target, not just a call:
		// a tail call is emitted as `jmp name` with no call at all, and
		// missing those would leave the import table short.
		if len(fields) < 2 {
			continue
		}
		switch attStripSuffix(fields[0]) {
		case "call", "jmp", "bnd":
			tgt := attStripCall(fields[len(fields)-1])
			// `callq *%rax` is an indirect branch through a function pointer.
			if tgt == "" || strings.HasPrefix(tgt, "*") || strings.HasPrefix(tgt, "%") {
				continue
			}
			referenced[tgt] = true
		}
	}
	bindings := attWin32Bindings()
	out := map[string]string{}
	for name := range referenced {
		if defined[name] || strings.HasPrefix(name, ".") {
			continue
		}
		if dll, ok := bindings[name]; ok {
			out[name] = dll
			continue
		}
		// Not a Windows entry point: it is a goclib function that the C
		// front end provides, which is outside what this test supplies.
		out[name] = "kernel32"
	}
	return out
}

// attWin32Bindings maps the Windows entry points goclib declares onto the DLL
// each one lives in. Only the ones that are *not* kernel32 need to be here --
// kernel32 is the default -- and getting the list from the headers rather than
// maintaining it by hand keeps the two in step: a symbol moved to user32 stops
// linking against the wrong DLL instead of failing obscurely at load time.
func attWin32Bindings() map[string]string {
	bindings := map[string]string{}
	// The headers are source, not a build product, so they are always present
	// next to this module.
	glob, err := filepath.Glob(filepath.Join("..", "goclib", "*.h"))
	if err != nil {
		return bindings
	}
	for _, h := range glob {
		b, err := os.ReadFile(h)
		if err != nil {
			continue
		}
		for _, m := range win32ExternRe.FindAllStringSubmatch(string(b), -1) {
			bindings[m[1]] = m[2]
		}
	}
	return bindings
}

// win32ExternRe matches a goclib extern declaration and captures the symbol
// name and the DLL it is bound to: `extern LPVOID HeapReAlloc(...), kernel32;`
var win32ExternRe = regexp.MustCompile(
	`extern\s+[A-Za-z_][A-Za-z0-9_ *]*?\b([A-Za-z_][A-Za-z0-9_]*)\s*\([^;]*\),\s*([A-Za-z0-9_]+)\s*;`)

// attStripCall removes the indirect-branch marker and a size suffix, so
// `*__goclib_write` and `__goclib_writeq` both name `__goclib_write`.
func attStripCall(s string) string {
	return attStripSuffix(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(s), "*"), "%"))
}

// attStripSuffix removes a trailing GAS operand-size letter: `callq` -> `call`,
// `__goclib_writeq` -> `__goclib_write`.
//
// It peels exactly one letter, and only when the stem is a known mnemonic.
// Peeling repeatedly would eat the mnemonic's own letters -- `callq` would
// lose its `q` and then its `l` and become `ca` -- while peeling a symbol has
// to keep going for names like `movslq`. The known-mnemonic check is what lets
// one function serve both: a symbol is never in that table, so it is peeled
// once, and `WriteConsoleA` keeps its `A` because `WriteConsole` is not a
// mnemonic either.
func attStripSuffix(mnem string) string {
	if len(mnem) < 2 {
		return mnem
	}
	last := mnem[len(mnem)-1]
	switch last {
	case 'q', 'l', 'w', 'b':
	default:
		return mnem
	}
	stem := mnem[:len(mnem)-1]
	// A symbol is not a mnemonic, so this test is really "is this an
	// instruction?" -- and it is the only safe discriminator. A name that
	// merely *ends* in a size letter must not be cut: ShowWindow would become
	// ShowWindo and WriteConsoleA would become WriteConsole, neither of which
	// exists, and the import would silently point at the wrong symbol or at
	// nothing at all.
	if attMnemonics[stem] || attPrefixedMnemonic(stem) {
		return stem
	}
	return mnem
}

// attExternGlobals returns definitions for the globals a translation unit
// refers to but does not define.
//
// LLVM is handed functions, not storage: goc's C front end emits every global
// under a "G_" prefix and tells LLVM about it as an external (see
// translateProgram's claimed set). So an LLVM .s legitimately contains
//
//	.long	.LCPI0_3
//	.quad	G_count          <- a pointer to a global defined elsewhere
//
// and never the storage itself. A link that expected the .s to be
// self-contained would report every one of those as undefined, which says
// nothing about the front end.
//
// The definition emitted here is deliberately the weakest one that links --
// eight zero bytes -- because what is under test is the *relocation*, not the
// initialiser: a wrong pointer is caught by the jump-table and call tests, and
// a real initialiser belongs to the front end that owns the variable.
func attExternGlobals(src []byte) []string {
	seen := map[string]bool{}
	for _, raw := range strings.Split(string(src), "\n") {
		line := strings.TrimSpace(stripATTComment(raw))
		// A `.quad G_x` slot is the reference; `.refptr.G_x` is the label
		// LLVM gives the slot itself, and the two together are the whole
		// pattern.
		if !strings.HasPrefix(line, ".quad") {
			continue
		}
		target := strings.TrimSpace(strings.TrimPrefix(line, ".quad"))
		if !strings.HasPrefix(target, "G_") || seen[target] {
			continue
		}
		// Thread-local globals are reached through the OS-provided index
		// rather than directly, and goa reserves that name itself; defining
		// it here would collide.
		if target == "G_goc_tls_index" {
			continue
		}
		seen[target] = true
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)

	var out []string
	for _, n := range names {
		out = append(out, "\nsection .bss\n"+n+":\n\t.zero 8\n")
	}
	return out
}
