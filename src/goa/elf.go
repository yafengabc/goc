package goa

import (
	"fmt"
	"os"
	"sort"
)

// ---------------------------------------------------------------------------
// ELF64 writer -- static Linux executables, no libc, no dynamic linker.
// ---------------------------------------------------------------------------
//
// Image layout (one PT_LOAD covering the whole file image at 0x400000):
//
//	0x00  ELF header (64 bytes) + one program header (56 bytes)
//	0x80  .text  -> vaddr elfBase + 0x80
//	      .rdata
//	      .data
//	      section headers + .shstrtab   (not part of the loadable image)
//
// A single R+W+X segment is what hand-written ELF loaders normally use: the
// kernel only requires p_vaddr ≡ p_offset (mod p_align). Splitting into a
// read-only text segment costs another page of file for no benefit here.
//
// Externs are resolved by emitSyscallStubs (see asm.go): an ELF target has no
// DLL imports, so `extern write` becomes `mov rax,1; syscall; ret`.

const (
	elfIdentMagic = 0x464C457F // "\x7fELF" read as a little-endian u32
	elfClass64    = 2
	elfDataLSB    = 1
	elfVersion    = 1
	elfOSABISysV  = 0

	etExec  = 2
	emX8664 = 0x3E

	ptLoad = 1
	pfX    = 0x1
	pfW    = 0x2
	pfR    = 0x4

	shtNull     = 0
	shtProgBits = 1
	shtSymTab   = 2
	shtStrTab   = 3
	shtNoBits   = 8 // SHT_NOBITS: .bss -- no file bytes, zero-filled at load

	shfWrite     = 0x1
	shfAlloc     = 0x2
	shfExecInstr = 0x4

	elfBase        = 0x400000
	elfEhSize      = 64
	elfPhEntSize   = 56
	elfShEntSize   = 64
	elfTextFileOff = 0x80 // ELF header + one program header, 16-aligned
)

// BuildELF lays out every section and writes a runnable ELF64 executable.
func (a *Assembler) BuildELF(outPath string) error {
	// Lay the sections out back to back. Every section gets a file offset
	// (== vaddr - elfBase, since there is a single PT_LOAD) even when empty,
	// so symbols in a section with no bytes still resolve.
	secOff := map[*Section]int{}
	type placed struct {
		s    *Section
		off  int
		flag uint64
		sz   int // virtual size: len(s.Data) normally, s.cur for .bss
	}
	var placed2 []placed

	cur := elfTextFileOff
	var bssSecs []*Section
	for _, name := range []string{".text", ".rdata", ".data", ".tls", ".bss"} {
		s := a.sectionByName(name)
		if s == nil {
			continue
		}
		if s.Bss {
			// .bss holds uninitialised globals: virtual space after the loadable
			// image but no file bytes. Reserved until the file end is known.
			bssSecs = append(bssSecs, s)
			continue
		}
		cur = align(cur, 16)
		secOff[s] = cur
		if len(s.Data) == 0 {
			continue
		}
		var flag uint64
		switch name {
		case ".text":
			flag = shfAlloc | shfExecInstr
		case ".rdata":
			flag = shfAlloc
		default:
			flag = shfAlloc | shfWrite
		}
		placed2 = append(placed2, placed{s: s, off: cur, flag: flag, sz: len(s.Data)})
		cur += len(s.Data)
	}
	loadEnd := align(cur, 16)
	// .bss sections live right after the loaded file data (virtual only); the
	// loader zero-fills them. memEnd tracks the full virtual extent.
	memEnd := loadEnd
	for _, s := range bssSecs {
		if s.cur == 0 {
			continue
		}
		secOff[s] = loadEnd
		placed2 = append(placed2, placed{s: s, off: loadEnd, flag: shfAlloc | shfWrite, sz: s.cur})
		memEnd += align(s.cur, 16)
	}

	// Resolve every symbol to a virtual address.
	symVA := map[string]int{}
	for name, loc := range a.syms {
		s := a.sections[loc.sect]
		if off, ok := secOff[s]; ok {
			symVA[name] = elfBase + off + loc.off
		}
	}

	// Apply fixups. The displacement is measured from the byte after the
	// displacement field itself, plus any trailing bytes the instruction has
	// (f.ripAdj). Short jumps carry a 1-byte rel8 instead of a disp32.
	for _, f := range a.fixups {
		t, ok := symVA[f.sym]
		if !ok {
			return fmt.Errorf("undefined symbol referenced: %s", f.sym)
		}
		s := a.sections[f.sect]
		if err := applyFixup(s, f, t, elfBase+secOff[s]); err != nil {
			return err
		}
	}

	entryVA, ok := symVA[a.entry]
	if !ok {
		return fmt.Errorf("entry symbol %q not defined", a.entry)
	}

	// ---- symbol table (.symtab + .strtab) ----
	// A standard symbol table keeps the ELF inspectable (objdump/readelf) and
	// lets a non-Linux loader locate chokepoint symbols such as __goc_syscall
	// by name instead of scanning code. It is not part of the loadable segment.
	secIdx := make(map[*Section]int, len(placed2))
	for i, p := range placed2 {
		secIdx[p.s] = i + 1
	}
	symtab, strtab := a.buildSymtab(symVA, secIdx)

	// ---- guest unwind metadata (.gocuw) ----
	// One fixed-layout record per function (see gocrun's parser for the exact
	// byte format). The Windows gocrun loader reads this and registers a
	// RtlAddFunctionTable so the x64 unwinder can walk guest frames during a
	// Win32 syscall's internal exception dispatch. Non-alloc, so it lives
	// outside the loadable segment (read from the file, not mapped at runtime).
	var uwData []byte
	putU32 := func(v uint32) {
		uwData = append(uwData, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
	}
	for _, fn := range a.uwRecs {
		so, ok1 := secOff[a.sections[fn.sect]]
		eo, ok2 := secOff[a.sections[fn.sect]]
		if !ok1 || !ok2 {
			continue
		}
		putU32(uint32(so + fn.start)) // RVA of function start (vaddr - elfBase)
		putU32(uint32(eo + fn.end))   // RVA of function end
		uwData = append(uwData, byte(len(fn.pushes)))
		for _, r := range fn.pushes {
			uwData = append(uwData, byte(r))
		}
		putU32(uint32(fn.alloc)) // sub rsp, N frame allocation
	}

	// Section headers live after the loadable image and the symbol table;
	// .shstrtab holds their names. Not loaded, but keeps objdump/readelf useful.
	// non-loaded metadata sections placed after the loadable image: .gocuw
	// (guest unwind table, if any), .symtab, .strtab, .shstrtab.
	// Declare the .shstrtab builder (shstr/addName) first so the posts slice
	// below can reference shstr directly.
	shstr := []byte{0}
	nameOff := map[string]int{}
	addName := func(n string) int {
		if o, ok := nameOff[n]; ok {
			return o
		}
		o := len(shstr)
		shstr = append(shstr, []byte(n)...)
		shstr = append(shstr, 0)
		nameOff[n] = o
		return o
	}

	type postSec struct {
		name string
		data []byte
	}
	posts := []postSec{}
	if len(uwData) > 0 {
		posts = append(posts, postSec{".gocuw", uwData})
	}
	posts = append(posts, postSec{".symtab", symtab}, postSec{".strtab", strtab})
	// .shstrtab is NOT appended here: building the posts slice captures the
	// (still empty) shstr slice, but addName is only called later (placed loop
	// and below), so the captured shstr would be stale. We emit .shstrtab last
	// from the final shstr instead. The trailing NULL must NOT be counted in
	// numSh or e_shstrndx would point past the real .shstrtab.
	numSh := 1 + len(placed2) + len(posts) + 1 // +1 for .shstrtab
	off := align(loadEnd, 8)
	postOff := make([]int, len(posts))
	for i := range posts {
		postOff[i] = off
		off += len(posts[i].data)
		off = align(off, 8)
	}
	shOff := off
	shstrOff := shOff + numSh*elfShEntSize
	shStrIdx := numSh - 1

	img := make([]byte, loadEnd)

	// ELF header.
	putU32at(img, 0, elfIdentMagic)
	img[4] = elfClass64
	img[5] = elfDataLSB
	img[6] = elfVersion
	img[7] = elfOSABISysV
	img[8] = 0 // ABI version
	putU16at(img, 16, etExec)
	putU16at(img, 18, emX8664)
	putU32at(img, 20, elfVersion)
	putU64at(img, 24, uint64(entryVA))
	putU64at(img, 32, elfEhSize)     // e_phoff
	putU64at(img, 40, uint64(shOff)) // e_shoff
	putU32at(img, 48, 0)             // e_flags
	putU16at(img, 52, elfEhSize)
	putU16at(img, 54, elfPhEntSize)
	putU16at(img, 56, 1) // e_phnum
	putU16at(img, 58, elfShEntSize)
	putU16at(img, 60, uint16(numSh))
	putU16at(img, 62, uint16(shStrIdx))

	// Program header: one PT_LOAD covering the whole image, RWX.
	putU32at(img, 64, ptLoad)
	putU32at(img, 68, pfR|pfW|pfX)
	putU64at(img, 72, 0)                // p_offset
	putU64at(img, 80, elfBase)          // p_vaddr
	putU64at(img, 88, elfBase)          // p_paddr (unused on Linux)
	putU64at(img, 96, uint64(loadEnd))  // p_filesz
	putU64at(img, 104, uint64(memEnd))  // p_memsz (includes zero-filled .bss)
	putU64at(img, 112, 0x1000)          // p_align

	// Section contents.
	for _, p := range placed2 {
		copy(img[p.off:], p.s.Data)
	}

	// Section header table.
	sh := make([]byte, numSh*elfShEntSize)
	putSection := func(i int, nameIdx int, typ uint32, flags, addr, off, size uint64, al int) {
		b := i * elfShEntSize
		putU32at(sh, b+0, uint32(nameIdx))
		putU32at(sh, b+4, typ)
		putU64at(sh, b+8, flags)
		putU64at(sh, b+16, addr)
		putU64at(sh, b+24, off)
		putU64at(sh, b+32, size)
		putU32at(sh, b+40, 0) // sh_link
		putU32at(sh, b+44, 0) // sh_info
		putU64at(sh, b+48, uint64(al))
		putU64at(sh, b+56, 0) // sh_entsize
	}
	putSection(0, 0, shtNull, 0, 0, 0, 0, 0)
	for i, p := range placed2 {
		var typ uint32 = shtProgBits
		if p.s.Bss {
			typ = shtNoBits
		}
		putSection(i+1, addName(p.s.Name), typ, p.flag,
			uint64(elfBase+p.off), uint64(p.off), uint64(p.sz), 16)
	}
	// Post-load (non-alloc) sections: .gocuw + symbol tables.
	base := 1 + len(placed2)
	for i, ps := range posts {
		idx := base + i
		var typ uint32 = shtProgBits
		if ps.name == ".symtab" {
			typ = shtSymTab
		} else if ps.name == ".strtab" || ps.name == ".shstrtab" {
			typ = shtStrTab
		}
		putSection(idx, addName(ps.name), typ, 0, 0, uint64(postOff[i]), uint64(len(ps.data)), 1)
	}
	// .shstrtab section header: point at the real shstr, written last in the
	// file (after the section header table, at shstrOff). addName(".shstrtab")
	// is safe here because every other section name has already been registered.
	putSection(numSh-1, addName(".shstrtab"), shtStrTab, 0, 0, uint64(shstrOff), uint64(len(shstr)), 1)
	// .symtab links to .strtab; sh_info is one past the last local symbol
	// (all our symbols are global, so the first global is index 1).
	for i, ps := range posts {
		if ps.name == ".symtab" {
			symIdx := base + i
			strIdx := base + i + 1
			putU32at(sh, symIdx*elfShEntSize+40, uint32(strIdx))
			putU32at(sh, symIdx*elfShEntSize+44, 1)
		}
	}

	out := make([]byte, 0, shstrOff+len(shstr))
	out = append(out, img...)
	for i := range posts {
		for len(out) < postOff[i] {
			out = append(out, 0)
		}
		out = append(out, posts[i].data...)
	}
	for len(out) < shOff {
		out = append(out, 0)
	}
	out = append(out, sh...)
	out = append(out, shstr...)

	return os.WriteFile(outPath, out, 0o755)
}

// buildSymtab emits a standard ELF64 symbol table (.symtab) paired with its
// string table (.strtab). Every symbol goc defines (function/label) becomes a
// global STT_FUNC entry so external tools -- and the Windows gocrun loader --
// can resolve names such as __goc_syscall by address.
func (a *Assembler) buildSymtab(symVA map[string]int, secIdx map[*Section]int) ([]byte, []byte) {
	type ent struct {
		name    string
		nameOff int
		info    uint8
		shndx   uint16
		value   uint64
	}
	ents := []ent{{}} // leading null symbol (all-zero)
	names := make([]string, 0, len(a.syms))
	for n := range a.syms {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		loc := a.syms[n]
		s := a.sections[loc.sect]
		shndx := 0
		if idx, ok := secIdx[s]; ok {
			shndx = idx
		}
		// STB_GLOBAL<<4 | STT_FUNC
		ents = append(ents, ent{name: n, info: 1<<4 | 2, shndx: uint16(shndx), value: uint64(symVA[n])})
	}

	var strtab []byte
	strtab = append(strtab, 0) // leading NUL
	for i := 1; i < len(ents); i++ {
		ents[i].nameOff = len(strtab)
		strtab = append(strtab, []byte(ents[i].name)...)
		strtab = append(strtab, 0)
	}

	var symtab []byte
	for _, e := range ents {
		var b [24]byte
		putU32at(b[:], 0, uint32(e.nameOff))
		b[4] = e.info
		b[5] = 0 // st_other
		putU16at(b[:], 6, e.shndx)
		putU64at(b[:], 8, e.value)
		putU64at(b[:], 16, 0) // st_size
		symtab = append(symtab, b[:]...)
	}
	return symtab, strtab
}
