package main

import (
	"fmt"
	"os"
	"sort"
)

// ---------------------------------------------------------------------------
// PE32+ (x86-64) executable writer
// ---------------------------------------------------------------------------

// Memory vs. on-disk granularity. SectionAlignment is the page size (4 KiB) and
// cannot shrink; FileAlignment only governs padding inside the file, and 512 is
// the smallest value the PE spec allows. Using 512 instead of 4096 is what keeps
// a 300-byte .text section from costing 4 KiB on disk.
const (
	sectAlign = 0x1000
	fileAlign = 0x200
)

func align(v, a int) int {
	if a <= 1 {
		return v
	}
	return (v + a - 1) / a * a
}

func putU16at(b []byte, off int, v uint16) {
	b[off] = byte(v)
	b[off+1] = byte(v >> 8)
}
func putU32at(b []byte, off int, v uint32) {
	b[off] = byte(v)
	b[off+1] = byte(v >> 8)
	b[off+2] = byte(v >> 16)
	b[off+3] = byte(v >> 24)
}
func putU64at(b []byte, off int, v uint64) {
	for i := 0; i < 8; i++ {
		b[off+i] = byte(v >> (8 * i))
	}
}

func align8(v int) int { return align(v, 8) }

func sortedKeys(m map[string][]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// buildIData constructs the .idata section content (import directory, ILT, IAT,
// hint/name tables, DLL name strings). Returns the raw bytes plus RVA info.
func (a *Assembler) buildIData(idataBase int) (data []byte, iatSymOff map[string]int, dirRVA, dirSize, iatRVA, iatSize int) {
	iatSymOff = map[string]int{}
	dlls := map[string][]string{}
	for name, dll := range a.exts {
		dlls[dll] = append(dlls[dll], name)
	}
	dllNames := sortedKeys(dlls)

	// pad appends zero bytes until data length is a multiple of 8.
	pad8 := func() {
		for len(data)%8 != 0 {
			data = append(data, 0)
		}
	}
	// pad2 aligns to a 2-byte (WORD) boundary, required for hint fields.
	pad2 := func() {
		for len(data)%2 != 0 {
			data = append(data, 0)
		}
	}

	// reserve import directory (one descriptor per dll + zero terminator)
	dirOff := len(data)
	dirSize = (len(dllNames) + 1) * 20
	data = append(data, make([]byte, dirSize)...)

	type desc struct{ ilt, name, iat, nfunc int }
	descs := []desc{}

	for _, dll := range dllNames {
		funcs := dlls[dll]
		// Each ILT/IAT must start on an 8-byte boundary; PAD FIRST, then
		// record the offset so the captured offset matches the real data.
		pad8()
		iltOff := len(data)
		data = append(data, make([]byte, (len(funcs)+1)*8)...)
		pad8()
		iatOff := len(data)
		data = append(data, make([]byte, (len(funcs)+1)*8)...)

		hnOffs := make([]int, len(funcs))
		for i, f := range funcs {
			pad2()
			hn := len(data)
			data = append(data, 0, 0) // hint (0)
			data = append(data, []byte(f)...)
			data = append(data, 0) // null terminator
			hnOffs[i] = hn
		}
		pad2()
		dllNameOff := len(data)
		data = append(data, []byte(dll)...)
		data = append(data, 0)

		for i, f := range funcs {
			v := uint64(idataBase + hnOffs[i])
			putU64at(data, iltOff+i*8, v)
			putU64at(data, iatOff+i*8, v)
			iatSymOff["IAT:"+f] = iatOff + i*8
		}
		descs = append(descs, desc{iltOff, dllNameOff, iatOff, len(funcs)})
	}

	// fill directory descriptors
	for i, d := range descs {
		base := dirOff + i*20
		putU32at(data, base+0, uint32(idataBase+d.ilt))   // OriginalFirstThunk
		putU32at(data, base+12, uint32(idataBase+d.name)) // Name
		putU32at(data, base+16, uint32(idataBase+d.iat))  // FirstThunk
	}

	dirRVA = idataBase + dirOff

	// IAT range (first IAT start .. last IAT end)
	minIat, maxEnd := 1<<30, 0
	for _, d := range descs {
		if d.iat < minIat {
			minIat = d.iat
		}
		end := d.iat + (d.nfunc+1)*8
		if end > maxEnd {
			maxEnd = end
		}
	}
	if len(descs) > 0 {
		iatRVA = idataBase + minIat
		iatSize = maxEnd - minIat
	}
	return
}

// BuildPE assembles all sections and writes a valid PE32+ executable.
//
// Layout: one .text section (RX) plus a single merged read-write .data section
// that holds .rdata, .data and the generated import table back to back. Two
// sections is the minimum that keeps code non-writable, it lets the headers fit
// in one 512-byte file block, and it avoids paying FileAlignment padding three
// times over.
func (a *Assembler) BuildPE(outPath string) error {
	text := a.sectionByName(".text")
	if text == nil || len(text.Data) == 0 {
		return fmt.Errorf("no code in .text section")
	}
	rdata := a.sectionByName(".rdata")
	data := a.sectionByName(".data")

	blob := func(s *Section) []byte {
		if s == nil {
			return nil
		}
		return s.Data
	}
	rdataLen := len(blob(rdata))
	dataLen := len(blob(data))

	// Virtual layout: .text at 0x1000; the merged data section starts right
	// after the text section's page-aligned virtual size. A fixed 0x2000 base
	// worked only while .text fit in one page — once it outgrows that, .text's
	// declared VirtualSize overlaps the data section's virtual address and the
	// loader rejects the file.
	textBase := sectAlign
	dataBase := textBase + align(len(text.Data), sectAlign)

	// Offsets of each blob inside the merged data section (8-byte aligned so
	// that dq constants stay naturally aligned).
	rdOff := 0
	dOff := align(rdataLen, 8)
	idOff := align(dOff+dataLen, 8)
	idataBase := dataBase + idOff

	idata, iatSymOff, dirRVA, dirSize, iatRVA, iatSize := a.buildIData(idataBase)

	// Base RVA per *source* section, used to resolve symbol references.
	symBase := map[string]int{
		".text":  textBase,
		".rdata": dataBase + rdOff,
		".data":  dataBase + dOff,
	}

	// Resolve symbol RVAs.
	symRVA := map[string]int{}
	for name, loc := range a.syms {
		s := a.sections[loc.sect]
		symRVA[name] = symBase[s.Name] + loc.off
	}
	for k, v := range iatSymOff {
		symRVA[k] = idataBase + v
	}

	// Apply fixups: patch each recorded displacement (rel32 normally, rel8 for
	// short jumps) at the site the instruction left for it.
	for _, f := range a.fixups {
		t, ok := symRVA[f.sym]
		if !ok {
			return fmt.Errorf("undefined symbol referenced: %s", f.sym)
		}
		s := a.sections[f.sect]
		if err := applyFixup(s, f, t, symBase[s.Name]); err != nil {
			return err
		}
	}

	entryRVA, ok := symRVA[a.entry]
	if !ok {
		return fmt.Errorf("entry symbol %q not defined", a.entry)
	}

	// Concatenate the data blobs into one section payload.
	merged := make([]byte, 0, idOff+len(idata))
	merged = append(merged, blob(rdata)...)
	for len(merged) < dOff {
		merged = append(merged, 0)
	}
	merged = append(merged, blob(data)...)
	for len(merged) < idOff {
		merged = append(merged, 0)
	}
	merged = append(merged, idata...)

	type outSec struct {
		name string
		va   int
		data []byte
		ch   uint32
	}
	sections := []outSec{{".text", textBase, text.Data, 0x60000020}}
	if len(merged) > 0 {
		sections = append(sections, outSec{".data", dataBase, merged, 0xC0000040})
	}

	// SizeOfImage must cover the end of the last section's virtual range
	// (each section's virtual address plus its virtual size, rounded up to the
	// section alignment). Hard-coding two pages worked only while the whole
	// image fit in them; once .text or .data outgrows that, the loader rejects
	// the file as malformed.
	imageSize := 0
	for _, s := range sections {
		end := s.va + align(len(s.data), sectAlign)
		if end > imageSize {
			imageSize = end
		}
	}

	numSec := len(sections)
	// 0x98 = DOS header (0x80) + PE signature (4) + COFF header (20).
	headerSize := align(0x98+240+40*numSec, fileAlign)

	hdr := make([]byte, headerSize)
	// DOS header
	putU16at(hdr, 0, 0x5A4D)  // "MZ"
	putU16at(hdr, 0x3C, 0x80) // e_lfanew -> 0x80
	// PE signature
	hdr[0x80] = 'P'
	hdr[0x81] = 'E'
	hdr[0x82] = 0
	hdr[0x83] = 0
	// COFF header (0x84)
	putU16at(hdr, 0x84, 0x8664)         // Machine = AMD64
	putU16at(hdr, 0x86, uint16(numSec)) // NumberOfSections
	putU16at(hdr, 0x94, 240)            // SizeOfOptionalHeader
	putU16at(hdr, 0x96, 0x0022)         // Characteristics
	// Optional header (PE32+) at 0x98
	oh := 0x98
	putU16at(hdr, oh+0, 0x20B) // Magic
	putU32at(hdr, oh+4, uint32(len(text.Data)))
	putU32at(hdr, oh+8, uint32(len(merged)))
	putU32at(hdr, oh+16, uint32(entryRVA))
	putU32at(hdr, oh+20, uint32(textBase))
	putU64at(hdr, oh+24, 0x140000000) // ImageBase
	putU32at(hdr, oh+32, sectAlign)   // SectionAlignment
	putU32at(hdr, oh+36, fileAlign)   // FileAlignment
	putU16at(hdr, oh+40, 6)           // OS major version
	putU16at(hdr, oh+48, 6)           // Subsystem major version
	putU32at(hdr, oh+56, uint32(imageSize))
	putU32at(hdr, oh+60, uint32(headerSize))
	putU16at(hdr, oh+68, a.subsystem) // Subsystem (2 = GUI, 3 = console)
	putU64at(hdr, oh+72, 0x100000)
	putU64at(hdr, oh+80, 0x1000)
	putU64at(hdr, oh+88, 0x100000)
	putU64at(hdr, oh+96, 0x1000)
	putU32at(hdr, oh+108, 16) // NumberOfRvaAndSizes
	// Data directories at oh+112
	dd := oh + 112
	if dirSize > 0 {
		putU32at(hdr, dd+8, uint32(dirRVA)) // Import (index 1)
		putU32at(hdr, dd+12, uint32(dirSize))
	}
	if iatSize > 0 {
		putU32at(hdr, dd+12*8, uint32(iatRVA)) // IAT (index 12)
		putU32at(hdr, dd+12*8+4, uint32(iatSize))
	}
	// Section table at oh+240; raw pointers advance by file-aligned sizes.
	st := oh + 240
	filePtr := headerSize
	for _, s := range sections {
		for i := 0; i < 8; i++ {
			if i < len(s.name) {
				hdr[st+i] = s.name[i]
			} else {
				hdr[st+i] = ' '
			}
		}
		rawSize := align(len(s.data), fileAlign)
		putU32at(hdr, st+8, uint32(len(s.data))) // VirtualSize
		putU32at(hdr, st+12, uint32(s.va))       // VirtualAddress
		putU32at(hdr, st+16, uint32(rawSize))    // SizeOfRawData
		putU32at(hdr, st+20, uint32(filePtr))    // PointerToRawData
		putU32at(hdr, st+36, s.ch)
		st += 40
		filePtr += rawSize
	}

	// Assemble final file.
	out := make([]byte, 0, filePtr)
	out = append(out, hdr...)
	for _, s := range sections {
		out = append(out, s.data...)
		out = append(out, make([]byte, align(len(s.data), fileAlign)-len(s.data))...)
	}
	return os.WriteFile(outPath, out, 0o755)
}
