package main

import "testing"

// TestLiteralDBComma guards against stripComment treating a ';' inside a
// string literal as a comment start (the ",;." corruption). Assembling these
// db lines must keep LC0 (",;.") intact at .rdata offset 0.
func TestLiteralDBComma(t *testing.T) {
	a := NewAssembler()
	src := `section .rdata
LC0 db ",;.", 0
LC1 db "A strchr(t in ,;.)=%d\n", 0
LC2 db "d0=%d d1=%d d2=%d d3=%d len=%d\n", 0
LC3 db "dstr=%s\n", 0
`
	if err := a.Assemble(src); err != nil {
		t.Fatalf("assemble: %v", err)
	}
	var got string
	for _, s := range a.sections {
		if s.Name == ".rdata" || s.Name == ".data" {
			got = string(s.Data)
			break
		}
	}
	want := ",;.\x00A strchr(t in ,;.)=%d\n\x00d0=%d d1=%d d2=%d d3=%d len=%d\n\x00dstr=%s\n\x00"
	if got != want {
		t.Errorf("rdata mismatch:\n got %q\nwant %q", got, want)
	}
}

// TestArithMemSrc verifies that arithmetic instructions accept a memory source
// operand ("add eax, [rbp-16]"), which the inline-asm binder emits when a C
// variable is read. Both register-based and RIP-relative memory are covered.
func TestArithMemSrc(t *testing.T) {
	a := NewAssembler()
	src := `section .text
add eax, [rbp-16]
add rax, [rbp-16]
add eax, [rip+G_g]
sub eax, [rbp-16]
and eax, [rbp-16]
or  eax, [rbp-16]
xor eax, [rbp-16]
cmp eax, [rbp-16]
section .data
G_g dq 0
`
	if err := a.Assemble(src); err != nil {
		t.Fatalf("assemble: %v", err)
	}
	var got []byte
	for _, s := range a.sections {
		if s.Name == ".text" {
			got = s.Data
			break
		}
	}
	want := []byte{
		0x03, 0x45, 0xF0, // add eax, [rbp-16]  (32-bit, no REX.W)
		0x48, 0x03, 0x45, 0xF0, // add rax, [rbp-16]  (REX.W)
		0x03, 0x05, 0, 0, 0, 0, // add eax, [rip+G_g]  (fixup fills disp32)
		0x2B, 0x45, 0xF0, // sub eax, [rbp-16]
		0x23, 0x45, 0xF0, // and eax, [rbp-16]
		0x0B, 0x45, 0xF0, // or  eax, [rbp-16]
		0x33, 0x45, 0xF0, // xor eax, [rbp-16]
		0x3B, 0x45, 0xF0, // cmp eax, [rbp-16]
	}
	if len(got) != len(want) {
		t.Fatalf("text len = %d, want %d\n got %x\nwant %x", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("text[%d] = 0x%02x, want 0x%02x\n got %x\nwant %x", i, got[i], want[i], got, want)
		}
	}
}

// TestArithMemSrcBad guards the error path: a memory *destination* (or any
// other non-register dst) is rejected, and an unknown src form is too.
func TestArithMemSrcBad(t *testing.T) {
	cases := []string{
		"add rax, r9", // reg,reg still fine
	}
	bad := []string{
		"add [rbp-8], eax",    // mem dst not supported (store direction)
		"add [rbp-8], 5",      // mem dst with imm
		"add rax, [rbp-16]*4", // malformed mem
		"add rax, bogusreg",   // unknown register => not a valid operand
	}
	for _, src := range cases {
		a := NewAssembler()
		if err := a.Assemble("section .text\n" + src + "\n"); err != nil {
			t.Errorf("unexpected error for %q: %v", src, err)
		}
	}
	for _, src := range bad {
		a := NewAssembler()
		if err := a.Assemble("section .text\n" + src + "\n"); err == nil {
			t.Errorf("expected error for %q", src)
		}
	}
}

// TestStripCommentInString guards stripComment against ';' and "//" inside
// string literals, and against an escaped quote closing the string early.
func TestStripCommentInString(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`LC0 db ",;.", 0`, `LC0 db ",;.", 0`},
		{`LC1 db "A strchr(t in ,;.)=%d\n", 0`, `LC1 db "A strchr(t in ,;.)=%d\n", 0`},
		{`LC2 db "say \"hi\"; bye", 0`, `LC2 db "say \"hi\"; bye", 0`},
		{`mov rax, 5 ; comment`, `mov rax, 5 `},
		{`db "path//x", 0`, `db "path//x", 0`},
		{`mov rax, 5 // comment`, `mov rax, 5 `},
	}
	for _, c := range cases {
		if got := stripComment(c.in); got != c.want {
			t.Errorf("stripComment(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestLiteralDBEscapedQuote guards splitTopLevel's escape handling. It used to
// end a db operand's string at the first quote whether or not that quote was
// backslash-escaped, so a literal holding JSON -- "a\\\\\"b,c" -- had its
// quoting state desynchronised and the comma inside the string was split off
// as a separate operand ("bad db operand").
func TestLiteralDBEscapedQuote(t *testing.T) {
	a := NewAssembler()
	src := "section .rdata\n" +
		"LC0 db \"a\\\\\\\"b,c\", 0\n" +
		"LC1 db \"q\\\"b\\\\\\\\f\\nb\", 0\n"
	if err := a.Assemble(src); err != nil {
		t.Fatalf("assemble: %v", err)
	}
	var got string
	for _, s := range a.sections {
		if s.Name == ".rdata" || s.Name == ".data" {
			got = string(s.Data)
			break
		}
	}
	// LC0: a, backslash, quote, b, comma, c -- the comma belongs to the string
	// LC1: q, quote, b, backslash, backslash, f, backslash, n, b
	want := "a\\\"b,c\x00q\"b\\\\f\nb\x00"
	if got != want {
		t.Errorf("rdata mismatch:/n got %q\nwant %q", got, want)
	}
}
