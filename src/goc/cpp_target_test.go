package compiler

import (
	"goc/common"
	"goc/frontend"
	"strings"
	"testing"
)

// Regression for the doElif fix: once an #if branch is taken, later #elif
// branches must stay inactive. Also locks in the platform macros injected by
// PreprocessTarget (_WIN32/_WIN64 vs __linux__/__linux).
func TestPlatformBranchSelection(t *testing.T) {
	src := "#if defined(_WIN32)\nint winv;\n#elif defined(__linux__)\nint linv;\n#else\nint otherv;\n#endif\n"
	cases := []struct {
		linux bool
		want  string
	}{
		{false, "int winv ;"},
		{true, "int linv ;"},
	}
	for _, tc := range cases {
		toks, err := common.PreprocessTarget(src, "probe.c", tc.linux)
		if err != nil {
			t.Fatalf("linux=%v: %v", tc.linux, err)
		}
		var texts []string
		for _, tk := range toks {
			if tk.Kind == frontend.TEOF {
				continue
			}
			texts = append(texts, tk.Text)
		}
		if got := strings.Join(texts, " "); got != tc.want {
			t.Errorf("linux=%v: got %q, want %q", tc.linux, got, tc.want)
		}
	}
}
