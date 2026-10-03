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
	if elf {
		if err := a.BuildELF(outPath); err != nil {
			return 0, err
		}
	} else {
		if err := a.BuildPE(outPath); err != nil {
			return 0, err
		}
	}
	fi, err := os.Stat(outPath)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
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
