package compiler

import (
	"strings"
	"testing"
)

// funcBody extracts the assembly for the named function from a full asm dump.
func funcBody(asm, name string) string {
	lines := strings.Split(asm, "\n")
	var b strings.Builder
	in := false
	for _, l := range lines {
		if strings.HasPrefix(l, name+":") {
			in = true
			continue
		}
		if in {
			if l == "" {
				continue
			}
			if strings.HasPrefix(l, name+":") {
				break
			}
			// a new top-level label (no tab indent) ends the function
			if !strings.HasPrefix(l, "\t") && !strings.HasPrefix(l, " ") && len(l) > 0 && l[len(l)-1] == ':' {
				// could be a local label like .Lfoo: -- those are indented differently;
				// top-level function labels start at column 0 and end with ':'.
				// Local labels also start at column 0 in goa output, so only break
				// on the next function label (same name pattern as a decl).
				if !strings.HasPrefix(l, ".") {
					break
				}
			}
			b.WriteString(l)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// TestF2ExtIndexConstAdd: a[i+1] with i register-cached must fold the +1 into
// the final scaled-index addressing ("lea r10, [r10 + r11*4 + 4]") instead of
// materialising i+1 as a spilled value ("movsxd r11, dword [rbp...]" from a
// slot). i must be a register-cached LOCAL (not a parameter, which is
// intentionally kept on the stack when the function is inline-likely).
func TestF2ExtIndexConstAdd(t *testing.T) {
	src := `int f(int a[], int n) { int i = n; return a[i+1]; }
int main(void) { int x[4]; return f(x, 0); }`
	asm := genAsmOpt(t, src, 1)
	body := funcBody(asm, "f")
	if !strings.Contains(body, "lea r10, [r10 + r11*4 + 4]") {
		t.Fatalf("F2-EXT must fold +1 into the scaled-index lea;\n%s", body)
	}
	if strings.Contains(body, "movsxd r11, dword [rbp") {
		t.Fatalf("F2-EXT must NOT spill i+1 to a slot;\n%s", body)
	}
}

// TestF2ExtIndexConstSub: a[i-1] folds to the same lea with a -4 displacement.
func TestF2ExtIndexConstSub(t *testing.T) {
	src := `int f(int a[], int n) { int i = n; return a[i-1]; }
int main(void) { int x[4]; return f(x, 1); }`
	asm := genAsmOpt(t, src, 1)
	body := funcBody(asm, "f")
	if !strings.Contains(body, "lea r10, [r10 + r11*4 - 4]") {
		t.Fatalf("F2-EXT must fold -1 into the scaled-index lea;\n%s", body)
	}
}

// TestF2ExtIndexConstGuardrail: the fold must NOT fire at -O0, preserving the
// byte-identical -O0 output contract.
func TestF2ExtIndexConstGuardrail(t *testing.T) {
	src := `int f(int a[], int i) { return a[i+1]; }
int main(void) { int x[4]; return f(x, 0); }`
	asm := genAsmOpt(t, src, 0)
	body := funcBody(asm, "f")
	if strings.Contains(body, "lea r10, [r10 + r11*4 + 4]") {
		t.Fatalf("-O0 must not apply F2-EXT;\n%s", body)
	}
}
