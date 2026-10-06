package gocld

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
//  2. Symbols and relocations. Every defined symbol enters img.Syms at its new
//     offset. Every undefined external becomes an import through the same
//     mechanism goa already uses for `extern Name, dll`: the name is registered
//     in img.Exts, and references to it are rewritten to the "IAT:name" form that
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
func (img *Image) IngestCOFF(path string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return img.IngestCOFFBytes(src)
}

// IngestCOFFBytes merges an in-memory object. It is the form the compiler uses,
// which already has the object in hand.
func (img *Image) IngestCOFFBytes(src []byte) error {
	o, err := parseCOFF(src)
	if err != nil {
		return err
	}
	return img.ingestParsedCOFF(o, src)
}

// refptrLoc is where an object's .refptr slot for an undefined data symbol
// ended up: which merged section holds it, and at what offset.
type refptrLoc struct {
	sect int
	off  int
}

// ingestParsedCOFF does the merge for an already-parsed object; src is the raw
// file, needed because relocations are read from it.
func (img *Image) ingestParsedCOFF(o *coffObj, src []byte) error {
	// LLVM emits a call to the C runtime's __main module initialiser at the top
	// of every function whose module has global constructors. goc does its own
	// start-up and never calls it, but the reference still has to resolve -- and
	// it has to resolve to a FUNCTION, because the call site will `ret` into
	// whatever this turns out to be.
	//
	// A real one is emitted here: a single `ret`. A `ret` reached through the
	// call pops the caller's own return address, so the program continues
	// exactly as if __main had done nothing -- which is precisely the intent.
	text := sectionByName(img, ".text")
	if text == nil {
		text = newSection(img, ".text", false, true)
	}
	if _, ok := img.Syms[coffNoOpAnchor]; !ok {
		if pad := align(text.VSize, 16) - text.VSize; pad > 0 {
			padSection(text, pad)
		}
		text.Data = append(text.Data, 0xC3) // ret
		text.VSize++
		img.Syms[coffNoOpAnchor] = SymLoc{Sect: sectionIndexOf(img, text), Off: text.VSize - 1}
	}

	// LLVM lowers a stack frame larger than a page into a call to the C
	// runtime's stack-probe helper, ___chkstk_ms, with the frame size in
	// RAX. A goc image links no C runtime, so the reference has to resolve
	// here or the link fails with "undefined symbol: ___chkstk_ms".
	//
	// LLVM's Win64 large-frame prologue is "mov eax,size; call ___chkstk_ms;
	// sub rsp,rax": the helper must only touch the guard pages (probe) and
	// leave both rsp and rax intact -- rsp for the caller's own sub rsp,rax,
	// rax because it still holds the frame size for that sub. The earlier
	// helpers either read rcx (which the caller never sets, so the probe
	// walked the stack for a garbage size) or subtracted pages themselves
	// (double allocation), both ending in STATUS_STACK_OVERFLOW (0xC00000FD).
	// r10/r11 are volatile per the Windows ABI, so they are free scratch.
	if _, ok := img.Syms["___chkstk_ms"]; !ok {
		if pad := align(text.VSize, 16) - text.VSize; pad > 0 {
			padSection(text, pad)
		}
		start := text.VSize
		// mov r11, rax ; mov r10, rsp
		// (4C 8B D4 = mov r10,rsp; the 89 variant would be mov rsp,r10 and
		// clobber the stack pointer with garbage r10 on entry.)
		text.Data = append(text.Data, 0x49, 0x89, 0xC3, 0x4C, 0x8B, 0xD4)
		text.VSize += 6
		loop := text.VSize
		// loop: sub rsp,4096 ; mov [rsp],r11 ; sub r11,4096
		// The probe block is 18 bytes (7 + 4 + 7); the cursor must advance by
		// exactly the bytes appended or every symbol merged from the COFF
		// object lands early -- the entry stub's call to main then jumps at the
		// alignment byte in front of the function and faults immediately.
		text.Data = append(text.Data,
			0x48, 0x81, 0xEC, 0x00, 0x10, 0x00, 0x00, // sub rsp, 4096
			0x4C, 0x89, 0x1C, 0x24, // mov [rsp], r11  (touch the page)
			0x49, 0x81, 0xEB, 0x00, 0x10, 0x00, 0x00) // sub r11, 4096
		text.VSize += 18
		// jg loop: short jump back to the probe block. The offset is relative
		// to the instruction's end; it must be computed, not hard-coded -- a
		// literal offset would land the jump outside the probe block.
		rel8 := int8(loop - (text.VSize + 2))
		text.Data = append(text.Data, 0x7F, byte(rel8))
		text.VSize += 2
		// mov rsp, r10 ; ret
		text.Data = append(text.Data, 0x4C, 0x89, 0xD4, 0xC3)
		text.VSize += 4
		img.Syms["___chkstk_ms"] = SymLoc{Sect: sectionIndexOf(img, text), Off: start}
	}

	// --- sections ---
	// refptrFor records, for each undefined data symbol, the eight-byte slot
	// the object put in .rdata to hold its address. See the .refptr note below.
	refptrFor := map[string]refptrLoc{}

	// sectOf maps a 1-based COFF section number to the goa section that received
	// its bytes; baseOf records where inside that section they landed.
	sectOf := make([]int, len(o.secs)+1)
	baseOf := make([]int, len(o.secs)+1)
	for i, cs := range o.secs {
		mapped, known := coffSectionMap[cs.name]
		name := cs.name
		if !known {
			// A `.refptr` fragment belongs to .rdata. Win64 COFF puts a
			// reference to undefined data in its own eight-byte section named
			// after the symbol, because an object file has no data relocation
			// and a RIP-relative displacement needs an address to point at.
			// Giving the fragment a section of its own would leave the image
			// without it -- the image builder emits the known sections -- and
			// every reference through it would read zeroes.
			if k := strings.LastIndex(cs.name, "$.refptr."); k >= 0 {
				name = ".rdata"
			}
		}
		if name != cs.name {
			mapped = coffSectionMap[".rdata"]
			known = true
		}
		if !known {
			// An unfamiliar section (a COMDAT leftover, a CRT chunk). Give it a
			// home instead of dropping data; read-only is the safe assumption.
			mapped.writable = false
		}
		gs := sectionByName(img, name)
		if gs == nil {
			gs = newSection(img, name, mapped.writable, mapped.code)
		}
		if mapped.bss {
			gs.Bss = true
		}
		// The unwind sections are merged all the same -- their symbols resolve
		// and their relocations are applied, so nothing is left dangling -- but
		// the bytes do not go into the image. See Section.Unmapped for why that
		// is a size decision rather than a correctness one.
		if name == ".xdata" || name == ".pdata" {
			gs.Unmapped = true
		}
		want := coffAlign[cs.name]
		if want == 0 {
			want = 8
		}
		if pad := align(gs.VSize, want) - gs.VSize; pad > 0 {
			padSection(gs, pad)
		}
		// Win64 COFF has no data relocation: a reference to undefined data
		// cannot be a RIP-relative displacement to the symbol, because there
		// is no address to point at yet. LLVM's answer is a pointer slot in a
		// section named `.rdata$.refptr.<name>`, holding the address once the
		// host supplies the storage. The slot is filled in by the relocation
		// pass below; what matters here is remembering which section holds it,
		// so a reference to the undefined symbol can be resolved to the slot.
		if k := strings.LastIndex(cs.name, "$.refptr."); k >= 0 {
			refptrFor[cs.name[k+len("$.refptr."):]] = refptrLoc{
				sect: sectionIndexOf(img, gs), off: gs.VSize,
			}
		}
		baseOf[i+1] = gs.VSize
		if mapped.bss {
			// An uninitialised section has no file bytes; its size is virtual.
			// Advancing only by len(data) would leave the cursor at zero, the
			// image builder would skip the section entirely, and the symbols in
			// it would resolve to whatever address the NEXT section got -- which
			// is how a .bss counter ends up aliasing the unwind table.
			gs.VSize += cs.vsize
		} else {
			if len(cs.data) > 0 {
				gs.Data = append(gs.Data, cs.data...)
				gs.VSize += len(cs.data)
			}
		}
		sectOf[i+1] = sectionIndexOf(img, gs)
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
			if _, already := img.Syms[s.name]; already {
				continue // provided by the assembly we are merging into
			}
			// A data symbol the object declared but did not define, and for
			// which it emitted a .refptr slot. Every reference the object made
			// to this name is really a reference to that slot, so resolving the
			// name to the slot is what makes the addresses come out right: the
			// slot holds the address, and the host writes the address there.
			//
			// Without this the name resolves to nothing, and a RIP-relative
			// reference to it encodes a displacement from wherever the section
			// happens to start -- a valid instruction that reads the wrong
			// bytes, which is why the failure is a crash rather than a link
			// error.
			if rp, ok := refptrFor[s.name]; ok {
				img.Syms[s.name] = SymLoc{Sect: rp.sect, Off: rp.off}
				continue
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
			// The assembly's own `extern Name, dll` declarations are authoritative:
			// they name the exact library, so an undefined object symbol the stub
			// already imported needs no further work. This is how a program that
			// calls a Win32 function goa's closed table does not list -- a user32
			// MessageBoxA, say -- reaches the right DLL: gocl's externalImports
			// wrote the `extern` line, and we honour it instead of guessing (and
			// then failing with "undefined symbol"). The closed table stays as a
			// fallback for symbols the runtime reaches without an explicit extern.
			if img.Exts != nil {
				if _, declared := img.Exts[s.name]; declared {
					continue
				}
			}
			dll, known := coffImportDLL(s.name)
			if !known {
				unresolved = append(unresolved, s.name)
				continue
			}
			if img.Exts == nil {
				img.Exts = map[string]string{}
			}
			if _, dup := img.Exts[s.name]; !dup {
				// parseExtern normalises the name by appending ".dll", and the
				// loader matches the string exactly -- an import written as
				// "kernel32" instead of "kernel32.dll" names a library that does
				// not exist, and the image fails to load. Two descriptors for
				// what is really one DLL is the visible symptom.
				img.Exts[s.name] = dll + ".dll"
			}
			continue
		}
		si := int(s.secNum)
		img.Syms[s.name] = SymLoc{Sect: sectOf[si], Off: baseOf[si] + int(s.value)}
	}

	// --- import thunks ---
	// The object's relative calls and jumps to imported functions (REL32
	// against an undefined symbol) cannot target the IAT slot directly: the
	// slot holds the function's address as DATA, and executing those bytes
	// faults. Emit one jump thunk per import -- jmp [rip+rel32] to the IAT
	// slot -- and route call/jmp fixups through it, the same shape MSVC links.
	// Data references (lea/mov RIP-relative) keep naming the IAT slot, which
	// is the correct address-of semantics. Generating a thunk for every
	// import is a few bytes each and keeps the code simple.
	if len(img.Exts) > 0 {
		for name := range img.Exts {
			if _, ok := img.Syms["thunk:"+name]; ok {
				continue
			}
			off := text.VSize
			// FF 25 <rel32>: jmp qword ptr [rip+disp]
			text.Data = append(text.Data, 0xFF, 0x25, 0, 0, 0, 0)
			text.VSize += 6
			img.Fixups = append(img.Fixups, Fixup{
				Sect: sectionIndexOf(img, text), Off: off + 2, Sym: "IAT:" + name,
			})
			img.Syms["thunk:"+name] = SymLoc{Sect: sectionIndexOf(img, text), Off: off}
		}
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
				case img.definesSymbol(sym.name):
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
				//
				// A is NOT optional here, and dropping it is silent corruption
				// rather than a visible error. COFF relocation records have no
				// addend field: A lives in the field's own unrelocated bytes,
				// so it has to be read out of the section before the patch
				// overwrites it. LLVM uses that for every RIP-relative access
				// to a member of a global -- "mov %rax, stdout_file+32(%rip)"
				// carries A=32, "movq $1, stdin_file+8(%rip)" carries A=4 (the
				// trailing imm32 is already folded in) -- and calls carry A=0.
				// Leaving A out made every such store land on the symbol's base
				// instead of the member: the stdio initialiser then wrote
				// _writable/_base/_size/_off straight over _fd, so a FILE came
				// up with _writable == 0 and every stdio write returned an
				// error with nothing on the console -- while file I/O, which
				// goes through a heap FILE set up field by field, kept working.
				//
				// A REL32 field whose target is an import is normally a call or
				// jmp (opcode E8/E9) whose target must be CODE; pointing it at
				// the IAT slot would execute address-table bytes and fault.
				// Route those through the per-import jump thunk emitted above.
				// Any other opcode is a data reference (lea/mov RIP-relative),
				// which legitimately names the slot itself. The import check
				// goes through img.Exts rather than the key's prefix: an import
				// that already satisfied a defining reference lands here under
				// its bare name (definesSymbol), not as "IAT:...".
				if off > 0 && off <= len(cs.data) {
					if op := cs.data[off-1]; op == 0xE8 || op == 0xE9 {
						if _, isImport := img.Exts[sym.name]; isImport {
							key = "thunk:" + sym.name
						}
					}
				}
				addend := rd32(cs.data, off)
				img.Fixups = append(img.Fixups, Fixup{
					Sect: sectOf[si], Off: at, Sym: key, Addend: addend,
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
				img.Fixups = append(img.Fixups, Fixup{
					Sect: sectOf[si], Off: at, Sym: key,
					Absolute: true, Addend: addend,
				})
			case relAMD64Addr64:
				// A 64-bit absolute address: a pointer-sized slot holding the
				// target's image address, which is how a 64-bit global is
				// reached. It stores an address rather than a distance, so it
				// goes through the same absolute path as ADDR32NB.
				img.Fixups = append(img.Fixups, Fixup{
					Sect: sectOf[si], Off: at, Sym: key,
					Absolute: true, Wide: true, Virtual: true,
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
func (img *Image) definesSymbol(name string) bool {
	if _, ok := img.Syms[name]; ok {
		return true
	}
	_, imported := img.Exts[name]
	return imported
}

// padSection advances a section by n bytes, appending zeros for a normal
// section and only moving the cursor for .bss (which has no file bytes).
func padSection(s *Section, n int) {
	if n <= 0 {
		return
	}
	if s.Bss {
		s.VSize += n
		return
	}
	s.Data = append(s.Data, make([]byte, n)...)
	s.VSize += n
}

// sectionIndexOf returns the assembler's index of s, or -1.
func sectionIndexOf(img *Image, s *Section) int {
	for i, x := range img.Sections {
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
func (img *Image) findStubReturn() SymLoc {
	text := sectionByName(img, ".text")
	if text == nil {
		return SymLoc{Sect: -1}
	}
	callEntry, ok := img.Syms[img.Entry]
	if img.Entry == "" || !ok || img.Sections[callEntry.Sect] != text {
		return SymLoc{Sect: -1}
	}
	// The call is E8 rel32, so the instruction after it sits five bytes on.
	ret := callEntry.Off + 5
	if ret >= len(text.Data) {
		return SymLoc{Sect: -1}
	}
	return SymLoc{Sect: sectionIndexOf(img, text), Off: ret}
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
