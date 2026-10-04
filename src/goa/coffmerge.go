package goa

// IngestCOFF splices a COFF object -- the output of the LLVM backend's codegen
// -- into this assembler, so BuildPE/BuildELF lay out the result exactly as they
// would for hand-written assembly. Nothing downstream needs to know where the
// bytes came from.
//
// Two jobs:
//
//  1. Sections. A COFF section is appended to the corresponding goa section
//     (.text/.rdata/.data/.bss), aligned so the merge keeps the natural
//     alignment the object assumed. The Win64 unwind sections (.xdata/.pdata)
//     have no goa counterpart -- goa's own path emits none, because the loader
//     registers a synthetic table instead -- but LLVM always emits them, and
//     keeping them means the image stays well-formed for anything that walks the
//     exception chain.
//
//  2. Symbols and relocations. Every defined symbol enters a.syms at its new
//     offset. Every undefined external becomes an import through the same
//     mechanism goa already uses for `extern Name, dll`: the name is registered
//     in a.exts, and references to it are rewritten to the "IAT:name" form that
//     buildIData resolves into an address-table slot.
//
// Relocation arithmetic differs between the two models, and getting it wrong
// silently produces a program that jumps into the middle of nowhere:
//
//   COFF IMAGE_REL_AMD64_REL32    S + A - P, P = address OF the field
//   goa fixup                       target - (base + off + size + ripAdj)
//                                   (P = the address AFTER the field)
//
// So ripAdj = -size makes the two identical. IMAGE_REL_AMD64_ADDR32NB -- the
// form the unwind table uses -- is also S - P but with P at the START of the
// containing section, so ripAdj = -(off+size) is what lines that one up.

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// coffSectionMap routes a COFF section name to the goa section it joins.
var coffSectionMap = map[string]struct {
	writable bool
	code     bool
	bss      bool
}{
	".text":  {false, true, false},
	".rdata": {false, false, false},
	".data":  {true, false, false},
	".bss":   {true, false, true},
	".xdata": {false, false, false},
	".pdata": {false, false, false},
}

// coffAlign is the alignment each section kind wants once merged into an image.
// COFF records per-section alignment only implicitly, so these are the
// conventional values: 16 for code, 8 for read-only and read-write data, 4 for
// .bss and for the unwind tables (whose entries are 4 and 12 bytes).
var coffAlign = map[string]int{
	".text": 16, ".rdata": 8, ".data": 8, ".bss": 4,
	".xdata": 4, ".pdata": 4,
}

// IngestCOFF merges the object at path into the assembler.
func (a *Assembler) IngestCOFF(path string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return a.IngestCOFFBytes(src)
}

// IngestCOFFBytes merges an in-memory object. It is the form the compiler uses,
// which already has the object in hand.
func (a *Assembler) IngestCOFFBytes(src []byte) error {
	o, err := parseCOFF(src)
	if err != nil {
		return err
	}
	return a.ingestParsedCOFF(o, src)
}

// ingestParsedCOFF does the merge for an already-parsed object; src is the raw
// file, needed because relocations are read from it.
func (a *Assembler) ingestParsedCOFF(o *coffObj, src []byte) error {
	// LLVM emits a call to the C runtime's __main module initialiser at the top
	// of every function whose module has global constructors. goc does its own
	// start-up and never calls it, but the reference still has to resolve -- and
	// it has to resolve to a FUNCTION, because the call site will `ret` into
	// whatever this turns out to be.
	//
	// A real one is emitted here: a single `ret`. A `ret` reached through the
	// call pops the caller's own return address, so the program continues
	// exactly as if __main had done nothing -- which is precisely the intent.
	text := a.sectionByName(".text")
	if text == nil {
		text = a.newSection(".text", false, true)
	}
	if _, ok := a.syms[coffNoOpAnchor]; !ok {
		if pad := align(text.cur, 16) - text.cur; pad > 0 {
			padSection(text, pad)
		}
		text.Data = append(text.Data, 0xC3) // ret
		text.cur++
		a.syms[coffNoOpAnchor] = symLoc{sect: sectionIndexOf(a, text), off: text.cur - 1}
	}

	// LLVM lowers a stack frame larger than a page into a call to the C
	// runtime's stack-probe helper, ___chkstk_ms, passing the frame size in
	// rcx. A goc image links no C runtime, so the reference has to resolve
	// here or the link fails with "undefined symbol: ___chkstk_ms".
	//
	// The helper is the same loop the native generator inlines: walk down one
	// page at a time, touching each so the guard page is committed, then give
	// back the overshoot. r11 is used as the counter because the Windows ABI
	// lists rcx as argument-only and r11 as volatile.
	if _, ok := a.syms["___chkstk_ms"]; !ok {
		if pad := align(text.cur, 16) - text.cur; pad > 0 {
			padSection(text, pad)
		}
		start := text.cur
		// mov r11, rcx
		text.Data = append(text.Data, 0x49, 0x89, 0xC8)
		text.cur += 3
		loop := text.cur
		// sub rsp, 4096 ; sub r11, 4096 ; mov rax, [rsp] ; jg loop
		text.Data = append(text.Data,
			0x48, 0x81, 0xEC, 0x00, 0x10, 0x00, 0x00,
			0x49, 0x81, 0xEB, 0x00, 0x10, 0x00, 0x00,
			0x48, 0x8B, 0x04, 0x24,
			0x7F, 0x85)
		text.cur += 19
		rel := int32(loop) - int32(text.cur+4)
		text.Data = append(text.Data, byte(rel), byte(rel>>8), byte(rel>>16), byte(rel>>24))
		text.cur += 4
		// sub rsp, r11 ; ret
		text.Data = append(text.Data, 0x4C, 0x29, 0xDC, 0xC3)
		text.cur += 4
		a.syms["___chkstk_ms"] = symLoc{sect: sectionIndexOf(a, text), off: start}
	}

	// --- sections ---
	// sectOf maps a 1-based COFF section number to the goa section that received
	// its bytes; baseOf records where inside that section they landed.
	sectOf := make([]int, len(o.secs)+1)
	baseOf := make([]int, len(o.secs)+1)
	for i, cs := range o.secs {
		mapped, known := coffSectionMap[cs.name]
		name := cs.name
		if !known {
			// An unfamiliar section (a COMDAT leftover, a CRT chunk). Give it a
			// home instead of dropping data; read-only is the safe assumption.
			mapped.writable = false
		}
		gs := a.sectionByName(name)
		if gs == nil {
			gs = a.newSection(name, mapped.writable, mapped.code)
		}
		if mapped.bss {
			gs.Bss = true
		}
		want := coffAlign[cs.name]
		if want == 0 {
			want = 8
		}
		if pad := align(gs.cur, want) - gs.cur; pad > 0 {
			padSection(gs, pad)
		}
		baseOf[i+1] = gs.cur
		if mapped.bss {
			// An uninitialised section has no file bytes; its size is virtual.
			// Advancing only by len(data) would leave the cursor at zero, the
			// image builder would skip the section entirely, and the symbols in
			// it would resolve to whatever address the NEXT section got -- which
			// is how a .bss counter ends up aliasing the unwind table.
			gs.cur += cs.vsize
		} else {
			if len(cs.data) > 0 {
				gs.Data = append(gs.Data, cs.data...)
				gs.cur += len(cs.data)
			}
		}
		sectOf[i+1] = sectionIndexOf(a, gs)
	}

	// --- symbols ---
	// A symbol the object expects the host to supply becomes an import when it
	// names a Win32 entry point; anything else stays unresolved and is reported
	// at the end, because an image built on a guessed import loads fine and then
	// dies with STATUS_ENTRYPOINT_NOT_FOUND and no explanation.
	var unresolved []string
	for _, s := range o.syms {
		if s.name == "" || s.secNum < 0 {
			// secNum < 0 covers segment symbols (-1 = absolute, e.g. @feat.00)
			// and the file-name record (-2); neither needs an image address.
			continue
		}
		if s.class == scnClassFile || s.secNum > int32(len(o.secs)) {
			continue // file pseudo-symbol, or a section that does not exist
		}
		if s.secNum == 0 {
			if s.class != scnClassExternal {
				continue
			}
			if _, already := a.syms[s.name]; already {
				continue // provided by the assembly we are merging into
			}
			// __main is the module-initialiser stub a C runtime calls before
			// main. goc does its own start-up (the entry stub assembles the
			// argument vector and calls main directly), so nothing ever calls
			// it -- but the object still references it, so it has to resolve to
			// something. An empty body at the current position does the job.
			if s.name == "__main" {
				continue
			}
			// A name the object expects but nobody defines. It becomes an
			// import only if it is a Win32 entry point that really exists;
			// anything else is a link error naming the symbol, because an image
			// built on a guessed import loads fine and then dies with
			// STATUS_ENTRYPOINT_NOT_FOUND and no explanation.
			dll, known := coffImportDLL(s.name)
			if !known {
				unresolved = append(unresolved, s.name)
				continue
			}
			if a.exts == nil {
				a.exts = map[string]string{}
			}
			if _, dup := a.exts[s.name]; !dup {
				// parseExtern normalises the name by appending ".dll", and the
				// loader matches the string exactly -- an import written as
				// "kernel32" instead of "kernel32.dll" names a library that does
				// not exist, and the image fails to load. Two descriptors for
				// what is really one DLL is the visible symptom.
				a.exts[s.name] = dll + ".dll"
			}
			continue
		}
		si := int(s.secNum)
		a.syms[s.name] = symLoc{sect: sectOf[si], off: baseOf[si] + int(s.value)}
	}

	// --- relocations ---
	for i, cs := range o.secs {
		if cs.relCount == 0 {
			continue
		}
		si := i + 1
		for r := 0; r < cs.relCount; r++ {
			rec := cs.relOff + 10*r
			if rec+10 > len(src) {
				return fmt.Errorf("coff: relocation %d of %s out of range", r, cs.name)
			}
			// IMAGE_RELOCATION is {VirtualAddress(4), SymbolTableIndex(4),
			// Type(2)} -- the symbol index sits BETWEEN the offset and the type,
			// not after it. Type is a WORD: reading it as a dword swallows the
			// first two bytes of the next record and yields a nonsense value.
			off := rd32(src, rec)
			symIdx := rd32(src, rec+4)
			typ := rd16(src, rec+8)
			sym, err := o.symbolAt(int(symIdx))
			if err != nil {
				return fmt.Errorf("coff: %s relocation %d: %w", cs.name, r, err)
			}
			if off < 0 || off+4 > len(cs.data) {
				return fmt.Errorf("coff: %s relocation at %d out of range", cs.name, off)
			}
			// An undefined target is satisfied through the address table; a
			// defined one resolves to its own address. __main is the exception:
			// it is undefined in the object but never called by a goc program
			// (the entry stub calls main directly), so it points at the current
			// end of .text -- an address that resolves, is harmless if reached,
			// and keeps the reference from failing the link.
			// An undefined symbol is not automatically an import: the assembly
			// half may already define it. That is the normal case here, because
			// goa's generator emits the globals and the C runtime while LLVM
			// only references them. Checking the symbol table first is what
			// keeps a global from turning into a load from an import slot.
			key := sym.name
			if sym.secNum == 0 {
				switch {
				case sym.name == "__main":
					key = coffNoOpAnchor
				case a.definesSymbol(sym.name):
					key = sym.name
				default:
					key = "IAT:" + sym.name
				}
			}
			// The relocation's offset is relative to the start of ITS OWN COFF
			// section, but the fixup has to name a position inside the goa
			// section the bytes were merged into -- which the object does not
			// start at, because anything already assembled (the entry stub) and
			// the alignment padding come first. Forgetting the bias writes every
			// patch a few dozen bytes early, which corrupts unrelated code
			// instead of failing loudly.
			at := baseOf[si] + off
			switch typ {
			case relAMD64Abs:
				// Padding entry: nothing to patch.
			case relAMD64Rel32:
				// IMAGE_REL_AMD64_REL32 is S + A - P, but Microsoft's PE
				// specification defines P for this type as the address OF THE
				// FIELD while the hardware RIP after executing the instruction is
				// the field's END -- so a literal reading of "S - P" lands four
				// bytes past the target. ripAdj = 0 makes goa's
				// "target - (base + off + size)" match what the CPU computes.
				a.fixups = append(a.fixups, Fixup{
					sect: sectOf[si], off: at, sym: key,
				})
			case relAMD64Addr32NB:
				// A relocation against a *section* symbol is how the Win64 unwind
				// tables address code: the symbol names the section and the
				// position within it lives in the field's existing contents, not
				// in the symbol (whose value is 0 for a section symbol). COFF
				// relocations overwrite the field, so that addend has to be read
				// out first and folded back in, or every RUNTIME_FUNCTION entry
				// ends up pointing at the section start -- which the loader then
				// rejects, and the program dies with STATUS_PRIVILEGED_INSTRUCTION
				// the first time an exception tries to walk a frame.
				//
				// The value wanted is the target's absolute RVA, so P (the field's
				// own address) drops out entirely: ripAdj = -(at+4) cancels both.
				// The stored value must be the target's ABSOLUTE RVA, so this is
				// an absolute fixup: the usual "target minus where the field
				// sits" arithmetic would store the target's offset within its own
				// section minus this section's RVA, a large negative number the
				// loader rejects -- and the program then faults with
				// STATUS_PRIVILEGED_INSTRUCTION the first time an exception tries
				// to walk a frame.
				//
				// The addend is the field's own unrelocated contents in the
				// section being patched: for a section-symbol relocation that is
				// the offset within .text the entry needs, read out of THIS
				// section at the relocation's offset. (Reading it from the target
				// section instead yields whatever bytes sit at that offset there.)
				addend := 0
				if off+4 <= len(cs.data) {
					addend = rd32(cs.data, off)
				}
				a.fixups = append(a.fixups, Fixup{
					sect: sectOf[si], off: at, sym: key,
					absolute: true, addend: addend,
				})
			case relAMD64Addr64:
				// A 64-bit absolute address: a pointer-sized slot holding the
				// target's image address, which is how a 64-bit global is
				// reached. It stores an address rather than a distance, so it
				// goes through the same absolute path as ADDR32NB.
				a.fixups = append(a.fixups, Fixup{
					sect: sectOf[si], off: at, sym: key,
					absolute: true, wide: true, virtual: true,
				})
			default:
				return fmt.Errorf("coff: unknown relocation type %d for %s (%s)",
					typ, cs.name, sym.name)
			}
		}
	}
	// Report undefined names only after the whole object has been read, so one
	// pass names everything that is missing instead of stopping at the first.
	if len(unresolved) > 0 {
		sort.Strings(unresolved)
		return fmt.Errorf("coff: undefined symbol(s): %s", strings.Join(unresolved, ", "))
	}
	return nil
}

// symbolAt returns the syms[i] entry. The symbol table is stored in raw order,
// aux records included, so a relocation's symbol index addresses it directly.
func (o *coffObj) symbolAt(i int) (coffSym, error) {
	if i < 0 || i >= len(o.syms) {
		return coffSym{}, fmt.Errorf("symbol index %d out of range (%d symbols)", i, len(o.syms))
	}
	return o.syms[i], nil
}

// definesSymbol reports whether the assembler half already provides this name.
// A symbol the object leaves undefined is satisfied by the assembly when one is
// there, and becomes an import only when nothing else defines it.
func (a *Assembler) definesSymbol(name string) bool {
	if _, ok := a.syms[name]; ok {
		return true
	}
	_, imported := a.exts[name]
	return imported
}

// padSection advances a section by n bytes, appending zeros for a normal
// section and only moving the cursor for .bss (which has no file bytes).
func padSection(s *Section, n int) {
	if n <= 0 {
		return
	}
	if s.Bss {
		s.cur += n
		return
	}
	s.Data = append(s.Data, make([]byte, n)...)
	s.cur += n
}

// sectionIndexOf returns the assembler's index of s, or -1.
func sectionIndexOf(a *Assembler, s *Section) int {
	for i, x := range a.sections {
		if x == s {
			return i
		}
	}
	return -1
}

// coffWin32DLLs maps the Win32 API entry points this linker is willing to turn
// into imports, each to the library that actually exports it.
//
// The list is deliberately closed. A COFF object leaves every symbol it does not
// define undefined, and those fall into three groups: the program's own runtime
// (compiled into the object), genuinely external Win32 calls, and -- when the
// object was produced from LLVM IR that declared a helper but never defined it
// -- names that simply do not exist anywhere. Guessing "kernel32" for all of
// them produces an image that builds cleanly and then dies at load time with
// STATUS_ENTRYPOINT_NOT_FOUND (0xC0000139), which says nothing about the cause.
// An unknown name is therefore reported as the link error it is.
var coffWin32DLLs = map[string]string{
	// kernel32 -- process, file, memory and console basics.
	"ExitProcess": "kernel32", "GetCommandLineA": "kernel32", "GetCommandLineW": "kernel32",
	"GetModuleHandleA": "kernel32", "GetModuleHandleW": "kernel32", "GetProcAddress": "kernel32",
	"GetStdHandle": "kernel32", "WriteFile": "kernel32", "ReadFile": "kernel32",
	"GetLastError": "kernel32", "SetLastError": "kernel32", "GetTickCount64": "kernel32",
	"QueryPerformanceCounter": "kernel32", "QueryPerformanceFrequency": "kernel32",
	"GetCurrentProcess": "kernel32", "GetCurrentProcessId": "kernel32",
	"GetCurrentThreadId": "kernel32", "Sleep": "kernel32", "GetVersionExA": "kernel32",
	"VirtualAlloc": "kernel32", "VirtualFree": "kernel32", "HeapAlloc": "kernel32",
	"HeapFree": "kernel32", "GetProcessHeap": "kernel32", "IsDebuggerPresent": "kernel32",
	"SetUnhandledExceptionFilter": "kernel32", "UnhandledExceptionFilter": "kernel32",
	"TerminateProcess": "kernel32", "CreateFileA": "kernel32", "CreateFileW": "kernel32",
	"CloseHandle": "kernel32", "GetFileSize": "kernel32", "SetFilePointer": "kernel32",
	"CreateThread": "kernel32", "ResumeThread": "kernel32", "WaitForSingleObject": "kernel32",
	"GetSystemInfo": "kernel32", "GetSystemTimeAsFileTime": "kernel32",
	"InitializeCriticalSection": "kernel32", "EnterCriticalSection": "kernel32",
	"LeaveCriticalSection": "kernel32", "DeleteCriticalSection": "kernel32",
	"TlsAlloc": "kernel32", "TlsGetValue": "kernel32", "TlsSetValue": "kernel32",
	"DeleteFileA": "kernel32", "DeleteFileW": "kernel32", "MoveFileA": "kernel32",
	"GetFileAttributesA": "kernel32", "CreateDirectoryA": "kernel32",
	"RemoveDirectoryA": "kernel32", "GetSystemDirectoryA": "kernel32",
	"GetTempPathA": "kernel32", "GetLocalTime": "kernel32", "GetSystemTime": "kernel32",
	"RtlUnwind": "kernel32",
	// Process creation and query, directory enumeration, environment and
	// attribute access: reached through the whole-program LLVM module, which
	// carries every goclib function the program touches, so these appear even
	// when the program's own source never names them.
	"CreateProcessA": "kernel32", "GetExitCodeProcess": "kernel32",
	"FindFirstFileA": "kernel32", "FindNextFileA": "kernel32", "FindClose": "kernel32",
	"GetFileAttributesExA": "kernel32", "SetFileAttributesA": "kernel32",
	"GetCurrentDirectoryA": "kernel32", "GetEnvironmentVariableA": "kernel32",
	"GetTickCount": "kernel32", "HeapReAlloc": "kernel32",
}

// findStubReturn locates the instruction that follows the entry stub's call to
// main: the return address main would have on the stack. It recognises the
// `call main` / `mov <reg>, rax` / `call <exit>` shape the stub always has, and
// returns a negative value when the text does not look like that (an object
// linked without a stub, say).
func (a *Assembler) findStubReturn() symLoc {
	text := a.sectionByName(".text")
	if text == nil {
		return symLoc{sect: -1}
	}
	callEntry, ok := a.syms[a.entry]
	if a.entry == "" || !ok || a.sections[callEntry.sect] != text {
		return symLoc{sect: -1}
	}
	// The call is E8 rel32, so the instruction after it sits five bytes on.
	ret := callEntry.off + 5
	if ret >= len(text.Data) {
		return symLoc{sect: -1}
	}
	return symLoc{sect: sectionIndexOf(a, text), off: ret}
}

// coffNoOpAnchor is the synthetic symbol undefined references that must resolve
// without becoming imports point at. LLVM emits a call to the C runtime's
// __main module initialiser in every object; a goc program never makes that call
// (its entry stub runs main directly), but the reference still has to resolve.
const coffNoOpAnchor = "__goc_coff_anchor"

// coffImportDLL returns the library that exports name, and whether the name is a
// Win32 API this linker knows about. An unknown name is not guessed at: the
// caller turns it into a link error naming the symbol, because importing a name
// no DLL exports yields an image that fails to load with no diagnostic.
func coffImportDLL(name string) (string, bool) {
	dll, ok := coffWin32DLLs[name]
	return dll, ok
}
