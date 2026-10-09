package gocld

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
	elfClass32    = 1
	elfDataLSB    = 1
	elfVersion    = 1
	elfOSABISysV  = 0

	etExec  = 2
	emX8664 = 0x3E

	// EMX8664 is the ELF e_machine for x86-64, exported for callers that build
	// an Image from goa's own sections rather than from an ingested ELF object:
	// goa's encoder speaks x86-64 and nothing else, and without an ingest
	// nothing stamps the machine on the image, which the ELF writer then emits
	// as 0 -- a header the Linux kernel rejects with ENOEXEC.
	EMX8664 = emX8664

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
	elfSymEntSize  = 24   // sizeof(Elf64_Sym)
	elfTextFileOff = 0x80 // ELF header + one program header, 16-aligned

	// ELF32 sizes, used by the 32-bit ELF writer (ARM32, i386, RV32).
	elfEhSize32     = 52
	elfPhEntSize32  = 32
	elfShEntSize32  = 40
	elfSymEntSize32 = 16 // sizeof(Elf32_Sym)
	// elfBase32 is the load address of a 32-bit ELF image. ARM Linux maps a
	// static executable here; any address with the property p_vaddr ≡ p_offset
	// (mod p_align) works for a single RWX segment, and 0x8000 satisfies it.
	elfBase32        = 0x8000
	elfTextFileOff32 = 96 // elfEhSize32(52) + elfPhEntSize32(32) aligned to 16
)

// BuildELF lays out every section and writes a runnable ELF executable. The
// container width follows the object that was ingested: a 32-bit ELF object
// (ARM32, i386, RV32) links into an ELF32 image, a 64-bit object (x86-64,
// AArch64, RV64) into an ELF64. Both callers go through this one entry point so
// the choice is made in one place rather than duplicated at every link site.
func (img *Image) BuildELF(outPath string) error {
	if img.Class == elfClass32 {
		return img.buildELF32(outPath)
	}
	return img.buildELF64(outPath)
}

// buildELF64 lays out every section and writes a runnable ELF64 executable.
func (state *Image) buildELF64(outPath string) error {
	// Lay the sections out back to back. Every section gets a file offset
	// (== vaddr - elfBase, since there is a single PT_LOAD) even when empty,
	// so symbols in a section with no bytes still resolve.
	secOff := map[*Section]int{}
	type placed struct {
		s    *Section
		off  int
		flag uint64
		sz   int // virtual size: len(s.Data) normally, s.VSize for .bss
	}
	var placed2 []placed

	cur := elfTextFileOff
	var bssSecs []*Section
	for _, name := range []string{".text", ".rdata", ".data", ".tls", ".bss"} {
		s := sectionByName(state, name)
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
		if s.VSize == 0 {
			continue
		}
		secOff[s] = loadEnd
		placed2 = append(placed2, placed{s: s, off: loadEnd, flag: shfAlloc | shfWrite, sz: s.VSize})
		memEnd += align(s.VSize, 16)
	}

	// Resolve every symbol to a virtual address.
	symVA := map[string]int{}
	for name, loc := range state.Syms {
		s := state.Sections[loc.Sect]
		if off, ok := secOff[s]; ok {
			symVA[name] = elfBase + off + loc.Off
		}
	}

	// Apply fixups. The displacement is measured from the byte after the
	// displacement field itself, plus any trailing bytes the instruction has
	// (f.RipAdjust). Short jumps carry a 1-byte rel8 instead of a disp32.
	for _, f := range state.Fixups {
		t, ok := symVA[f.Sym]
		if !ok {
			return fmt.Errorf("undefined symbol referenced: %s", f.Sym)
		}
		var t2 int
		if f.Sym2 != "" {
			if t2, ok = symVA[f.Sym2]; !ok {
				return fmt.Errorf("undefined symbol referenced: %s", f.Sym2)
			}
		}
		s := state.Sections[f.Sect]
		if err := applyFixup(s, f, t, t2, elfBase+secOff[s]); err != nil {
			return err
		}
	}

	// Non-x86 relocations (AArch64 bit-field fixups, ...) are applied the same
	// way as the x86 Fixups: after layout, with each symbol's resolved address.
	for _, r := range state.Relocs {
		t, ok := symVA[r.Sym]
		if !ok {
			return fmt.Errorf("undefined symbol referenced: %s", r.Sym)
		}
		s := state.Sections[r.Sect]
		if err := applyReloc(s, r, t, elfBase+secOff[s]); err != nil {
			return err
		}
	}

	entryVA, ok := symVA[state.Entry]
	if !ok {
		return fmt.Errorf("entry symbol %q not defined", state.Entry)
	}

	// ---- symbol table (.symtab + .strtab) ----
	// A standard symbol table keeps the ELF inspectable (objdump/readelf) and
	// lets a non-Linux loader locate chokepoint symbols such as __goc_syscall
	// by name instead of scanning code. It is not part of the loadable segment.
	secIdx := make(map[*Section]int, len(placed2))
	for i, p := range placed2 {
		secIdx[p.s] = i + 1
	}
	symtab, strtab := state.buildSymtab(symVA, secIdx)

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
	for _, fn := range state.UWRecs {
		so, ok1 := secOff[state.Sections[fn.Sect]]
		eo, ok2 := secOff[state.Sections[fn.Sect]]
		if !ok1 || !ok2 {
			continue
		}
		putU32(uint32(so + fn.Start)) // RVA of function start (vaddr - elfBase)
		putU32(uint32(eo + fn.End))   // RVA of function end
		uwData = append(uwData, byte(len(fn.Pushes)))
		for _, r := range fn.Pushes {
			uwData = append(uwData, byte(r))
		}
		putU32(uint32(fn.Alloc)) // sub rsp, N frame allocation
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

	buf := make([]byte, loadEnd)

	// ELF header.
	putU32at(buf, 0, elfIdentMagic)
	buf[4] = elfClass64
	buf[5] = elfDataLSB
	buf[6] = elfVersion
	buf[7] = elfOSABISysV
	buf[8] = 0 // ABI version
	putU16at(buf, 16, etExec)
	putU16at(buf, 18, state.Machine)
	putU32at(buf, 20, elfVersion)
	putU64at(buf, 24, uint64(entryVA))
	putU64at(buf, 32, elfEhSize)     // e_phoff
	putU64at(buf, 40, uint64(shOff)) // e_shoff
	putU32at(buf, 48, 0)             // e_flags
	putU16at(buf, 52, elfEhSize)
	putU16at(buf, 54, elfPhEntSize)
	putU16at(buf, 56, 1) // e_phnum
	putU16at(buf, 58, elfShEntSize)
	putU16at(buf, 60, uint16(numSh))
	putU16at(buf, 62, uint16(shStrIdx))

	// Program header: one PT_LOAD covering the whole image, RWX.
	putU32at(buf, 64, ptLoad)
	putU32at(buf, 68, pfR|pfW|pfX)
	putU64at(buf, 72, 0)               // p_offset
	putU64at(buf, 80, elfBase)         // p_vaddr
	putU64at(buf, 88, elfBase)         // p_paddr (unused on Linux)
	putU64at(buf, 96, uint64(loadEnd)) // p_filesz
	putU64at(buf, 104, uint64(memEnd)) // p_memsz (includes zero-filled .bss)
	putU64at(buf, 112, 0x1000)         // p_align

	// Section contents.
	for _, p := range placed2 {
		copy(buf[p.off:], p.s.Data)
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
		ent := uint64(0)
		if ps.name == ".symtab" {
			typ = shtSymTab
			// One Elf64_Sym per entry. Leaving this zero is what makes the
			// output unloadable by anything that walks the table properly:
			// readelf reports "invalid sh_entsize of 0" and gdb refuses the
			// file outright ("not in executable format"), which costs the
			// whole symbol table -- and with it any chance of a backtrace --
			// for a binary that runs fine. It is only fixed up below, where
			// the index is known.
			ent = elfSymEntSize
		} else if ps.name == ".strtab" || ps.name == ".shstrtab" {
			typ = shtStrTab
		}
		b := idx * elfShEntSize
		putSection(idx, addName(ps.name), typ, 0, 0, uint64(postOff[i]), uint64(len(ps.data)), 1)
		putU64at(sh, b+56, ent)
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
	out = append(out, buf...)
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
func (state *Image) buildSymtab(symVA map[string]int, secIdx map[*Section]int) ([]byte, []byte) {
	type ent struct {
		name    string
		nameOff int
		info    uint8
		shndx   uint16
		value   uint64
	}
	ents := []ent{{}} // leading null symbol (all-zero)
	names := make([]string, 0, len(state.Syms))
	for n := range state.Syms {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		loc := state.Syms[n]
		s := state.Sections[loc.Sect]
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

// buildELF32 lays out every section and writes a runnable ELF32 executable
// (ARM32 today; i386 and RV32 follow the same container). It is buildELF64's
// 32-bit twin: one RWX PT_LOAD, a standard symbol table for inspectability, no
// unwind metadata. Only the field widths differ -- 32-bit ELF header (52 bytes),
// program header (32 bytes), section header (40 bytes), and Elf32_Sym (16 bytes)
// -- and the load address, which is the ARM convention 0x8000.
func (img *Image) buildELF32(outPath string) error {
	secOff := map[*Section]int{}
	type placed struct {
		s    *Section
		off  int
		flag uint64
		sz   int
	}
	var placed2 []placed

	cur := elfTextFileOff32
	var bssSecs []*Section
	for _, name := range []string{".text", ".rdata", ".data", ".tls", ".bss"} {
		s := sectionByName(img, name)
		if s == nil {
			continue
		}
		if s.Bss {
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
	memEnd := loadEnd
	for _, s := range bssSecs {
		if s.VSize == 0 {
			continue
		}
		secOff[s] = loadEnd
		placed2 = append(placed2, placed{s: s, off: loadEnd, flag: shfAlloc | shfWrite, sz: s.VSize})
		memEnd += align(s.VSize, 16)
	}

	// Resolve every symbol to a virtual address.
	symVA := map[string]int{}
	for name, loc := range img.Syms {
		s := img.Sections[loc.Sect]
		if off, ok := secOff[s]; ok {
			symVA[name] = elfBase32 + off + loc.Off
		}
	}

	// Apply fixups (x86-style relative/absolute fields).
	for _, f := range img.Fixups {
		t, ok := symVA[f.Sym]
		if !ok {
			return fmt.Errorf("undefined symbol referenced: %s", f.Sym)
		}
		var t2 int
		if f.Sym2 != "" {
			if t2, ok = symVA[f.Sym2]; !ok {
				return fmt.Errorf("undefined symbol referenced: %s", f.Sym2)
			}
		}
		s := img.Sections[f.Sect]
		if err := applyFixup(s, f, t, t2, elfBase32+secOff[s]); err != nil {
			return err
		}
	}

	// Non-x86 relocations (ARM32 bit-field fixups, ...) -- applied the same way
	// as the x86 Fixups, after layout, with each symbol's resolved address.
	for _, r := range img.Relocs {
		t, ok := symVA[r.Sym]
		if !ok {
			return fmt.Errorf("undefined symbol referenced: %s", r.Sym)
		}
		s := img.Sections[r.Sect]
		if err := applyReloc(s, r, t, elfBase32+secOff[s]); err != nil {
			return err
		}
	}

	entryVA, ok := symVA[img.Entry]
	if !ok {
		return fmt.Errorf("entry symbol %q not defined", img.Entry)
	}

	// ---- symbol table (.symtab + .strtab) ----
	secIdx := make(map[*Section]int, len(placed2))
	for i, p := range placed2 {
		secIdx[p.s] = i + 1
	}
	symtab, strtab := img.buildSymtab32(symVA, secIdx)

	// Section headers live after the loadable image and the symbol table;
	// .shstrtab holds their names. Not loaded, but keeps objdump/readelf useful.
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
	posts = append(posts, postSec{".symtab", symtab}, postSec{".strtab", strtab})
	numSh := 1 + len(placed2) + len(posts) + 1 // +1 for .shstrtab
	off := align(loadEnd, 8)
	postOff := make([]int, len(posts))
	for i := range posts {
		postOff[i] = off
		off += len(posts[i].data)
		off = align(off, 8)
	}
	shOff := off
	shstrOff := shOff + numSh*elfShEntSize32
	shStrIdx := numSh - 1

	buf := make([]byte, loadEnd)

	// ELF header.
	putU32at(buf, 0, elfIdentMagic)
	buf[4] = elfClass32
	buf[5] = elfDataLSB
	buf[6] = elfVersion
	buf[7] = elfOSABISysV
	buf[8] = 0 // ABI version
	putU16at(buf, 16, etExec)
	putU16at(buf, 18, img.Machine)
	putU32at(buf, 20, elfVersion)
	putU32at(buf, 24, uint32(entryVA))
	putU32at(buf, 28, uint32(elfEhSize32)) // e_phoff
	putU32at(buf, 32, uint32(shOff))       // e_shoff
	putU32at(buf, 36, 0)                   // e_flags
	putU16at(buf, 40, elfEhSize32)
	putU16at(buf, 42, elfPhEntSize32)
	putU16at(buf, 44, 1) // e_phnum
	putU16at(buf, 46, elfShEntSize32)
	putU16at(buf, 48, uint16(numSh))
	putU16at(buf, 50, uint16(shStrIdx))

	// Program header: one PT_LOAD covering the whole image, RWX, immediately
	// after the ELF header. The Elf32_Phdr layout is p_type(4) p_offset(4)
	// p_vaddr(4) p_paddr(4) p_filesz(4) p_memsz(4) p_flags(4) p_align(4) -- a
	// single segment with p_offset 0 placed at p_vaddr, so the whole file maps
	// contiguously and every section's vaddr is elfBase32 + its file offset.
	ph := elfEhSize32
	putU32at(buf, ph+0, ptLoad)             // p_type
	putU32at(buf, ph+4, 0)                  // p_offset
	putU32at(buf, ph+8, uint32(elfBase32))  // p_vaddr
	putU32at(buf, ph+12, uint32(elfBase32)) // p_paddr (unused on Linux)
	putU32at(buf, ph+16, uint32(loadEnd))   // p_filesz
	putU32at(buf, ph+20, uint32(memEnd))    // p_memsz (includes zero-filled .bss)
	putU32at(buf, ph+24, pfR|pfW|pfX)       // p_flags
	putU32at(buf, ph+28, 0x1000)            // p_align

	// Section contents.
	for _, p := range placed2 {
		copy(buf[p.off:], p.s.Data)
	}

	// Section header table.
	sh := make([]byte, numSh*elfShEntSize32)
	putSection := func(i int, nameIdx int, typ uint32, flags, addr, off, size uint64, al int) {
		b := i * elfShEntSize32
		putU32at(sh, b+0, uint32(nameIdx))
		putU32at(sh, b+4, typ)
		putU32at(sh, b+8, uint32(flags))
		putU32at(sh, b+12, uint32(addr))
		putU32at(sh, b+16, uint32(off))
		putU32at(sh, b+20, uint32(size))
		putU32at(sh, b+24, 0) // sh_link
		putU32at(sh, b+28, 0) // sh_info
		putU32at(sh, b+32, uint32(al))
		putU32at(sh, b+36, 0) // sh_entsize
	}
	putSection(0, 0, shtNull, 0, 0, 0, 0, 0)
	for i, p := range placed2 {
		var typ uint32 = shtProgBits
		if p.s.Bss {
			typ = shtNoBits
		}
		putSection(i+1, addName(p.s.Name), typ, p.flag,
			uint64(elfBase32+p.off), uint64(p.off), uint64(p.sz), 16)
	}
	base := 1 + len(placed2)
	for i, ps := range posts {
		idx := base + i
		var typ uint32 = shtProgBits
		ent := uint64(0)
		if ps.name == ".symtab" {
			typ = shtSymTab
			ent = elfSymEntSize32
		} else if ps.name == ".strtab" || ps.name == ".shstrtab" {
			typ = shtStrTab
		}
		b := idx * elfShEntSize32
		putSection(idx, addName(ps.name), typ, 0, 0, uint64(postOff[i]), uint64(len(ps.data)), 1)
		putU32at(sh, b+36, uint32(ent))
	}
	putSection(numSh-1, addName(".shstrtab"), shtStrTab, 0, 0, uint64(shstrOff), uint64(len(shstr)), 1)
	for i, ps := range posts {
		if ps.name == ".symtab" {
			symIdx := base + i
			strIdx := base + i + 1
			putU32at(sh, symIdx*elfShEntSize32+24, uint32(strIdx))
			putU32at(sh, symIdx*elfShEntSize32+28, 1)
		}
	}

	out := make([]byte, 0, shstrOff+len(shstr))
	out = append(out, buf...)
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

// buildSymtab32 emits a standard ELF32 symbol table (.symtab) paired with its
// string table (.strtab), the 16-byte Elf32_Sym mirror of buildSymtab.
func (img *Image) buildSymtab32(symVA map[string]int, secIdx map[*Section]int) ([]byte, []byte) {
	type ent struct {
		name    string
		nameOff int
		info    uint8
		shndx   uint16
		value   uint64
	}
	ents := []ent{{}} // leading null symbol (all-zero)
	names := make([]string, 0, len(img.Syms))
	for n := range img.Syms {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		loc := img.Syms[n]
		s := img.Sections[loc.Sect]
		shndx := uint16(0)
		if idx, ok := secIdx[s]; ok {
			shndx = uint16(idx)
		}
		// STB_GLOBAL<<4 | STT_FUNC
		ents = append(ents, ent{name: n, info: 1<<4 | 2, shndx: shndx, value: uint64(symVA[n])})
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
		var b [16]byte
		putU32at(b[:], 0, uint32(e.nameOff))
		putU32at(b[:], 4, uint32(e.value))
		putU32at(b[:], 8, 0) // st_size
		b[12] = e.info
		b[13] = 0 // st_other
		putU16at(b[:], 14, e.shndx)
		symtab = append(symtab, b[:]...)
	}
	return symtab, strtab
}
