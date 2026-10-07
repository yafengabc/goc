package goa

import (
	"fmt"
	"os"
	"path/filepath"

	"gocld"
)

// AssembleSource assembles asm source text and writes a runnable executable to
// outPath. elf selects a static Linux ELF64 container; otherwise a Windows
// PE32+ is produced (an `extern Name, dll` in the source becomes an import,
// and a `subsystem windows` directive selects the GUI subsystem). It returns
// the size of the file written.
//
// This is the entry point goc links against: the assembler is compiled into
// the compiler binary, so no external goa executable is needed.
func AssembleSource(src, outPath string, elf bool) (int64, error) {
	a := NewAssembler()
	if elf {
		a.target = targetELF
	}
	if err := a.Assemble(src); err != nil {
		return 0, err
	}
	return a.writeImage(outPath, elf)
}

// AssembleFile reads the assembly in srcPath, assembles it and writes outPath.
// format is "pe"/"win"/"windows" or "elf"/"linux"; an empty format keeps the
// default (PE). It returns the size of the file written.
func AssembleFile(srcPath, outPath, format string) (int64, error) {
	src, err := os.ReadFile(srcPath)
	if err != nil {
		return 0, err
	}
	elf := false
	switch format {
	case "", "pe", "win", "windows":
	case "elf", "linux":
		elf = true
	default:
		return 0, fmt.Errorf("unknown output format %q (want pe or elf)", format)
	}
	return AssembleSource(string(src), outPath, elf)
}

// AssembleATT is AssembleSource for AT&T (GAS) input -- the dialect LLVM's
// AsmPrinter emits. The front end in att.go rewrites each line into goa's own
// syntax and hands it to the same encoder, so the produced image is identical to
// what the equivalent goa-syntax source would have yielded.
//
// This exists so a whole LLVM-generated .s file can be assembled by goa instead
// of by an external assembler. That matters for two reasons: the pipeline stops
// depending on a COFF object being produced by libLLVM (and on the linker that
// would then have to consume it), and goa's optimiser and unwind bookkeeping
// get to see the real code rather than a pre-linked blob.
func AssembleATT(src, outPath string, elf bool) (int64, error) {
	a := NewAssembler()
	if elf {
		a.target = targetELF
	}
	if err := attAssemble(a, src); err != nil {
		return 0, err
	}
	return a.writeImage(outPath, elf)
}

// AssembleOpts carries the symbol classification the assembly text cannot
// express. Both sets are nil for a caller that has nothing to say, and both
// mean "no special treatment": a nil LibSyms makes every second definition an
// error, a nil InternalSyms makes every symbol externally linked.
type AssembleOpts struct {
	// LibSyms names the symbols that are a copy of the goc C library rather
	// than the user's own definitions. See AssembleObject.
	LibSyms map[string]bool
	// InternalSyms names the symbols the front end saw declared `static` at
	// file scope. See AssembleObject.
	InternalSyms map[string]bool
}

// AssembleObject assembles src and writes it as a relocatable object file
// rather than a linked executable. It returns the size of the file written.
//
// This is what makes `-c` mean "produce an object". The image is handed to the
// linker with its fixups still pending: the sections hold the machine code, the
// symbol table holds what this object defines, and the relocation table holds
// the places whose value depends on where a symbol lands. Deciding those
// addresses is a later step, and doing it here would make the object
// inseparable from the program that happens to be linked today.
//
// elf selects the container, and it has to: an object is not a portable thing.
// A Windows object is COFF and a Linux one is ELF64, and a linker handed the
// wrong one says so immediately rather than producing a program that cannot
// start. Which is why this is a parameter and not a guess -- the target decides
// the format, all the way down.
//
// The result is standard, so `objdump -h/-t/-r` reads it and any aware tool can
// consume it. Undefined symbols -- a call into the C library, an imported
// Windows API, a reference to a sibling unit -- are recorded as such, which is
// how the object says what it still needs.
//
// The two symbol sets are facts only the front end holds. `static` and "this is
// a copy of the C library" are both invisible in the assembly text -- a static
// function and an extern one are the same label goa is asked to define -- so they
// have to be handed in rather than recovered.
func AssembleObject(src, outPath string, elf bool, opts AssembleOpts) (int64, error) {
	libSyms := opts.LibSyms
	a := NewAssembler()
	a.staticSyms = opts.InternalSyms
	if elf {
		a.target = targetELF
	}
	if err := a.Assemble(src); err != nil {
		return 0, err
	}
	img := a.LinkImage()
	// The object's own name, so a link that finds two units defining the same
	// symbol can say which two rather than "an earlier object".
	img.FileName = filepath.Base(outPath)
	// Which definitions are the C library's. goc has no library stage -- a unit
	// inlines the library functions it calls -- so a program in which two units
	// print has two copies of printf, and the linker needs to be able to tell
	// that from a user who defined printf twice.
	//
	// goa's own runtime symbols join the set: they are marked the same way for
	// the same reason, and their names cannot be prefixed because a loader looks
	// them up by name.
	//
	// Merged into a fresh map rather than into libSyms, which belongs to the
	// caller: the set is per object, and writing into the caller's would make
	// the second object out of one compilation claim the first one's symbols.
	if len(libSyms) > 0 || len(a.libSyms) > 0 {
		img.LibSyms = make(map[string]bool, len(libSyms)+len(a.libSyms))
		for name := range libSyms {
			img.LibSyms[name] = true
		}
		for name := range a.libSyms {
			img.LibSyms[name] = true
		}
	}
	var obj []byte
	if elf {
		obj = gocld.WriteELFObject(img)
	} else {
		obj = gocld.WriteCOFFObject(img)
	}
	if err := os.WriteFile(outPath, obj, 0644); err != nil {
		return 0, err
	}
	return int64(len(obj)), nil
}

// AssembleWithObject assembles src and then merges a COFF object into the same
// image before writing it out.
//
// This is how the two halves of the LLVM backend come together: goa's own
// assembler produces the entry stub, the globals and the C runtime, LLVM
// produces the user's own functions, and the two are linked here. Order matters
// -- the assembly is assembled first so its symbols exist when the object's
// relocations are resolved against them.
//
// obj may be nil, in which case this is exactly AssembleSource.
func AssembleWithObject(src string, obj []byte, outPath string, elf bool) (int64, error) {
	a := NewAssembler()
	if elf {
		a.target = targetELF
	}
	if err := a.Assemble(src); err != nil {
		return 0, err
	}
	// One image for the whole program. Building it once matters: the object
	// merges into this Image, so a second LinkImage() would hand the linker a
	// program whose functions live in the object and whose entry stub lives in
	// a different copy of the sections -- and the stub's `call main` would
	// resolve against an image that has no main at all.
	img := a.LinkImage()
	if len(obj) > 0 {
		if elf {
			if err := img.IngestELFBytes(obj); err != nil {
				return 0, fmt.Errorf("linking the LLVM object: %w", err)
			}
		} else {
			if err := img.IngestCOFFBytes(obj); err != nil {
				return 0, fmt.Errorf("linking the LLVM object: %w", err)
			}
		}
	}
	return a.writeImageTo(img, outPath, elf)
}
