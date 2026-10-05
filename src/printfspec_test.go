package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The two back ends have to agree on the printf specialisation. If they
// disagreed, a program's size would depend on which back end built it -- and,
// worse, the emitter and the reachability prune would disagree about what the
// program references, so the rewrite would silently be undone.
//
// specializePrintfCall is the single decision both share; these tests pin the
// decision itself, and TestPrintfSpecAgreesAcrossBackEnds below pins the
// property the sharing exists to protect.

func TestSpecializePrintfCall(t *testing.T) {
	noShadow := printfQueries{
		userDefines:   func(string) bool { return false },
		shadowedByVar: func(string) bool { return false },
	}
	cases := []struct {
		name string
		call *Call
		want string // "" means no rewrite
	}{
		{
			name: "no conversion becomes fwrite",
			call: &Call{Name: "printf", Args: []Expr{&StrLit{Bytes: []byte("hi\n")}}},
			want: "fwrite",
		},
		{
			name: "integers become the lite formatter",
			call: &Call{Name: "printf", Args: []Expr{&StrLit{Bytes: []byte("%d items\n")},
				&NumLit{Val: 3, Kind: TInt}}},
			want: "__goclib_printf_lite",
		},
		{
			name: "a float picks the _f entry, keeping the integer-only one out",
			call: &Call{Name: "printf", Args: []Expr{&StrLit{Bytes: []byte("%f")},
				&NumLit{Val: 1, Kind: TInt}}},
			want: "__goclib_printf_lite_f",
		},
		{
			name: "fprintf without conversions becomes fwrite on that stream",
			call: &Call{Name: "fprintf", Args: []Expr{&Ident{Name: "logf"},
				&StrLit{Bytes: []byte("raw\n")}}},
			want: "fwrite",
		},
		// A width needs vfmt's field machinery, which is exactly what the
		// specialisation exists to avoid pulling in.
		{name: "width is not lite", call: &Call{Name: "printf",
			Args: []Expr{&StrLit{Bytes: []byte("%5d")}, &NumLit{Val: 1, Kind: TInt}}}},
		{name: "precision is not lite", call: &Call{Name: "printf",
			Args: []Expr{&StrLit{Bytes: []byte("%.2f")}, &NumLit{Val: 1, Kind: TInt}}}},
		{name: "%e needs the exponent estimator", call: &Call{Name: "printf",
			Args: []Expr{&StrLit{Bytes: []byte("%e")}, &NumLit{Val: 1, Kind: TInt}}}},
		{name: "%p is not lite", call: &Call{Name: "printf",
			Args: []Expr{&StrLit{Bytes: []byte("%p")}, &NumLit{Val: 1, Kind: TInt}}}},
		{name: "%s alone is lite", call: &Call{Name: "printf",
			Args: []Expr{&StrLit{Bytes: []byte("%s")}, &StrLit{Bytes: []byte("x")}}},
			want: "__goclib_printf_lite"},
		// A run-time format string proves nothing at compile time.
		{name: "a variable format is left alone", call: &Call{Name: "printf",
			Args: []Expr{&Ident{Name: "fmt"}}}},
		// Dropping extra arguments would lose their evaluation.
		{name: "extra arguments to a plain format are left alone", call: &Call{Name: "printf",
			Args: []Expr{&StrLit{Bytes: []byte("hi\n")}, &NumLit{Val: 1, Kind: TInt}}}},
		// The lite formatters write to stdout; there is no fprintf equivalent.
		{name: "fprintf with conversions is left alone", call: &Call{Name: "fprintf",
			Args: []Expr{&Ident{Name: "logf"}, &StrLit{Bytes: []byte("%d")},
				&NumLit{Val: 1, Kind: TInt}}}},
		{name: "another function is not ours", call: &Call{Name: "puts",
			Args: []Expr{&StrLit{Bytes: []byte("hi\n")}}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := specializePrintfCall(c.call, noShadow)
			if c.want == "" {
				if got != nil {
					t.Fatalf("rewritten to %s, want no rewrite", got.Name)
				}
				return
			}
			if got == nil {
				t.Fatalf("no rewrite, want %s", c.want)
			}
			if got.Name != c.want {
				t.Fatalf("rewritten to %s, want %s", got.Name, c.want)
			}
		})
	}
}

// A program that defines its own fwrite must keep it: the rewrite exists to
// call the C library's, and binding to the program's would be silently wrong.
func TestSpecializePrintfCallRespectsShadowing(t *testing.T) {
	call := &Call{Name: "printf", Args: []Expr{&StrLit{Bytes: []byte("hi\n")}}}
	t.Run("a program-defined fwrite blocks it", func(t *testing.T) {
		q := printfQueries{
			userDefines:   func(n string) bool { return n == "fwrite" },
			shadowedByVar: func(string) bool { return false },
		}
		if got := specializePrintfCall(call, q); got != nil {
			t.Fatalf("rewrote to %s despite the program's own fwrite", got.Name)
		}
	})
	t.Run("a function-pointer variable blocks it", func(t *testing.T) {
		q := printfQueries{
			userDefines:   func(string) bool { return false },
			shadowedByVar: func(n string) bool { return n == "fwrite" },
		}
		if got := specializePrintfCall(call, q); got != nil {
			t.Fatalf("rewrote to %s despite a shadowing variable", got.Name)
		}
	})
	t.Run("a program-defined lite entry blocks it", func(t *testing.T) {
		lite := &Call{Name: "printf", Args: []Expr{&StrLit{Bytes: []byte("%d")},
			&NumLit{Val: 1, Kind: TInt}}}
		q := printfQueries{
			userDefines:   func(n string) bool { return n == "__goclib_printf_lite" },
			shadowedByVar: func(string) bool { return false },
		}
		if got := specializePrintfCall(lite, q); got != nil {
			t.Fatalf("rewrote to %s despite the program's own lite entry", got.Name)
		}
	})
	// A nil query set means the caller could not answer; rewriting on a guess
	// would be worse than not rewriting at all.
	t.Run("no queries means no rewrite", func(t *testing.T) {
		if got := specializePrintfCall(call, printfQueries{}); got != nil {
			t.Fatalf("rewrote to %s with no shadow information", got.Name)
		}
	})
}

// The rewrite introduces references the original call did not contain -- most
// visibly the stdout accessor that fwrite is handed -- so the reachability
// prune has to learn about them. This checks the shape: a rewritten fwrite call
// must come with both names.
func TestPrintfSpecAddsItsOwnReferences(t *testing.T) {
	lit := &StrLit{Bytes: []byte("hi\n")}
	got := specializePrintfCall(&Call{Name: "printf", Args: []Expr{lit}}, printfQueries{
		userDefines:   func(string) bool { return false },
		shadowedByVar: func(string) bool { return false },
	})
	if got == nil {
		t.Fatal("expected a rewrite")
	}
	// The stream argument is the accessor, not a bare global: goclib models
	// stdout as a function so that the streams can be initialised lazily.
	if len(got.Args) != 4 {
		t.Fatalf("fwrite call has %d args, want 4", len(got.Args))
	}
	acc, ok := got.Args[3].(*Call)
	if !ok || acc.Name != "__goclib_stdout" {
		t.Fatalf("stream argument is %#v, want a __goclib_stdout() call", got.Args[3])
	}
	// Size and count have to be the literal's byte length, or fwrite would
	// write the wrong number of bytes.
	num, ok := got.Args[2].(*NumLit)
	if !ok || num.Val != int64(len(lit.Bytes)) {
		t.Fatalf("size argument is %#v, want %d", got.Args[2], len(lit.Bytes))
	}
}

// The format classifier is shared, so a change to it moves both back ends at
// once. These are the boundaries the goclib lite formatters document; a
// disagreement here is a bug in either the walk or the runtime.
func TestScanLiteFormat(t *testing.T) {
	cases := []struct {
		fmt       string
		has       bool
		hasFloat  bool
		reasoning string
	}{
		{"%d", true, false, "integer"},
		{"%s", true, false, "string"},
		{"%c", true, false, "char"},
		{"%u %o %x %X %i", true, false, "all the integer conversions"},
		{"%f", true, true, "float"},
		{"%F", true, true, "float, upper case"},
		// "%%" is not a conversion: it prints a literal percent and consumes no
		// argument, so a format made only of those has nothing to format. That
		// makes it the fwrite case, not the lite one.
		{"%%", false, false, "a literal percent is not a conversion"},
		{"100%% done", false, false, "percent among text, still no conversion"},
		{"50%% of %d", true, false, "text plus a real conversion"},
		{"%5d", false, false, "a width needs vfmt's field machinery"},
		{"%-5d", false, false, "a flag needs it too"},
		{"%.2f", false, false, "precision"},
		{"%ld", false, false, "a length modifier"},
		{"%e", false, false, "the exponent estimator"},
		{"%g", false, false, "same"},
		{"%a", false, false, "hex float needs the estimator"},
		{"%p", false, false, "pointer formatting is not in the lite set"},
		{"plain text", false, false, "no conversion at all -- the fwrite case"},
	}
	for _, c := range cases {
		has, hasFloat := scanLiteFormat(c.fmt)
		if has != c.has || hasFloat != c.hasFloat {
			t.Errorf("scanLiteFormat(%q) = (%v, %v), want (%v, %v) -- %s",
				c.fmt, has, hasFloat, c.has, c.hasFloat, c.reasoning)
		}
	}
}

// A run of the real thing, end to end: the specialisation must shrink the
// binary and the program must still print what it printed. The size assertion
// is the point -- a rewrite that quietly stops happening is invisible
// otherwise, since the program keeps producing correct output either way.
func TestPrintfSpecShrinksBinary(t *testing.T) {
	if !llvmAvailable(t) {
		t.Skip("skipping: no libLLVM configured (set GOC_LLVM_DLL)")
	}
	dir := t.TempDir()
	cPath := filepath.Join(dir, "spec.c")
	const src = "#include <stdio.h>\n" +
		"int main(void) { printf(\"plain text, no conversion\\n\"); return 0; }\n"
	if err := os.WriteFile(cPath, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "spec.exe")
	cmd := exec.Command(exePath(t), "-fllvm", cPath, "-o", exe)
	// The compiler writes scratch files through TMP; on Windows the child
	// inherits nothing useful unless it is set explicitly.
	cmd.Env = append(os.Environ(), "TMP="+dir, "TEMP="+dir, "TMPDIR="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("goc -fllvm failed: %v\n%s", err, out)
	}
	// The format has no conversion, so it specialises to a direct fwrite. If it
	// did not, printf would drag in the float exponent machine and the binary
	// would be several times larger -- which is the regression this guards.
	if fi, err := os.Stat(exe); err != nil {
		t.Fatal(err)
	} else if fi.Size() > 12*1024 {
		t.Errorf("-fllvm binary is %d bytes; a printf with no conversion should "+
			"specialise to fwrite and stay far below the %d the full format "+
			"engine costs", fi.Size(), 16896)
	}
	run := exec.Command(exe)
	o, _ := run.CombinedOutput()
	if got := run.ProcessState.ExitCode(); got != 0 {
		t.Errorf("exit code %d, want 0 (stdout %q)", got, o)
	}
	if !containsBytes(string(o), "plain text, no conversion") {
		t.Errorf("stdout %q does not contain the printed line", o)
	}
}

// TestUnwindSectionsStayOutOfImage guards a size regression that is invisible
// from the section contents: LLVM always emits .pdata/.xdata, and a PE section
// occupies a whole multiple of FileAlignment -- 512, the smallest Windows
// accepts -- no matter how few bytes it holds. Keeping the unwind tables
// therefore cost every -fllvm image at least 1024 bytes, which was enough to
// make `print("hello world")` come out larger than the same program built by
// the native generator, which emits no unwind info at all.
//
// The test reads the section table rather than the total size, so a future
// change that makes the tables grow cannot quietly make this pass while
// reintroducing the waste.
func TestUnwindSectionsStayOutOfImage(t *testing.T) {
	if !llvmAvailable(t) {
		t.Skip("skipping: no libLLVM configured (set GOC_LLVM_DLL)")
	}
	dir := t.TempDir()
	cPath := filepath.Join(dir, "uw.c")
	const src = "int main(void) { return 0; }\n"
	if err := os.WriteFile(cPath, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "uw.exe")
	cmd := exec.Command(exePath(t), "-fllvm", cPath, "-o", exe)
	cmd.Env = append(os.Environ(), "TMP="+dir, "TEMP="+dir, "TMPDIR="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("goc -fllvm failed: %v\n%s", err, out)
	}
	img, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	// The exception directory is a direct proxy: pe.go only fills it when a
	// .pdata section was given an address, so a zero here means the tables were
	// left out on purpose rather than by accident.
	if got := peDataDirectory(img, 3); got != 0 {
		t.Errorf("exception directory is %#x, want 0: the Win64 unwind table "+
			"reached the image and cost at least 1024 bytes of file alignment", got)
	}
	for _, name := range []string{".pdata", ".xdata"} {
		if _, _, ok := peFindSection(img, name); ok {
			t.Errorf("image has a %s section; it should be merged but not mapped", name)
		}
	}
}

// peDataDirectory returns the (rva, size) pair of the PE optional header's
// data directory entry i, or 0 for the RVA when the entry is absent.
func peDataDirectory(img []byte, i int) uint32 {
	off := int(le32(img, 0x3c))
	if off+4+20+128 > len(img) {
		return 0
	}
	opt := off + 4 + 20 // PE sig + COFF header
	magic := le16(img, opt)
	var dd int
	switch magic {
	case 0x20b: // PE32+
		dd = opt + 112
	case 0x10b: // PE32
		dd = opt + 96
	default:
		return 0
	}
	at := dd + i*8
	if at+8 > len(img) {
		return 0
	}
	return le32(img, at)
}

// peFindSection reports whether the image carries a section with the given name.
func peFindSection(img []byte, name string) (rva, size uint32, ok bool) {
	off := int(le32(img, 0x3c))
	if off+4+20 > len(img) {
		return 0, 0, false
	}
	nsec := int(le16(img, off+6))
	optSize := int(le16(img, off+20))
	st := off + 4 + 20 + optSize
	for i := 0; i < nsec; i++ {
		e := st + 40*i
		if e+40 > len(img) {
			return 0, 0, false
		}
		if string(img[e:e+8]) == name {
			return le32(img, e+12), le32(img, e+8), true
		}
	}
	return 0, 0, false
}

func le16(b []byte, off int) uint16 { return uint16(b[off]) | uint16(b[off+1])<<8 }

func le32(b []byte, off int) uint32 {
	return uint32(b[off]) | uint32(b[off+1])<<8 | uint32(b[off+2])<<16 | uint32(b[off+3])<<24
}

// TestPrimitivesShrinkUnderLLVM pins the case that started this: the built-in
// print lowers to a raw write of a string literal, the smallest thing goc can
// emit, and the image should reflect that. Before the runtime's file-scope
// variables were pruned and the unwind tables dropped, the same program was 50%
// *larger* under -fllvm than under the native generator.
func TestPrimitivesShrinkUnderLLVM(t *testing.T) {
	if !llvmAvailable(t) {
		t.Skip("skipping: no libLLVM configured (set GOC_LLVM_DLL)")
	}
	dir := t.TempDir()
	cPath := filepath.Join(dir, "tiny.c")
	const src = "int main(void) { print(\"hello world\"); return 0; }\n"
	if err := os.WriteFile(cPath, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	sizeOf := func(extra ...string) int64 {
		exe := filepath.Join(dir, "tiny"+strings.Join(extra, "")+".exe")
		args := append([]string{"-o", exe}, extra...)
		args = append(args, cPath)
		cmd := exec.Command(exePath(t), args...)
		cmd.Env = append(os.Environ(), "TMP="+dir, "TEMP="+dir, "TMPDIR="+dir)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("goc %v failed: %v\n%s", extra, err, out)
		}
		fi, err := os.Stat(exe)
		if err != nil {
			t.Fatal(err)
		}
		return fi.Size()
	}
	native := sizeOf()
	llvm := sizeOf("-fllvm")
	if llvm >= native {
		t.Errorf("-fllvm image is %d bytes, native is %d; the LLVM backend "+
			"should not be the larger of the two for a program this small", llvm, native)
	}
	exe := filepath.Join(dir, "tiny-fllvm.exe")
	run := exec.Command(exe)
	o, _ := run.CombinedOutput()
	if got := run.ProcessState.ExitCode(); got != 0 {
		t.Errorf("exit code %d, want 0 (stdout %q)", got, o)
	}
	if !containsBytes(string(o), "hello world") {
		t.Errorf("stdout %q does not contain the printed text", o)
	}
}
