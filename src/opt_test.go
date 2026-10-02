// T1.6 elimRedundantExt unit tests. These exercise the pass directly on
// instruction fixtures (the marked-pair rules, flag discipline, slot/register
// canonicality) plus one end-to-end compile asserting the marker survives
// inlineCalls.
package main

import (
	"strings"
	"testing"
)

// mkI builds a marked wrap pair on reg r: shl r,32; sar r,32 (IntWrap).
func mkPair(r string) []Inst {
	return []Inst{
		{Kind: instInstr, Text: "\tshl " + r + ", 32", IntWrap: true},
		{Kind: instInstr, Text: "\tsar " + r + ", 32", IntWrap: true},
	}
}

// runExt runs elimRedundantExt over the fixture and returns the surviving
// instruction texts.
func runExt(insts []Inst) []string {
	out := elimRedundantExt(insts)
	texts := make([]string, 0, len(out))
	for _, in := range out {
		texts = append(texts, strings.TrimSpace(in.Text))
	}
	return texts
}

func countText(texts []string, sub string) int {
	n := 0
	for _, t := range texts {
		if strings.Contains(t, sub) {
			n++
		}
	}
	return n
}

// TestExtAdjacentPairDropped: two adjacent marked pairs canonicalise an
// already-canonical value; the second pair is a no-op and must go.
func TestExtAdjacentPairDropped(t *testing.T) {
	in := append([]Inst{
		{Kind: instInstr, Text: "\tadd rax, r10"},
	}, mkPair("rax")...)
	in = append(in, mkPair("rax")...)
	got := runExt(in)
	if countText(got, "shl rax, 32") != 1 || countText(got, "sar rax, 32") != 1 {
		t.Fatalf("adjacent double pair must collapse to one, got %v", got)
	}
	if got[0] != "add rax, r10" {
		t.Fatalf("op before the pairs must survive, got %v", got)
	}
}

// TestExtUserShiftKept: an unmarked shl/sar pair is a user shift and must
// never be touched, even when canonicality would otherwise allow it.
func TestExtUserShiftKept(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\tmov rax, 5"},
		{Kind: instInstr, Text: "\tshl rax, 32"},
		{Kind: instInstr, Text: "\tsar rax, 32"},
	}
	got := runExt(in)
	if countText(got, "shl rax, 32") != 1 || countText(got, "sar rax, 32") != 1 {
		t.Fatalf("unmarked user shift must stay, got %v", got)
	}
}

// TestExtFlagReaderBlocks: a flag-reading branch between a kept pair and a
// candidate pair makes the deletion unsafe (the branch reads the pair's
// flags), so the candidate pair must survive.
func TestExtFlagReaderBlocks(t *testing.T) {
	in := append(mkPair("rax"), []Inst{
		{Kind: instInstr, Text: "\tje .Lx"},
	}...)
	in = append(in, mkPair("rax")...)
	got := runExt(in)
	if countText(got, "shl rax, 32") != 2 {
		t.Fatalf("flag-reading branch must block deletion, got %v", got)
	}
}

// TestExtFlagWriterAllows: when a cmp (a flag writer) sits between the kept
// pair and the candidate pair, no flag read can observe the candidate pair's
// flags, so it is deleted. The store between the pairs breaks output
// adjacency (forcing the lookahead path) but preserves canonicality.
func TestExtFlagWriterAllows(t *testing.T) {
	in := append(mkPair("rax"), []Inst{
		{Kind: instInstr, Text: "\tmov [rbp-16], rax"},
	}...)
	in = append(in, mkPair("rax")...)
	in = append(in, Inst{Kind: instInstr, Text: "\tcmp rax, 0"})
	got := runExt(in)
	if countText(got, "shl rax, 32") != 1 || countText(got, "cmp rax, 0") != 1 {
		t.Fatalf("flag writer must allow deletion, got %v", got)
	}
	if countText(got, "mov [rbp-16], rax") != 1 {
		t.Fatalf("store between pairs must survive, got %v", got)
	}
}

// TestExtSlotRoundTrip: a canonical store followed by a load of the same slot
// carries canonicality, so a marked pair after the load is redundant.
func TestExtSlotRoundTrip(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\tmov rax, 5"}, // int32-fit immediate: canonical
		{Kind: instInstr, Text: "\tmov [rbp-8], rax"},
		{Kind: instInstr, Text: "\tmov rbx, [rbp-8]"},
		{Kind: instInstr, Text: "\tmov rax, rbx"},
	}
	in = append(in, mkPair("rax")...)
	in = append(in, Inst{Kind: instInstr, Text: "\tcmp rax, 0"}) // flag writer
	got := runExt(in)
	if countText(got, "shl rax, 32") != 0 {
		t.Fatalf("canonical slot round trip must drop the pair, got %v", got)
	}
	for _, keep := range []string{"mov [rbp-8], rax", "mov rbx, [rbp-8]", "mov rax, rbx", "cmp rax, 0"} {
		if countText(got, keep) != 1 {
			t.Fatalf("instruction %q must survive, got %v", keep, got)
		}
	}
}

// TestExtNonCanonicalSlotKeepsPair: an indirect load puts unknown value into
// rax; the marked pair after it is a real canonicalisation and must stay.
func TestExtNonCanonicalSlotKeepsPair(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\tmov rax, [r10]"}, // indirect: canonicality unknown
	}
	in = append(in, mkPair("rax")...)
	got := runExt(in)
	if countText(got, "shl rax, 32") != 1 {
		t.Fatalf("pair after unknown load must stay, got %v", got)
	}
}

// TestExtSizedStoresSurvive: narrow stores (with and without size prefix,
// to a slot and to an indirect address) must never be dropped.
func TestExtSizedStoresSurvive(t *testing.T) {
	in := append(mkPair("rax"), []Inst{
		{Kind: instInstr, Text: "\tmov [rbp-16], rax"},
		{Kind: instInstr, Text: "\tmov dword [r10], eax"},
		{Kind: instInstr, Text: "\tmov byte [rbp-3], al"},
		{Kind: instInstr, Text: "\tmov [r11], rax"},
	}...)
	got := runExt(in)
	for _, keep := range []string{"mov [rbp-16], rax", "mov dword [r10], eax", "mov byte [rbp-3], al", "mov [r11], rax"} {
		if countText(got, keep) != 1 {
			t.Fatalf("store %q must survive, got %v", keep, got)
		}
	}
}

// TestExtUnsignedPairKept: the unsigned canonicalisation (shl/shr) is not
// marked and is a real 32-bit zero-extension of a 64-bit value -- never
// deletable by this pass.
func TestExtUnsignedPairKept(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\tmov rax, 5"},
		{Kind: instInstr, Text: "\tmov rax, [rbp-24]"},
		{Kind: instInstr, Text: "\tshl rax, 32"},
		{Kind: instInstr, Text: "\tshr rax, 32"},
	}
	got := runExt(in)
	if countText(got, "shl rax, 32") != 1 || countText(got, "shr rax, 32") != 1 {
		t.Fatalf("unsigned pair must stay, got %v", got)
	}
}

// TestExtCallBreaksWindow: knowledge does not survive a call; the candidate
// pair after the call must stay.
func TestExtCallBreaksWindow(t *testing.T) {
	in := append(mkPair("rax"), []Inst{
		{Kind: instInstr, Text: "\tcall foo"},
	}...)
	in = append(in, mkPair("rax")...)
	got := runExt(in)
	if countText(got, "shl rax, 32") != 2 {
		t.Fatalf("call must break the window, got %v", got)
	}
}

// TestExtNarrowDestClearsCanon: a 32-bit destination write (mov eax, [m],
// xor eax, eax) zero-extends into the wide register; the wide register's
// canonicality must be cleared so a following marked pair stays.
func TestExtNarrowDestClearsCanon(t *testing.T) {
	load := []Inst{
		{Kind: instInstr, Text: "\tmov rax, 5"}, // canonical
		{Kind: instInstr, Text: "\tmov eax, [r10]"},
	}
	load = append(load, mkPair("rax")...)
	load = append(load, Inst{Kind: instInstr, Text: "\tcmp rax, 0"})
	if got := runExt(load); countText(got, "shl rax, 32") != 1 {
		t.Fatalf("pair after mov eax,[m] must stay, got %v", got)
	}
	alu := []Inst{
		{Kind: instInstr, Text: "\tmov rax, 5"}, // canonical
		{Kind: instInstr, Text: "\txor eax, eax"},
	}
	alu = append(alu, mkPair("rax")...)
	alu = append(alu, Inst{Kind: instInstr, Text: "\tcmp rax, 0"})
	if got := runExt(alu); countText(got, "shl rax, 32") != 1 {
		t.Fatalf("pair after xor eax,eax must stay, got %v", got)
	}
}

// TestExtNeverGrows: across the fixtures above the pass must never increase
// the instruction count (the plan's instruction-count static assertion).
func TestExtNeverGrows(t *testing.T) {
	fixtures := [][]Inst{
		append(mkPair("rax"), mkPair("rax")...),
		[]Inst{{Kind: instInstr, Text: "\tmov rax, 5"}, {Kind: instInstr, Text: "\tshl rax, 32"}, {Kind: instInstr, Text: "\tsar rax, 32"}},
		append(append(mkPair("rax"), Inst{Kind: instInstr, Text: "\tje .Lx"}), mkPair("rax")...),
		[]Inst{{Kind: instInstr, Text: "\tmov dword [r10], eax"}, {Kind: instInstr, Text: "\tmov byte [rbp-3], al"}},
		append(append(mkPair("rax"), Inst{Kind: instInstr, Text: "\tcall foo"}), mkPair("rax")...),
	}
	for i, in := range fixtures {
		out := elimRedundantExt(in)
		if len(out) > len(in) {
			t.Fatalf("fixture %d grew: %d -> %d", i, len(in), len(out))
		}
	}
}

// TestExtEndToEndDoubleCast compiles a function whose (int) cast emits two
// adjacent canonicalisation pairs and asserts the pass collapses them -- this
// proves the markers survive inlineCalls (the inlined copy carries IntWrap)
// and the full pipeline fires.
func TestExtEndToEndDoubleCast(t *testing.T) {
	src := `int h(int a){ return (int)(a + 1); }
int main(){ return h(3); }`
	asm := genAsmOpt(t, src, 2)
	body := fnAsm(asm, "h")
	if !strings.Contains(body, "\tshl rax, 32") {
		t.Fatalf("expected one surviving pair in h, got:\n%s", body)
	}
	if strings.Contains(body, "shl rax, 32\n\tsar rax, 32\n\tshl rax, 32") {
		t.Fatalf("adjacent double pair must collapse, got:\n%s", body)
	}
	if strings.Count(body, "shl rax, 32") > 1 {
		t.Fatalf("expected exactly one shl pair in h, got:\n%s", body)
	}
	// And a user shift of the same shape stays untouched.
	src2 := `int u(int a){ return (a << 32 >> 32) + 1; }
int main(){ return u(3); }`
	asm2 := genAsmOpt(t, src2, 2)
	if strings.Count(asm2, "shl rax, 32") < 2 {
		t.Fatalf("user shift must survive, got:\n%s", asm2)
	}
}
