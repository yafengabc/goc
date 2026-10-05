package main

// The goa command-line assembler. The assembler itself lives in the goa
// package (../..) so goc can link it in instead of shelling out to a separate
// binary; this is the standalone front-end that keeps bin/goa.exe working
// (used by src/goa/run_tests.sh and by hand-written asm).

import (
	"fmt"
	"os"
	"strings"

	"goa"
)

func usage() {
	fmt.Fprintf(os.Stderr,
		"goa - assemble a small x86-64 subset into a runnable executable\n\n"+
			"usage: goa [-f pe|elf] <file.asm> [output]\n\n"+
			"  -f pe    Windows PE32+ (default). Output gets a .exe suffix.\n"+
			"  -f elf   Linux ELF64, static, no libc. Output has no suffix.\n\n"+
			"PE targets import externs from DLLs (`extern WriteFile, kernel32`).\n"+
			"ELF targets have no DLL imports: an extern must name a Linux\n"+
			"syscall (`extern write`), which becomes a `mov rax,N; syscall` stub.\n")
}

func main() {
	var positional []string
	format := "pe"

	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-f" || arg == "--format" || arg == "-target":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "goa: -f needs an argument (pe or elf)")
				os.Exit(1)
			}
			i++
			format = args[i]
		case strings.HasPrefix(arg, "--format="):
			format = strings.TrimPrefix(arg, "--format=")
		case strings.HasPrefix(arg, "-target="):
			format = strings.TrimPrefix(arg, "-target=")
		case arg == "-h" || arg == "--help":
			usage()
			return
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(os.Stderr, "goa: unknown option %q\n", arg)
			os.Exit(1)
		default:
			positional = append(positional, arg)
		}
	}

	if len(positional) < 1 {
		usage()
		os.Exit(1)
	}

	srcPath := positional[0]
	outPath := ""
	if len(positional) > 1 {
		outPath = positional[1]
	} else if format == "elf" || format == "linux" {
		// Linux executables conventionally carry no suffix.
		outPath = strings.TrimSuffix(srcPath, ".asm")
	} else {
		outPath = strings.TrimSuffix(srcPath, ".asm") + ".exe"
	}

	n, err := goa.AssembleFile(srcPath, outPath, format)
	if err != nil {
		fmt.Fprintln(os.Stderr, "goa:", err)
		os.Exit(1)
	}
	fmt.Printf("compiled %s (%d bytes)\n", outPath, n)
}
