package main

import (
	"fmt"
	"os"
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
	shtStrTab   = 3

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
	}
	var placed2 []placed

	cur := elfTextFileOff
	for _, name := range []string{".text", ".rdata", ".data"} {
		s := a.sectionByName(name)
		if s == nil {
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
		placed2 = append(placed2, placed{s: s, off: cur, flag: flag})
		cur += len(s.Data)
	}
	loadEnd := align(cur, 16)

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

	// Section headers live after the loadable image; .shstrtab holds their
	// names. Not loaded, but it keeps objdump/readelf useful.
	numSh := 2 + len(placed2) // null + sections + .shstrtab
	shOff := align(loadEnd, 8)
	shstrOff := shOff + numSh*elfShEntSize
	shStrIdx := numSh - 1

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
	putU64at(img, 104, uint64(loadEnd)) // p_memsz
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
		putSection(i+1, addName(p.s.Name), shtProgBits, p.flag,
			uint64(elfBase+p.off), uint64(p.off), uint64(len(p.s.Data)), 16)
	}
	putSection(shStrIdx, addName(".shstrtab"), shtStrTab, 0, 0, uint64(shstrOff), uint64(len(shstr)), 1)

	out := make([]byte, 0, shstrOff+len(shstr))
	out = append(out, img...)
	out = append(out, sh...)
	out = append(out, shstr...)

	return os.WriteFile(outPath, out, 0o755)
}
