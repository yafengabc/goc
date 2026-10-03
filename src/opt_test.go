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

// runSlot runs slotCache over the fixture and returns the surviving
// instruction texts.
func runSlot(insts []Inst) []string {
	out := slotCache(insts)
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
	// C5-b: the first pair collapses to the equivalent single `movsxd rax, eax`
	// (nothing reads its flags before the cmp), and the second pair is then
	// genuinely redundant and disappears entirely. So no shl survives at all,
	// where before C5 the first pair stayed as two instructions.
	if countText(got, "shl rax, 32") != 0 {
		t.Fatalf("flag writer must let the first pair collapse, got %v", got)
	}
	if countText(got, "movsxd rax, eax") != 1 {
		t.Fatalf("first pair must collapse to one movsxd, got %v", got)
	}
	if countText(got, "cmp rax, 0") != 1 {
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
	// The pair must NOT be dropped -- the narrow write cleared canonicality, so
	// the sign extension is still needed. C5-b: it is now emitted as the
	// equivalent single `movsxd` instead of `shl`+`sar`.
	if got := runExt(load); countText(got, "movsxd rax, eax") != 1 || countText(got, "shl rax, 32") != 0 {
		t.Fatalf("pair after mov eax,[m] must survive as one movsxd, got %v", got)
	}
	alu := []Inst{
		{Kind: instInstr, Text: "\tmov rax, 5"}, // canonical
		{Kind: instInstr, Text: "\txor eax, eax"},
	}
	alu = append(alu, mkPair("rax")...)
	alu = append(alu, Inst{Kind: instInstr, Text: "\tcmp rax, 0"})
	if got := runExt(alu); countText(got, "movsxd rax, eax") != 1 || countText(got, "shl rax, 32") != 0 {
		t.Fatalf("pair after xor eax,eax must survive as one movsxd, got %v", got)
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
	// And a user shift of the same shape stays untouched. Under T1.6 an int
	// shift emits the 32-bit form (shl eax, cl), which is NOT an IntWrap pair,
	// so elimRedundantExt must leave it alone.
	src2 := `int u(int a){ return (a << 32 >> 32) + 1; }
int main(){ return u(3); }`
	asm2 := genAsmOpt(t, src2, 2)
	if strings.Count(asm2, "shl eax, cl") < 1 {
		t.Fatalf("user shift must survive, got:\n%s", asm2)
	}
}

// mkSlot fixtures for T1.4 slotCache.
func slotStore(reg string) Inst  { return Inst{Kind: instInstr, Text: "\tmov [rbp-8], " + reg} }
func slotLoad(dst string) Inst   { return Inst{Kind: instInstr, Text: "\tmov " + dst + ", [rbp-8]"} }

// TestSlotForwardReg: a load of a slot known to hold register rS becomes a
// register move into the load's destination.
func TestSlotForwardReg(t *testing.T) {
	in := []Inst{slotStore("rax"), slotLoad("rcx")}
	got := runSlot(in)
	if countText(got, "mov rcx, rax") != 1 {
		t.Fatalf("store/load round trip must forward to mov rcx, rax, got %v", got)
	}
	if countText(got, "mov rcx, [rbp-8]") != 0 {
		t.Fatalf("slot load must be replaced, got %v", got)
	}
	if countText(got, "mov [rbp-8], rax") != 1 {
		t.Fatalf("store must survive, got %v", got)
	}
}

// TestSlotForwardSameRegDropped: reloading into the very register the slot
// holds is a self-move and is dropped entirely.
func TestSlotForwardSameRegDropped(t *testing.T) {
	in := []Inst{slotStore("rax"), slotLoad("rax")}
	got := runSlot(in)
	if countText(got, "mov rax, [rbp-8]") != 0 {
		t.Fatalf("same-reg reload must be dropped, got %v", got)
	}
	if countText(got, "mov [rbp-8], rax") != 1 {
		t.Fatalf("store must survive, got %v", got)
	}
}

// TestSlotClobberBlocksForward: writing the source register between store and
// load invalidates the cache; the load must stay a memory load.
func TestSlotClobberBlocksForward(t *testing.T) {
	in := []Inst{
		slotStore("rax"),
		{Kind: instInstr, Text: "\tmov rax, 5"},
		slotLoad("rcx"),
	}
	got := runSlot(in)
	if countText(got, "mov rcx, [rbp-8]") != 1 {
		t.Fatalf("clobbered source must block forwarding, got %v", got)
	}
	// 32-bit write clobbers the wide form too.
	in2 := []Inst{
		slotStore("rax"),
		{Kind: instInstr, Text: "\txor eax, eax"},
		slotLoad("rcx"),
	}
	if got := runSlot(in2); countText(got, "mov rcx, [rbp-8]") != 1 {
		t.Fatalf("narrow write must block forwarding, got %v", got)
	}
}

// TestSlotRedundantStoreDropped: storing the same register's value into a slot
// that already holds it is a no-op; storing a different value is kept.
func TestSlotRedundantStoreDropped(t *testing.T) {
	in := []Inst{slotStore("rax"), slotStore("rax")}
	got := runSlot(in)
	if countText(got, "mov [rbp-8], rax") != 1 {
		t.Fatalf("duplicate same-value store must be dropped, got %v", got)
	}
	in2 := []Inst{slotStore("rax"), slotStore("rbx")}
	got2 := runSlot(in2)
	if countText(got2, "mov [rbp-8], rax") != 1 || countText(got2, "mov [rbp-8], rbx") != 1 {
		t.Fatalf("stores of different values must survive, got %v", got2)
	}
}

// TestSlotImmForwardAndRedundant: an immediate store forwards to the load as
// an immediate and a same-immediate re-store is dropped.
func TestSlotImmForwardAndRedundant(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\tmov [rbp-8], 5"},
		slotLoad("rcx"),
	}
	got := runSlot(in)
	if countText(got, "mov rcx, 5") != 1 {
		t.Fatalf("imm slot load must forward to mov rcx, 5, got %v", got)
	}
	in2 := []Inst{
		{Kind: instInstr, Text: "\tmov [rbp-8], 5"},
		{Kind: instInstr, Text: "\tmov [rbp-8], 5"},
	}
	if got := runSlot(in2); countText(got, "mov [rbp-8], 5") != 1 {
		t.Fatalf("same-immediate re-store must be dropped, got %v", got)
	}
}

// TestSlotIndirectWriteClears: any non-slot memory write kills all slot
// knowledge (the write may alias any slot).
func TestSlotIndirectWriteClears(t *testing.T) {
	in := []Inst{
		slotStore("rax"),
		{Kind: instInstr, Text: "\tmov [r10], rbx"},
		slotLoad("rcx"),
	}
	got := runSlot(in)
	if countText(got, "mov rcx, [rbp-8]") != 1 {
		t.Fatalf("indirect write must clear the cache, got %v", got)
	}
}

// TestSlotSizedStoreClears: a partial (sized) write to the slot destroys the
// cached full-width value.
func TestSlotSizedStoreClears(t *testing.T) {
	in := []Inst{
		slotStore("rax"),
		{Kind: instInstr, Text: "\tmov byte [rbp-8], al"},
		slotLoad("rcx"),
	}
	got := runSlot(in)
	if countText(got, "mov rcx, [rbp-8]") != 1 {
		t.Fatalf("sized store must clear the slot, got %v", got)
	}
}

// TestSlotCallClears: calls clobber registers and memory; no forwarding across.
func TestSlotCallClears(t *testing.T) {
	in := []Inst{
		slotStore("rax"),
		{Kind: instInstr, Text: "\tcall foo"},
		slotLoad("rcx"),
	}
	got := runSlot(in)
	if countText(got, "mov rcx, [rbp-8]") != 1 {
		t.Fatalf("call must clear the cache, got %v", got)
	}
}

// TestSlotLeaEscapeClears: taking the slot's address lets any pointer write
// reach it, so all slot knowledge drops.
func TestSlotLeaEscapeClears(t *testing.T) {
	in := []Inst{
		slotStore("rax"),
		{Kind: instInstr, Text: "\tlea r10, [rbp-8]"},
		slotLoad("rcx"),
	}
	got := runSlot(in)
	if countText(got, "mov rcx, [rbp-8]") != 1 {
		t.Fatalf("lea of slot must clear the cache, got %v", got)
	}
}

// TestSlotForwardKillsDest: a forwarded load still writes its destination
// register, so cache entries sourced from it must be invalidated. Without
// this, the linux register-arg spill sequence (`mov [s],rax; mov rax,[t] ->
// mov rax,rY; mov r10,[s]`) forwarded the second reload through a clobbered
// rax and miscomputed.
func TestSlotForwardKillsDest(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\tmov [rbp-8], rax"},  // cache[8] = rax
		{Kind: instInstr, Text: "\tmov [rbp-16], rbx"}, // cache[16] = rbx
		{Kind: instInstr, Text: "\tmov rax, [rbp-16]"}, // forward -> mov rax, rbx; must kill rax
		{Kind: instInstr, Text: "\tmov r10, [rbp-8]"},  // rax clobbered: must NOT forward
	}
	got := runSlot(in)
	if countText(got, "mov r10, [rbp-8]") != 1 {
		t.Fatalf("reload through clobbered rax must stay a memory load, got %v", got)
	}
	if countText(got, "mov rax, rbx") != 1 {
		t.Fatalf("first forward must still happen, got %v", got)
	}
}

// TestSlotLabelClears: a label marks a control-flow merge; values cannot be
// trusted across it.
func TestSlotLabelClears(t *testing.T) {
	in := []Inst{
		slotStore("rax"),
		{Kind: instInstr, Text: ".L1:"},
		slotLoad("rcx"),
	}
	got := runSlot(in)
	if countText(got, "mov rcx, [rbp-8]") != 1 {
		t.Fatalf("label must clear the cache, got %v", got)
	}
}

// TestSlotNeverGrows: slotCache must never increase the instruction count.
func TestSlotNeverGrows(t *testing.T) {
	fixtures := [][]Inst{
		{slotStore("rax"), slotLoad("rcx")},
		{slotStore("rax"), slotStore("rax")},
		{slotStore("rax"), {Kind: instInstr, Text: "\tcall foo"}, slotLoad("rcx")},
		{slotStore("rax"), {Kind: instInstr, Text: "\tmov [r10], rbx"}, slotLoad("rcx")},
		{slotStore("rax"), slotStore("rbx"), slotStore("rcx"), slotLoad("rdx")},
	}
	for i, in := range fixtures {
		out := slotCache(in)
		if len(out) > len(in) {
			t.Fatalf("fixture %d grew: %d -> %d", i, len(in), len(out))
		}
	}
}

// TestSlotEndToEnd compiles the recursive ternary fib (the bench2 shape) at
// -O2.
//
// T2.1 (R2) changed the expected shape here. fib is recursive, so it is NOT
// an inline candidate, and its parameter is a register-capable scalar -- so it
// is homed in a callee-save register. The old expectation encoded the
// pre-R2 pipeline: a prologue spill to [rbp-48] with slotCache forwarding the
// reload into a register move. Now the prologue moves rcx straight into rbx and
// the branch-guarded reloads are register moves from the start, so the spill
// and every reload disappear rather than being forwarded.
func TestSlotEndToEnd(t *testing.T) {
	src := `int fib(int n) { return n < 2 ? n : fib(n-1) + fib(n-2); }
int main(){ return fib(10); }`
	asm := genAsmOpt(t, src, 2)
	body := fnAsm(asm, "fib")
	if !strings.Contains(body, "\tmov rbx, rcx\n") {
		t.Fatalf("param must be homed in a callee-save register, got:\n%s", body)
	}
	if strings.Contains(asm, "mov [rbp-48], rcx") {
		t.Fatalf("a register-homed param must not be spilled to a slot, got:\n%s", asm)
	}
	// The reloads inside the branches, previously memory loads that had to
	// survive slotCache's window, are now register moves from rbx; copyElim
	// then folds "mov rax, rbx; mov r10, rax" into "mov r10, rbx", so count
	// both the folded and the unfolded shape.
	if strings.Count(asm, "mov rax, rbx")+strings.Count(asm, "mov r10, rbx") < 2 {
		t.Fatalf("branch-guarded reloads must become register moves, got:\n%s", asm)
	}
}

// ---------- F4 slotCache value-snapshot (copy-chain) unit tests ----------

// mov64 builds a GP64-to-GP64 register move fixture.
func mov64(dst, src string) Inst {
	return Inst{Kind: instInstr, Text: "\tmov " + dst + ", " + src}
}

// TestSlotSnapshotChainForward: the headline F4 case. Store a copy of r12
// through rax, then clobber rax; the snapshot root (r12) survives, so the
// reload forwards from r12 instead of doing a real load.
func TestSlotSnapshotChainForward(t *testing.T) {
	in := []Inst{
		mov64("rax", "r12"),    // copyOf[rax] = r12
		slotStore("rax"),       // cache[8] = {reg: root(rax)=r12}
		{Kind: instInstr, Text: "\tmov rax, 5"}, // kill rax (not r12)
		slotLoad("rcx"),        // forward -> mov rcx, r12
	}
	got := runSlot(in)
	if countText(got, "mov rcx, r12") != 1 {
		t.Fatalf("chain-root forward must give mov rcx, r12, got %v", got)
	}
	if countText(got, "mov rcx, [rbp-8]") != 0 {
		t.Fatalf("slot load must be replaced, got %v", got)
	}
}

// TestSlotSnapshotRootKilled: if the chain root itself is written, the slot
// is invalidated and the reload stays a real memory load.
func TestSlotSnapshotRootKilled(t *testing.T) {
	in := []Inst{
		mov64("rax", "r12"),    // copyOf[rax] = r12
		slotStore("rax"),       // cache[8] = {reg: r12}
		{Kind: instInstr, Text: "\tmov r12, 5"}, // kill the ROOT
		slotLoad("rcx"),        // root dead: real load
	}
	got := runSlot(in)
	if countText(got, "mov rcx, [rbp-8]") != 1 {
		t.Fatalf("root killed must block forwarding, got %v", got)
	}
}

// TestSlotSnapshotNoChain: without a copy chain, the store source IS the root.
// Writing that register kills the slot (same as T1.4).
func TestSlotSnapshotNoChain(t *testing.T) {
	in := []Inst{
		slotStore("rax"),       // cache[8] = {reg: rax}
		{Kind: instInstr, Text: "\tmov rax, 5"}, // kill rax (root)
		slotLoad("rcx"),        // root dead: real load
	}
	got := runSlot(in)
	if countText(got, "mov rcx, [rbp-8]") != 1 {
		t.Fatalf("no-chain root killed must block forwarding, got %v", got)
	}
}

// TestSlotSnapshotChainTwoHop: a two-hop copy chain still resolves to the
// ultimate root. This is one of the three probe field cases.
func TestSlotSnapshotChainTwoHop(t *testing.T) {
	in := []Inst{
		mov64("rbx", "rcx"),    // copyOf[rbx] = rcx
		mov64("rax", "rbx"),    // copyOf[rax] = root(rbx)=rcx
		slotStore("rax"),       // cache[8] = {reg: rcx}
		{Kind: instInstr, Text: "\tmov rax, 2"}, // kill rax (not rcx)
		slotLoad("r10"),        // forward -> mov r10, rcx
	}
	got := runSlot(in)
	if countText(got, "mov r10, rcx") != 1 {
		t.Fatalf("two-hop chain must resolve to rcx, got %v", got)
	}
	if countText(got, "mov r10, [rbp-8]") != 0 {
		t.Fatalf("slot load must be replaced, got %v", got)
	}
}

// TestSlotSnapshotForwardKillsDst: a forwarded load writes dst, so the slot
// that was rooted at dst must be invalidated (sum7 discipline). The slot
// rooted at the surviving root still forwards.
func TestSlotSnapshotForwardKillsDst(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\tmov [rbp-8], r12"},   // cache[8] = {reg: r12}
		{Kind: instInstr, Text: "\tmov [rbp-16], rax"},  // cache[16] = {reg: rax}
		mov64("rcx", "r12"),                              // copyOf[rcx] = r12
		{Kind: instInstr, Text: "\tmov [rbp-24], rcx"},   // cache[24] = {reg: r12}
		// Forward [rbp-8] into rcx: rcx was a copy of r12, but now rcx is
		// overwritten by the forward. The slot [rbp-24] still roots at r12
		// (alive), so it should still forward.
		{Kind: instInstr, Text: "\tmov rcx, [rbp-8]"},   // forward -> mov rcx, r12
		{Kind: instInstr, Text: "\tmov r10, [rbp-24]"},  // root r12 alive: forward
	}
	got := runSlot(in)
	if countText(got, "mov r10, r12") != 1 {
		t.Fatalf("slot rooted at alive r12 must still forward, got %v", got)
	}
}

// TestSlotSnapshotNarrowWriteBreaksChain: a 32-bit write to the copy register
// kills that register but NOT the root. The slot rooted at the root still
// forwards.
func TestSlotSnapshotNarrowWriteBreaksChain(t *testing.T) {
	in := []Inst{
		mov64("rax", "r12"),    // copyOf[rax] = r12
		slotStore("rax"),       // cache[8] = {reg: r12}
		{Kind: instInstr, Text: "\txor eax, eax"}, // kill rax (32-bit), not r12
		slotLoad("rcx"),        // root r12 alive: forward -> mov rcx, r12
	}
	got := runSlot(in)
	if countText(got, "mov rcx, r12") != 1 {
		t.Fatalf("narrow write to copy must not break root forwarding, got %v", got)
	}
	if countText(got, "mov rcx, [rbp-8]") != 0 {
		t.Fatalf("slot load must be replaced, got %v", got)
	}
}

// TestSlotSnapshotSizedStoreInvalidates: a sized (partial) store to the slot
// clears the cached value even in snapshot mode.
func TestSlotSnapshotSizedStoreInvalidates(t *testing.T) {
	in := []Inst{
		mov64("rax", "r12"),
		slotStore("rax"),       // cache[8] = {reg: r12}
		{Kind: instInstr, Text: "\tmov dword [rbp-8], 0"}, // sized store: clobber
		slotLoad("rcx"),        // slot cleared: real load
	}
	got := runSlot(in)
	if countText(got, "mov rcx, [rbp-8]") != 1 {
		t.Fatalf("sized store must clear the slot, got %v", got)
	}
}

// TestSlotSnapshotLeaEscape: lea of a slot address clears all knowledge even
// in snapshot mode.
func TestSlotSnapshotLeaEscape(t *testing.T) {
	in := []Inst{
		mov64("rax", "r12"),
		slotStore("rax"),
		{Kind: instInstr, Text: "\tlea r10, [rbp-8]"},
		slotLoad("rcx"),
	}
	got := runSlot(in)
	if countText(got, "mov rcx, [rbp-8]") != 1 {
		t.Fatalf("lea escape must clear all, got %v", got)
	}
}

// TestSlotSnapshotIndirectWrite: an indirect (non-slot) memory write clears
// all knowledge even in snapshot mode.
func TestSlotSnapshotIndirectWrite(t *testing.T) {
	in := []Inst{
		mov64("rax", "r12"),
		slotStore("rax"),
		{Kind: instInstr, Text: "\tmov [r10], rbx"},
		slotLoad("rcx"),
	}
	got := runSlot(in)
	if countText(got, "mov rcx, [rbp-8]") != 1 {
		t.Fatalf("indirect write must clear all, got %v", got)
	}
}

// TestSlotSnapshotRedundantStoreRoot: storing through the same copy chain
// root twice is redundant (second store dropped).
func TestSlotSnapshotRedundantStoreRoot(t *testing.T) {
	in := []Inst{
		mov64("rax", "r12"),
		slotStore("rax"),       // cache[8] = {reg: r12}
		mov64("rcx", "r12"),    // copyOf[rcx] = r12
		slotStore("rcx"),       // root(rcx)=r12 == cache[8].reg: redundant
	}
	got := runSlot(in)
	if countText(got, "mov [rbp-8], ") != 1 {
		t.Fatalf("same-root re-store must be dropped, got %v", got)
	}
}

// ---------- F3-R copyElim (basic-block local copy elimination) unit tests ----------

// runCopyElim runs copyElim over the fixture and returns surviving texts.
func runCopyElim(insts []Inst) []string {
	out := copyElim(insts)
	texts := make([]string, 0, len(out))
	for _, in := range out {
		texts = append(texts, strings.TrimSpace(in.Text))
	}
	return texts
}

// TestCopyElimChainDrop: mov rax, r14; mov r10, rax; mov rax, 5 ->
// fold to mov r10, r14 (delete the first mov).
func TestCopyElimChainDrop(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\tmov rax, r14"},
		{Kind: instInstr, Text: "\tmov r10, rax"},
		{Kind: instInstr, Text: "\tmov rax, 5"},
	}
	got := runCopyElim(in)
	if countText(got, "mov r10, r14") != 1 {
		t.Fatalf("chain must fold to mov r10, r14, got %v", got)
	}
	if countText(got, "mov rax, r14") != 0 {
		t.Fatalf("dead copy must be deleted, got %v", got)
	}
}

// TestCopyElimStoreDrop: mov rax, r12; mov [rbp-8], rax; mov rax, 5 ->
// fold to mov [rbp-8], r12.
func TestCopyElimStoreDrop(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\tmov rax, r12"},
		{Kind: instInstr, Text: "\tmov [rbp-8], rax"},
		{Kind: instInstr, Text: "\tmov rax, 5"},
	}
	got := runCopyElim(in)
	if countText(got, "mov [rbp-8], r12") != 1 {
		t.Fatalf("store must fold to mov [rbp-8], r12, got %v", got)
	}
	if countText(got, "mov rax, r12") != 0 {
		t.Fatalf("dead copy must be deleted, got %v", got)
	}
}

// TestCopyElimMultiConsumerKeeps: mov rax, r14; mov r10, rax; mov rcx, rax
// -> second consumer keeps the mov (rax has a later read).
func TestCopyElimMultiConsumerKeeps(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\tmov rax, r14"},
		{Kind: instInstr, Text: "\tmov r10, rax"},
		{Kind: instInstr, Text: "\tmov rcx, rax"},
	}
	got := runCopyElim(in)
	// First mov consumed by r10 AND rcx — rax is read again (rcx) before
	// being written, so the mov is kept but consumers are substituted.
	if countText(got, "mov rax, r14") != 1 {
		t.Fatalf("mov must be kept (rax has later read), got %v", got)
	}
	if countText(got, "mov r10, r14") != 1 {
		t.Fatalf("first consumer must be substituted, got %v", got)
	}
	if countText(got, "mov rcx, r14") != 1 {
		t.Fatalf("second consumer must be substituted, got %v", got)
	}
}

// TestCopyElimLabelBreaks: mov chain across a label must not fold.
func TestCopyElimLabelBreaks(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\tmov rax, r14"},
		{Kind: instLabel, Text: ".L1:"},
		{Kind: instInstr, Text: "\tmov r10, rax"},
	}
	got := runCopyElim(in)
	if countText(got, "mov rax, r14") != 1 {
		t.Fatalf("label must break the window, got %v", got)
	}
	if countText(got, "mov r10, r14") != 0 {
		t.Fatalf("no substitution across label, got %v", got)
	}
}

// TestCopyElimCallBreaks: mov chain across a call must not fold.
func TestCopyElimCallBreaks(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\tmov rax, r14"},
		{Kind: instInstr, Text: "\tcall foo"},
		{Kind: instInstr, Text: "\tmov r10, rax"},
	}
	got := runCopyElim(in)
	if countText(got, "mov rax, r14") != 1 {
		t.Fatalf("call must break the window, got %v", got)
	}
}

// TestCopyElimDstReadWriteNoSub: add rax, rbx's op0 (rax) must NOT be
// substituted even though rax is in copyOf — the add writes rax.
func TestCopyElimDstReadWriteNoSub(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\tmov rax, r14"},
		{Kind: instInstr, Text: "\tadd rax, rbx"},
	}
	got := runCopyElim(in)
	// The mov is kept (next insn is add, not a mov consumer).
	if countText(got, "mov rax, r14") != 1 {
		t.Fatalf("mov must be kept for non-mov consumer, got %v", got)
	}
	// add rax's op0 must stay rax (not substituted to r14).
	if countText(got, "add r14, rbx") != 0 {
		t.Fatalf("add dst must not be substituted, got %v", got)
	}
}

// TestCopyElimDivImplied: div reads rax/rdx implicitly — backward scan
// must recognize that.
func TestCopyElimDivImplied(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\tmov rax, r14"},
		{Kind: instInstr, Text: "\tmov r10, rax"},
		{Kind: instInstr, Text: "\tidiv r11"},
	}
	got := runCopyElim(in)
	// The mov rax, r14; mov r10, rax: r10 consumes rax. But div after
	// implicitly reads rax (dividend). So rax is read before its next write
	// → mov must be kept.
	if countText(got, "mov rax, r14") != 1 {
		t.Fatalf("div reads rax implicitly: mov must be kept, got %v", got)
	}
}

// TestCopyElimXmmGpWrite: movq rbx, xmm0 / cvttsd2si rax, xmm0 write GP regs.
func TestCopyElimXmmGpWrite(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\tmovq rbx, xmm0"},
		{Kind: instInstr, Text: "\tmov r10, rbx"},
	}
	got := runCopyElim(in)
	// movq rbx, xmm0 writes rbx (fresh, unknown) — killReg rbx.
	// Then mov r10, rbx: rbx is killed, no chain, so real load.
	if countText(got, "movq rbx, xmm0") != 1 {
		t.Fatalf("movq must be kept, got %v", got)
	}
}

// TestCopyElimSubRegNoChain: mov eax, ebx does not build a chain.
func TestCopyElimSubRegNoChain(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\tmov eax, ebx"},
		{Kind: instInstr, Text: "\tmov r10, rax"},
	}
	got := runCopyElim(in)
	if countText(got, "mov eax, ebx") != 1 {
		t.Fatalf("32-bit mov must be kept, got %v", got)
	}
}

// TestCopyElimNarrowWriteBreaks: mov rax, r12; xor eax, eax; mov rcx, rax
// → xor kills rax (narrow write), rcx reads new rax value.
func TestCopyElimNarrowWriteBreaks(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\tmov rax, r12"},
		{Kind: instInstr, Text: "\txor eax, eax"},
		{Kind: instInstr, Text: "\tmov rcx, rax"},
	}
	got := runCopyElim(in)
	// xor eax kills rax (narrow write → reg64Name maps to rax).
	// After xor, rax=0 (fresh). mov rcx, rax should NOT fold to r12.
	if countText(got, "mov rcx, r12") != 0 {
		t.Fatalf("narrow write must break the chain, got %v", got)
	}
}

// ---------- T1.3 algebraic-identity unit tests (comparison-to-zero) ----------

// runAlg runs algebraicIdent over the fixture and returns the surviving
// instruction texts.
func runAlg(insts []Inst) []string {
	out := algebraicIdent(insts)
	texts := make([]string, 0, len(out))
	for _, in := range out {
		texts = append(texts, strings.TrimSpace(in.Text))
	}
	return texts
}

// TestAlgImmZeroJe: cmp rax, 0 followed by a ZF read (je) rewrites to
// `test rax, rax`.
func TestAlgImmZeroJe(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\tcmp rax, 0"},
		{Kind: instInstr, Text: "\tje .Lx"},
	}
	got := runAlg(in)
	if countText(got, "test rax, rax") != 1 {
		t.Fatalf("cmp rax,0 ; je must become test rax,rax, got %v", got)
	}
	if countText(got, "cmp rax, 0") != 0 {
		t.Fatalf("cmp rax,0 must be gone, got %v", got)
	}
}

// TestAlgRegZeroJne: cmp r10, rax where rax is known-zero (xor eax,eax), then
// jne, rewrites to `test r10, r10` -- the second operand is a tracked zero reg.
func TestAlgRegZeroJne(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\txor eax, eax"},
		{Kind: instInstr, Text: "\tcmp r10, rax"},
		{Kind: instInstr, Text: "\tjne .Ly"},
	}
	got := runAlg(in)
	if countText(got, "test r10, r10") != 1 {
		t.Fatalf("cmp r10,rax(zero) ; jne must become test r10,r10, got %v", got)
	}
}

// TestAlgUnsignedKept: cmp rax, 0 followed by an unsigned read (ja) must NOT
// rewrite -- `test` clears CF, so the unsigned branch would misread it.
func TestAlgUnsignedKept(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\tcmp rax, 0"},
		{Kind: instInstr, Text: "\tja .Lx"},
	}
	got := runAlg(in)
	if countText(got, "cmp rax, 0") != 1 {
		t.Fatalf("unsigned read must keep cmp, got %v", got)
	}
	if countText(got, "test rax, rax") != 0 {
		t.Fatalf("unsigned read must not become test, got %v", got)
	}
}

// TestAlgOverwrittenRegKept: rax is zeroed, then clobbered by `mov rax,5`,
// so a later `cmp r10, rax` must NOT be rewritten -- the second operand is no
// longer a known zero.
func TestAlgOverwrittenRegKept(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\txor eax, eax"},
		{Kind: instInstr, Text: "\tmov rax, 5"},
		{Kind: instInstr, Text: "\tcmp r10, rax"},
		{Kind: instInstr, Text: "\tje .Lx"},
	}
	got := runAlg(in)
	if countText(got, "cmp r10, rax") != 1 {
		t.Fatalf("clobbered second operand must keep cmp, got %v", got)
	}
	if countText(got, "test r10, r10") != 0 {
		t.Fatalf("clobbered second operand must not become test, got %v", got)
	}
}

// TestAlgNonFlagReadKept: cmp rax,0 not followed by a flag read must stay cmp.
func TestAlgNonFlagReadKept(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\tcmp rax, 0"},
		{Kind: instInstr, Text: "\tmov rbx, rax"},
	}
	got := runAlg(in)
	if countText(got, "cmp rax, 0") != 1 {
		t.Fatalf("non-flag-read successor must keep cmp, got %v", got)
	}
}

// fnAsmFull extracts a full top-level function body, stopping only at the
// next NON-internal top-level label (one that does not start with '.'), so the
// extracted region includes the compiler's internal .L* labels instead of
// being truncated at the first one (which fnAsm does).
func fnAsmFull(asm, name string) string {
	start := strings.Index(asm, name+":")
	if start < 0 {
		return asm
	}
	rest := asm[start:]
	lines := strings.Split(rest, "\n")
	for i := 1; i < len(lines); i++ {
		ln := lines[i]
		if ln == "" {
			continue
		}
		if ln[0] == '\t' || ln[0] == ' ' {
			continue
		}
		if strings.HasSuffix(ln, ":") && !strings.HasPrefix(ln, ".") {
			return strings.Join(lines[:i], "\n")
		}
	}
	return rest
}

// TestAlgEndToEnd: a small equality-against-zero function must lower its
// compare-to-zero to `test r,r` at -O1. Here `if (x==0)` emits
// `cmp r10, rax` (rax materialised as zero) which the pass turns into
// `test r10, r10`; the immediate form `cmp rax, 0` must not survive either.
func TestAlgEndToEnd(t *testing.T) {
	src := `int isz(int x){ if (x == 0) return 1; return 0; }
int main(){ return isz(0); }`
	asm := genAsmOpt(t, src, 1)
	body := fnAsmFull(asm, "isz")
	if strings.Contains(body, "cmp rax, 0") {
		t.Fatalf("isz must not contain cmp rax,0, got:\n%s", body)
	}
	if strings.Contains(body, "cmp r10, rax") || strings.Contains(body, "cmp rbx, rax") ||
		strings.Contains(body, "cmp rcx, rax") || strings.Contains(body, "cmp rdx, rax") {
		t.Fatalf("isz compare-against-zero must lower to test, got:\n%s", body)
	}
	if !strings.Contains(body, "test ") {
		t.Fatalf("isz must contain a test instruction, got:\n%s", body)
	}
}

// TestC2IntCarryModel pins the T1.6 C2 invariants: int inc/dec steps in 32-bit
// (no canonInt shl/sar round-trip), signed int indexes sign-extend into the
// 64-bit scale-and-add, and int switch dispatch compares 32-bit so a negative
// case constant matches the materialized value.
func TestC2IntCarryModel(t *testing.T) {
	// N7/N8: an int inc/dec (memory-backed here) emits a 32-bit step and no
	// canonInt pair.
	src1 := `int f(int x){ x++; --x; return x; }
int main(){ return f(3); }`
	asm1 := genAsmOpt(t, src1, 2)
	body1 := fnAsm(asm1, "f")
	if strings.Contains(body1, "shl rax, 32") || strings.Contains(body1, "sar rax, 32") {
		t.Fatalf("int inc/dec must not canonicalise, got:\n%s", body1)
	}
	if !strings.Contains(body1, "inc eax") && !strings.Contains(body1, "inc rax") {
		t.Fatalf("int inc/dec must step the loaded value, got:\n%s", body1)
	}
	// N11: a signed int negative index sign-extends before the 64-bit add.
	src2 := `int f(int *p){ return p[-2]; }
int main(){ int a[8]; return f(a+4); }`
	asm2 := genAsmOpt(t, src2, 2)
	body2 := fnAsm(asm2, "f")
	if !strings.Contains(body2, "movsxd r11, dword [rbp") {
		t.Fatalf("signed int index must sign-extend, got:\n%s", body2)
	}
	// N21: an int switch compares 32-bit (negative case constants match the
	// materialized value, whose high 32 bits are zero).
	src3 := `int f(int x){ switch(x){ case -1: return 7; default: return 0; } }
int main(){ return f(-1); }`
	asm3 := genAsmOpt(t, src3, 2)
	body3 := fnAsm(asm3, "f")
	if !strings.Contains(body3, "cmp eax, -1") {
		t.Fatalf("int switch must compare eax against the case constant, got:\n%s", body3)
	}
}

// TestC3ABIMaterialized pins the T1.6 C3 ABI: an int argument passed to an int
// parameter travels materialized (no movsxd at the call site -- the callee's
// int consumption is 32-bit-aware), while an int argument passed to a LONG
// parameter is a C widening conversion and must still sign-extend. A "(long)x"
// cast of a materialized int must also sign-extend (the pre-C3 call-site
// canonicalization used to mask that site).
func TestC3ABIMaterialized(t *testing.T) {
	// int -> int param: no movsxd when marshalling the argument.
	src1 := `int g(int x){ return x; }
int f(int a){ return g(a) + 1; }
int main(){ return f(-3); }`
	asm1 := genAsmOpt(t, src1, 2)
	body1 := fnAsm(asm1, "f")
	if strings.Contains(body1, "movsxd rax, eax") {
		t.Fatalf("int arg to int param must not sign-extend, got:\n%s", body1)
	}
	// int -> long param: the widening conversion must sign-extend.
	src2 := `long g(long x){ return x; }
long f(int a){ return g(a); }
int main(){ return f(-3); }`
	asm2 := genAsmOpt(t, src2, 2)
	body2 := fnAsm(asm2, "f")
	if !strings.Contains(body2, "movsxd rax, eax") {
		t.Fatalf("int arg to long param must sign-extend (widening), got:\n%s", body2)
	}
	// "(long)x" cast of a materialized signed int must sign-extend.
	src3 := `long f(int a){ return (long)a * 100; }
int main(){ return f(-7); }`
	asm3 := genAsmOpt(t, src3, 2)
	body3 := fnAsm(asm3, "f")
	if !strings.Contains(body3, "movsxd rax, eax") {
		t.Fatalf("(long) cast of materialized int must sign-extend, got:\n%s", body3)
	}
}

// TestCopyElimBlockWideChain: a GP-GP mov with two mov consumers before the
// next rewrite of D is deleted; both consumers read the root.
func TestCopyElimBlockWideChain(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\tmov rax, r12"},
		{Kind: instInstr, Text: "\tmov r10, rax"},
		{Kind: instInstr, Text: "\tmov r11, rax"},
		{Kind: instInstr, Text: "\tmov rax, 5"},
	}
	got := runCopyElim(in)
	if countText(got, "mov rax, r12") != 0 {
		t.Fatalf("mov must be deleted, got %v", got)
	}
	if countText(got, "mov r10, r12") != 1 || countText(got, "mov r11, r12") != 1 {
		t.Fatalf("consumers must read the root, got %v", got)
	}
}

// TestCopyElimBlockWideStore: a store consumer before the rewrite is
// substituted and the mov deleted.
func TestCopyElimBlockWideStore(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\tmov rax, r12"},
		{Kind: instInstr, Text: "\tmov [rbp-8], rax"},
		{Kind: instInstr, Text: "\tmov rax, 5"},
	}
	got := runCopyElim(in)
	if countText(got, "mov rax, r12") != 0 {
		t.Fatalf("mov must be deleted, got %v", got)
	}
	if countText(got, "mov [rbp-8], r12") != 1 {
		t.Fatalf("store consumer must read the root, got %v", got)
	}
}

// TestCopyElimBlockWideAbortNonMov: a non-mov read of D aborts the deletion.
func TestCopyElimBlockWideAbortNonMov(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\tmov rax, r12"},
		{Kind: instInstr, Text: "\tmov r10, rax"},
		{Kind: instInstr, Text: "\tadd r11, rax"},
		{Kind: instInstr, Text: "\tmov rax, 5"},
	}
	got := runCopyElim(in)
	if countText(got, "mov rax, r12") != 1 {
		t.Fatalf("non-mov consumer must abort deletion, got %v", got)
	}
}

// TestCopyElimBlockWideAbortSrcWrite: a write of src before the consumer
// aborts the deletion.
func TestCopyElimBlockWideAbortSrcWrite(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\tmov rax, r12"},
		{Kind: instInstr, Text: "\tmov r10, rax"},
		{Kind: instInstr, Text: "\tmov r12, 7"},
		{Kind: instInstr, Text: "\tmov r11, rax"},
	}
	got := runCopyElim(in)
	if countText(got, "mov rax, r12") != 1 {
		t.Fatalf("src write must abort deletion, got %v", got)
	}
}

// TestCopyElimBlockWideUnterminated: a window ending at a block boundary
// without D being rewritten must not delete (D may be read later).
func TestCopyElimBlockWideUnterminated(t *testing.T) {
	in := []Inst{
		{Kind: instInstr, Text: "\tmov rax, r12"},
		{Kind: instInstr, Text: "\tmov r10, rax"},
		{Kind: instInstr, Text: "\tjmp .L1"},
		{Kind: instLabel, Text: ".L1:"},
	}
	got := runCopyElim(in)
	if countText(got, "mov rax, r12") != 1 {
		t.Fatalf("unterminated window must not delete, got %v", got)
	}
}
