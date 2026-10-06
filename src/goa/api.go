package goa

import (
	"fmt"
	"os"
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
