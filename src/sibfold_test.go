// F1 (sibFold) and F2 (direct-register index) unit tests.
//
// sibFold collapses the array-index idiom
//
//	imul r11, K; add r10, r11; mov D, [r10]   (or mov [r10], S)
//
// into a single SIB operand [r10+r11*K]; F2 makes genLValue consume a
// register-cached index variable straight from its callee-save home instead of
// spilling and reloading it. Both are exercised on fixtures, plus one
// end-to-end compile for F2.
package main

import (
	"strings"
	"testing"
)

// runFold runs sibFold over the fixture and returns the surviving texts.
func runFold(insts []Inst) []string {
	out := sibFold(insts)
	texts := make([]string, 0, len(out))
	for _, in := range out {
		texts = append(texts, strings.TrimSpace(in.Text))
	}
	return texts
}

// TestFoldLoadSIB: a plain dword load folds to [r10+r11*4], imul/add gone.
func TestFoldLoadSIB(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\tlea r10, [rip+G_a]"},
		{Kind: instInstr, Text: "\tmovsxd r11, dword [rbp-8]"},
		{Kind: instInstr, Text: "\timul r11, 4"},
		{Kind: instInstr, Text: "\tadd r10, r11"},
		{Kind: instInstr, Text: "\tmov eax, [r10]"},
		{Kind: instInstr, Text: "\tmov r10, [rbp-16]"}, // write of r10 ends the window
	}
	got := runFold(in)
	if countText(got, "mov eax, [r10+r11*4]") != 1 {
		t.Fatalf("load not folded into SIB: %v", got)
	}
	if countText(got, "imul r11, 4") != 0 || countText(got, "add r10, r11") != 0 {
		t.Fatalf("imul/add must be deleted: %v", got)
	}
}

// TestFoldStoreSIB: a sized store folds too.
func TestFoldStoreSIB(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\timul r11, 8"},
		{Kind: instInstr, Text: "\tadd r10, r11"},
		{Kind: instInstr, Text: "\tmov [r10], rax"},
		{Kind: instInstr, Text: "\tmov r11, 0"}, // write of r11 ends the window
	}
	got := runFold(in)
	if countText(got, "mov [r10+r11*8], rax") != 1 {
		t.Fatalf("store not folded into SIB: %v", got)
	}
	if countText(got, "imul r11, 8") != 0 || countText(got, "add r10, r11") != 0 {
		t.Fatalf("imul/add must be deleted: %v", got)
	}
}

// TestFoldByteStoreSIB: the sized "mov byte [r10], al" form folds.
func TestFoldByteStoreSIB(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\timul r11, 1"},
		{Kind: instInstr, Text: "\tadd r10, r11"},
		{Kind: instInstr, Text: "\tmov byte [r10], al"},
		{Kind: instInstr, Text: "\tmov r10, 0"},
	}
	got := runFold(in)
	if countText(got, "mov byte [r10+r11*1], al") != 1 {
		t.Fatalf("byte store not folded: %v", got)
	}
}

// TestFoldNoBadScale: a non-SIB scale (3) must not fold.
func TestFoldNoBadScale(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\timul r11, 3"},
		{Kind: instInstr, Text: "\tadd r10, r11"},
		{Kind: instInstr, Text: "\tmov eax, [r10]"},
	}
	got := runFold(in)
	if countText(got, "[r10+r11*3]") != 0 || countText(got, "imul r11, 3") != 1 {
		t.Fatalf("scale 3 must be left alone: %v", got)
	}
}

// TestFoldNoReadOfBaseAfter: a read of r10 after the idiom observes the
// pre-fold element address (r10 = base+index), which the fold would turn into
// the bare base -- so it must not fold.
func TestFoldNoReadOfBaseAfter(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\timul r11, 4"},
		{Kind: instInstr, Text: "\tadd r10, r11"},
		{Kind: instInstr, Text: "\tmov eax, [r10]"},
		{Kind: instInstr, Text: "\tmov rax, r10"}, // reads the old element address
	}
	got := runFold(in)
	if countText(got, "[r10+r11*4]") != 0 {
		t.Fatalf("must not fold when r10 is read afterwards: %v", got)
	}
}

// TestFoldNoFlagRead: a conditional jump before the next flag write observes
// the imul/add flags; deleting them changes behaviour.
func TestFoldNoFlagRead(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\timul r11, 4"},
		{Kind: instInstr, Text: "\tadd r10, r11"},
		{Kind: instInstr, Text: "\tmov eax, [r10]"},
		{Kind: instInstr, Text: "\tje .Ldone"}, // flag read, no flag write in between
	}
	got := runFold(in)
	if countText(got, "[r10+r11*4]") != 0 {
		t.Fatalf("must not fold when flags are read: %v", got)
	}
}

// TestFoldLabelBreaksWindow: a label after the mov is a window break; the fold
// is still safe (nothing after can observe r10/r11/flag state without a
// branch target pointing into the deleted instructions, which labels never do).
func TestFoldLabelBreaksWindow(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\timul r11, 4"},
		{Kind: instInstr, Text: "\tadd r10, r11"},
		{Kind: instInstr, Text: "\tmov eax, [r10]"},
		{Kind: instLabel, Text: ".Ldone:"},
	}
	got := runFold(in)
	if countText(got, "mov eax, [r10+r11*4]") != 1 {
		t.Fatalf("label break must not prevent an otherwise-safe fold: %v", got)
	}
}

// TestFoldNonAdjacent: an intervening instruction between add and the load
// breaks the idiom; no fold.
func TestFoldNonAdjacent(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\timul r11, 4"},
		{Kind: instInstr, Text: "\tadd r10, r11"},
		{Kind: instInstr, Text: "\tmov rax, [rbp-8]"}, // unrelated load in between
		{Kind: instInstr, Text: "\tmov eax, [r10]"},
	}
	got := runFold(in)
	if countText(got, "[r10+r11*4]") != 0 {
		t.Fatalf("must only fold the strictly-adjacent idiom: %v", got)
	}
}

// TestFoldLoadIntoBase: loading into r10 itself would destroy the SIB base.
func TestFoldLoadIntoBase(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\timul r11, 4"},
		{Kind: instInstr, Text: "\tadd r10, r11"},
		{Kind: instInstr, Text: "\tmov r10, [r10]"},
	}
	got := runFold(in)
	if countText(got, "[r10+r11*4]") != 0 {
		t.Fatalf("must not fold a load into the base register: %v", got)
	}
}

// TestFoldCallBreaksWindow: a call may clobber r10/r11 and never reads their
// values; the fold remains safe.
func TestFoldCallBreaksWindow(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\timul r11, 4"},
		{Kind: instInstr, Text: "\tadd r10, r11"},
		{Kind: instInstr, Text: "\tmov eax, [r10]"},
		{Kind: instInstr, Text: "\tcall foo"},
	}
	got := runFold(in)
	if countText(got, "mov eax, [r10+r11*4]") != 1 {
		t.Fatalf("call break must not prevent an otherwise-safe fold: %v", got)
	}
}

// TestFoldWriteReadsRegister: "mov r10, [r10]" both writes and reads r10 --
// the read observes the pre-fold value, so no fold.
func TestFoldWriteReadsRegister(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\timul r11, 4"},
		{Kind: instInstr, Text: "\tadd r10, r11"},
		{Kind: instInstr, Text: "\tmov eax, [r10]"},
		{Kind: instInstr, Text: "\tmov r10, [r10]"},
	}
	got := runFold(in)
	if countText(got, "[r10+r11*4]") != 0 {
		t.Fatalf("must not fold when a r10 write also reads r10: %v", got)
	}
}

// TestFoldDoubleStoreSource: a store whose source is r10 would race the SIB
// base read.
func TestFoldDoubleStoreSource(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\timul r11, 4"},
		{Kind: instInstr, Text: "\tadd r10, r11"},
		{Kind: instInstr, Text: "\tmov [r10], r10"},
	}
	got := runFold(in)
	if countText(got, "[r10+r11*4]") != 0 {
		t.Fatalf("must not fold a store whose source is the base: %v", got)
	}
}

// F2 end-to-end: an index that is a register-cached int loop variable is
// consumed straight from its callee-save home (movsxd r1Xd) and the index
// spill/reload disappears.
func TestF2IndexRegDirect(t *testing.T) {
	src := `int main(void) {
    int a[8];
    int s, i;
    s = 0;
    for (i = 0; i < 8; i++) {
        a[i] = i;
        s += a[i];
    }
    return s;
}`
	asm := genAsmOpt(t, src, 3)
	// fnAsm is deliberately not used here: goc emits loop labels at column 0
	// (".Lfor1:"), which fnAsm's top-level-label detection would stop at. The
	// _start stub cannot contain any of the asserted patterns, so asserting on
	// the whole assembly is safe.
	if strings.Count(asm, "movsxd r11, dword [rbp") != 0 {
		t.Fatalf("F2 must not spill/reload a register-cached index:\n%s", asm)
	}
	// at least one direct consumption from the index's home register (the
	// 32-bit name of a callee-save: ebx/r12d/r13d/r14d, or r8d/r9d in a leaf)
	if !strings.Contains(asm, "movsxd r11, ") {
		t.Fatalf("F2 must consume the index from its home register:\n%s", asm)
	}
	// the SIB fold should also fire on the loop's a[i] LOAD (imul/add/mov
	// are adjacent in the load path; the store path has the rhs evaluation
	// between add and the store, which sibFold deliberately leaves alone)
	if !strings.Contains(asm, "[r10+r11*4]") {
		t.Fatalf("sibFold must fold the loop's a[i] loads:\n%s", asm)
	}
}

// F2 disabled (independent switch): the spill/reload path comes back. At -O0
// the pass chain is off, so the fixture shows the codegen-level difference
// without sibFold's later folding of the same sequence.
func TestF2SwitchOff(t *testing.T) {
	old := f2IdxRegSkip
	f2IdxRegSkip = true
	defer func() { f2IdxRegSkip = old }()
	src := `int main(void) {
    int a[8];
    int i;
    for (i = 0; i < 8; i++)
        a[i] = i;
    return a[7];
}`
	asm := genAsmOpt(t, src, 0)
	if strings.Count(asm, "movsxd r11, dword [rbp") == 0 {
		t.Fatalf("with F2 off the index must spill and reload:\n%s", asm)
	}
}

// sibFold disabled (independent switch, end-to-end): with the switch on, the
// imul/add idiom survives the -O2 pipeline.
func TestSibFoldSwitchOff(t *testing.T) {
	old := sibFoldSkip
	sibFoldSkip = true
	defer func() { sibFoldSkip = old }()
	src := `int main(void) {
    int a[8];
    int i;
    for (i = 0; i < 8; i++)
        a[i] = i;
    return a[7];
}`
	asm := genAsmOpt(t, src, 3)
	if strings.Count(asm, "imul r11,") == 0 {
		t.Fatalf("with sibFoldSkip the idiom must survive the pipeline:\n%s", asm)
	}
	if strings.Contains(asm, "[r10+r11*4]") {
		t.Fatalf("with sibFoldSkip no SIB operand may appear:\n%s", asm)
	}
}
