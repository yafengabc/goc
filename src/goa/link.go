package goa

import (
	"os"

	"gocld"
)

// LinkImage hands the assembled program to the linker as data.
//
// The Assembler is the encoding half: it turns assembly text into section
// bytes, a symbol table and a list of relocations still to apply. The linker
// takes those and produces the image. Neither owns the other's state, so this
// is where one is copied into the other's shape -- a copy rather than a shared
// struct, because sharing would put the linker's fields in the encoder and
// make a change to either a change to both.
//
// The copy is cheap next to what it replaces: a program is thousands of
// instructions and a few dozen symbols.
func (a *Assembler) LinkImage() *gocld.Image {
	img := gocld.NewImage(a.target)

	// The sections are shared rather than copied. The encoder is finished with
	// them by the time a program is linked -- nothing appends after Assemble
	// returns -- and the linker pads and merges them in place. Copying the
	// slices would mean copying their contents too, which is the one part of
	// this that is not cheap.
	img.Sections = make([]*gocld.Section, len(a.sections))
	for i, s := range a.sections {
		ns := &gocld.Section{
			Name:     s.Name,
			Writable: s.Writable,
			Code:     s.Code,
			Data:     s.Data,
			VSize:    s.cur,
			Bss:      s.Bss,
			Unmapped: s.Unmapped,
		}
		img.Sections[i] = ns
	}
	for name, loc := range a.syms {
		img.Syms[name] = gocld.SymLoc{
			Sect:   loc.sect,
			Off:    loc.off,
			Static: a.staticSyms[name],
		}
	}
	for _, f := range a.fixups {
		nf := gocld.Fixup{
			Sect:      f.sect,
			Off:       f.off,
			Sym:       f.sym,
			Sym2:      f.sym2,
			Addend:    f.addend,
			RipAdjust: f.ripAdj,
			Absolute:  f.absolute,
			Wide:      f.wide,
			Virtual:   f.virtual,
			Short:     f.short,
		}
		img.Fixups = append(img.Fixups, nf)
	}
	for _, u := range a.uwRecs {
		img.UWRecs = append(img.UWRecs, &gocld.UWFunc{
			Sect:       u.sect,
			Start:      u.start,
			End:        u.end,
			Pushes:     u.pushes,
			Alloc:      u.alloc,
			HasProlog:  u.hasProlog,
			PrologDone: u.prologDone,
		})
	}
	for name, dll := range a.exts {
		img.Exts[name] = dll
	}
	img.Entry = a.entry
	img.Subsystem = a.subsystem
	img.PdataRVA = a.pdataRVA
	img.PdataSize = a.pdataSize
	return img
}

// writeImage hands the assembled program to the linker and returns the size of
// the file written.
//
// Nothing is copied back: the linker resolves the symbols, but the encoder has
// no further use for their addresses -- a symbol recorded after this point
// refers to a section the linker has already laid out, and the caller reaches
// it through the Image if it needs to.
func (a *Assembler) writeImage(outPath string, elf bool) (int64, error) {
	return a.writeImageTo(a.LinkImage(), outPath, elf)
}

// writeImageTo is writeImage for a caller that already has the image, because
// it merged something into it.
func (a *Assembler) writeImageTo(img *gocld.Image, outPath string, elf bool) (int64, error) {
	build := img.BuildPE
	if elf {
		build = img.BuildELF
	}
	if err := build(outPath); err != nil {
		return 0, err
	}
	fi, err := os.Stat(outPath)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}
