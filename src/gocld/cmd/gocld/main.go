package main

// gocld -- the linker.
//
// It reads relocatable objects and writes an executable: a Windows PE32+ or a
// static Linux ELF64. Until now it existed only as a library, reached from
// inside the compiler, with the assembler and the linker always adjacent -- a
// program existed only as a finished exe. As a command it is the second stage
// of a two-stage build:
//
//	goc -c a.c        -> a.o
//	goc -c b.c        -> b.o
//	gocld a.o b.o -o app.exe
//
// The objects are ordinary COFF, so a program can be built partly with goc and
// partly with another toolchain's compiler, as long as that toolchain emits
// COFF for the same target. What gocld adds is that the final link stays inside
// goc's own model: no C runtime, no system linker, and the same import table
// (kernel32 and user32 only) that the single-stage build produces.
//
// The one thing this cannot do is decide what to do about a symbol two objects
// both define. A real toolchain has comdat groups and a --allow-multiple-
// definition policy; gocld has neither, and the reason is specific rather than
// merely unimplemented -- see resolveDuplicates.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gocld"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

const usage = `usage: gocld [options] file.o... -o out[.exe]

Reads relocatable COFF objects and links them into an executable.

Options:
  -o <file>       write the executable here (default: a.exe, or a.out for -target linux)
  -target <win|linux>  container format (default: win)
  -e <symbol>     entry symbol (default: _start, then __goc_start, then main)
  -static         accepted and ignored: the output is always static
  -s, -strip      accepted and ignored
  -L <dir>, -l<name>  accepted and ignored: there are no libraries to search yet
  --version       print the version and exit
  -h, --help      print this message
`

func run(args []string, errOut *os.File) int {
	var (
		out      string
		target   = "win"
		entry    string
		inputs   []string
		wantHelp bool
	)

	// A separate-value flag has to swallow its value so the value is not
	// mistaken for an input file. gcc's own long options are accepted and
	// ignored where they make no sense for a linker with no libraries, because a
	// build command that passes them should not fail on them.
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") || a == "-" {
			inputs = append(inputs, a)
			continue
		}
		name, val := a, ""
		if eq := strings.IndexByte(a, '='); eq >= 0 {
			name, val = a[:eq], a[eq+1:]
		}
		need := func() string {
			if val != "" {
				return val
			}
			i++
			if i >= len(args) {
				fmt.Fprintf(errOut, "gocld: %s needs a value\n", name)
				return ""
			}
			return args[i]
		}
		switch name {
		case "-h", "--help", "-help":
			wantHelp = true
		case "--version", "-v":
			fmt.Println("gocld (goc linker)")
			return 0
		case "-o":
			if v := need(); v != "" {
				out = v
			}
		case "-target":
			if v := need(); v != "" {
				target = v
			}
		case "-e":
			if v := need(); v != "" {
				entry = v
			}
		case "-L", "-l", "-static", "-s", "-strip", "-n", "-O", "-g",
			"-m", "-shared", "-pie", "--gc-sections", "-nostdlib",
			"-nodefaultlibs", "--no-undefined", "-M", "-MD", "-MF",
			"--build-id", "-z", "--eh-frame-hdr", "--gc-keep-exported",
			"-Xlinker", "--start-group", "--end-group":
			// Accepted and ignored. -l and -L name libraries, and there are none
			// to search: gocld links objects, and the C runtime arrives inside
			// them rather than from a library path.
			if name == "-l" && val == "" {
				i++
			}
		default:
			// Anything else gcc might pass (-Wl,..., -p, --no-relax) is
			// swallowed rather than rejected, so a foreign build command works.
		}
	}

	if wantHelp {
		fmt.Fprint(os.Stdout, usage)
		return 0
	}
	if len(inputs) == 0 {
		fmt.Fprintln(errOut, "gocld: no input files")
		fmt.Fprint(errOut, usage)
		return 1
	}

	elf := false
	switch target {
	case "win", "windows", "pe":
	case "linux", "elf":
		elf = true
	default:
		fmt.Fprintf(errOut, "gocld: unknown target %q (want win or linux)\n", target)
		return 1
	}

	if out == "" {
		if elf {
			out = "a.out"
		} else {
			out = "a.exe"
		}
	}

	if err := link(inputs, out, entry, elf); err != nil {
		fmt.Fprintln(errOut, "gocld:", err)
		return 1
	}
	return 0
}

// link reads every input, merges them into one image and writes the executable.
func link(inputs []string, outPath, entryOverride string, elf bool) error {
	if dir := filepath.Dir(outPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	img := gocld.NewImage(gocld.TargetPE)
	if elf {
		img.Target = gocld.TargetELF
	}

	for _, in := range inputs {
		data, err := os.ReadFile(in)
		if err != nil {
			return err
		}
		if elf {
			err = img.IngestELFBytes(data)
		} else {
			err = img.IngestCOFFBytes(data)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", in, err)
		}
	}

	img.Entry = pickEntry(img, entryOverride)

	n, err := gocld.LinkObject(img, nil, outPath, elf)
	if err != nil {
		return err
	}
	fmt.Printf("linked %d object%s -> %s (%d bytes)\n",
		len(inputs), plural(len(inputs)), outPath, n)
	return nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// entryCandidates are the names an object may use to say "this is the program
// entry", in the order they are tried.
//
// _start and __goc_start are the startup symbols goa's codegen emits around
// main; main is the fallback so that an object whose startup was inlined away,
// or one built by hand, still links without the flag.
var entryCandidates = []string{"_start", "__goc_start", "main"}

// pickEntry decides which symbol the loader jumps to.
//
// A COFF object has no entry-point field -- the entry is a property of the
// linked image, not of any one object -- so it is recovered by name. An object
// that is only a helper contributes none of these and that is fine: an
// executable whose main comes from one of its objects still gets a valid entry.
func pickEntry(img *gocld.Image, override string) string {
	if override != "" {
		return override
	}
	for _, name := range entryCandidates {
		if _, ok := img.Syms[name]; ok {
			return name
		}
	}
	// Nothing matched. Returning "" makes BuildPE report the missing entry by
	// name, which is a far better failure than jumping somewhere arbitrary.
	return ""
}
