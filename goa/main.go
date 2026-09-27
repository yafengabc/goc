package main

import (
	"fmt"
	"os"
	"strings"
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

	elf := false
	switch format {
	case "pe", "win", "windows":
	case "elf", "linux":
		elf = true
	default:
		fmt.Fprintf(os.Stderr, "goa: unknown output format %q (want pe or elf)\n", format)
		os.Exit(1)
	}

	srcPath := positional[0]
	src, err := os.ReadFile(srcPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read error:", err)
		os.Exit(1)
	}

	a := NewAssembler()
	if elf {
		a.target = targetELF
	}
	if err := a.Assemble(string(src)); err != nil {
		fmt.Fprintln(os.Stderr, "assemble error:", err)
		os.Exit(1)
	}

	outPath := ""
	if len(positional) > 1 {
		outPath = positional[1]
	} else if elf {
		// Linux executables conventionally carry no suffix.
		outPath = strings.TrimSuffix(srcPath, ".asm")
	} else {
		outPath = strings.TrimSuffix(srcPath, ".asm") + ".exe"
	}

	if elf {
		err = a.BuildELF(outPath)
	} else {
		err = a.BuildPE(outPath)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "build error:", err)
		os.Exit(1)
	}
	fmt.Printf("compiled %s (%d bytes)\n", outPath, mustSize(outPath))
}

func mustSize(p string) int64 {
	fi, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return fi.Size()
}
