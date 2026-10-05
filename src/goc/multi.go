package compiler

// Multi-translation-unit builds.
//
// goc has no separate linking stage: one frontend.Program is compiled into one
// executable. `goc a.c b.c` therefore parses each file as its own translation
// unit (its own macros, typedefs, struct tags and enumerators -- frontend.Parse resets
// those tables, so nothing leaks between files), merges the resulting
// declarations into a single frontend.Program, and type-checks/generates that once.
//
// Two consequences are handled explicitly here:
//
//   - Duplicate external definitions (two files defining the same non-static
//     function or global) are an error, exactly as they are for a real linker.
//
//   - static means internal linkage: two files may each have their own static
//     helper() or static int buf[10]. Where a static name is also defined by
//     another file, this file's symbol is renamed (declaration plus every
//     reference) to a per-file unique name before merging. Renaming only
//     happens on an actual clash, so the common case leaves the AST untouched.
//
// Only the enumerator table needs care: enumerators live in one package-level
// map that frontend.Check and Gen consult, so each unit's enumerators are snapshotted
// and merged back after the last frontend.Parse (which clears the table).

import (
	"fmt"
	"goc/common"
	"os"
	"strings"
)

// parsedUnit is one .c file after preprocessing and parsing.

// buildMulti compiles several .c files into one executable.
// buildMulti compiles several .c files as one program. Each file is its own
// translation unit -- its own macros, its own type names -- and the merged
// result is what the image is built from.
//
// The per-file work lives in common.Translate because gocl needs it too, and
// two copies of the merging rules would eventually disagree about what a
// duplicate static is. Only -E stays here: it has to emit every unit's text
// separately, which is a request about output rather than about compilation.
func buildMulti(cfg buildCfg, isCC bool) (string, error) {
	// -E: gcc concatenates the preprocessed translation units to stdout (or
	// to -o). No parsing, no merging.
	if cfg.mode == "preprocess" {
		var b strings.Builder
		for _, path := range cfg.inputs {
			toks, err := common.PreprocessFile(path, cfg.defines, cfg.linux, cfg.incDirs...)
			if err != nil {
				return "", err
			}
			b.WriteString(common.SerializeTokens(toks))
			b.WriteString("\n")
		}
		if cfg.outFile != "" {
			if err := os.WriteFile(cfg.outFile, []byte(b.String()), 0644); err != nil {
				return "", err
			}
		} else {
			fmt.Print(b.String())
		}
		return "", nil
	}

	prog, err := common.Translate(cfg.inputs, cfg.defines, cfg.linux, cfg.incDirs...)
	if err != nil {
		return "", err
	}
	return emitProgram(prog, cfg, isCC)
}
