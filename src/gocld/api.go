package gocld

import "os"

// The three operations on an Image that both the COFF merger and the two
// container builders need, plus the public entry points.
//
// They are functions over *Image rather than methods on an encoder because the
// encoder is not here: this package never assembles anything. A caller states
// an Image and gets a file.

// NewImage returns an empty image targeting the given container.
func NewImage(target int) *Image {
	return &Image{
		Syms:      map[string]SymLoc{},
		Exts:      map[string]string{},
		Subsystem: 3, // console, the PE default
		Target:    target,
	}
}

// newSection appends a section and returns it.
func newSection(img *Image, name string, writable, code bool) *Section {
	s := &Section{Name: name, Writable: writable, Code: code}
	img.Sections = append(img.Sections, s)
	return s
}

// sectionByName finds a section by name, or nil.
func sectionByName(img *Image, name string) *Section {
	for _, s := range img.Sections {
		if s.Name == name {
			return s
		}
	}
	return nil
}

// LinkObject merges a COFF object and writes the image, which is the whole task
// in one call: lay out the sections, resolve the symbols, apply the
// relocations, write the headers.
//
// The object is the program's machine code and its relocations. The entry
// point and any imports have to be set on the Image first -- an object does not
// say which of its symbols the loader jumps to, and a program that prints needs
// a WriteFile the object does not define.
func LinkObject(img *Image, obj []byte, outPath string, elf bool) (int64, error) {
	if len(obj) > 0 {
		if err := img.IngestCOFFBytes(obj); err != nil {
			return 0, err
		}
	}
	if elf {
		if err := img.BuildELF(outPath); err != nil {
			return 0, err
		}
	} else if err := img.BuildPE(outPath); err != nil {
		return 0, err
	}
	fi, err := os.Stat(outPath)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}
