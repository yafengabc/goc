package main

// Multi-translation-unit builds.
//
// goc has no separate linking stage: one Program is compiled into one
// executable. `goc a.c b.c` therefore parses each file as its own translation
// unit (its own macros, typedefs, struct tags and enumerators -- Parse resets
// those tables, so nothing leaks between files), merges the resulting
// declarations into a single Program, and type-checks/generates that once.
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
// map that Check and Gen consult, so each unit's enumerators are snapshotted
// and merged back after the last Parse (which clears the table).

import (
	"fmt"
	"os"
	"reflect"
	"strings"
)

// parsedUnit is one .c file after preprocessing and parsing.
type parsedUnit struct {
	path string
	prog *Program
	// enums holds the enumerators this unit declared; Parse clears the global
	// table before every unit, so the values are collected here and merged
	// back once all units are parsed.
	enums map[string]int64
}

// buildMulti compiles several .c files into one executable.
func buildMulti(cfg buildCfg, isCC bool) (string, error) {
	units := make([]*parsedUnit, 0, len(cfg.inputs))

	// -E: gcc concatenates the preprocessed translation units to stdout (or
	// to -o). No parsing, no merging.
	if cfg.mode == "preprocess" {
		var b strings.Builder
		for _, path := range cfg.inputs {
			toks, err := preprocessFile(cfg, path)
			if err != nil {
				return "", err
			}
			b.WriteString(SerializeTokens(toks))
			b.WriteString("\n")
		}
		out := b.String()
		if cfg.outFile != "" && !isDir(cfg.outFile) {
			if err := os.WriteFile(cfg.outFile, []byte(out), 0644); err != nil {
				return "", err
			}
		} else {
			os.Stdout.WriteString(out)
		}
		return "", nil
	}

	for _, path := range cfg.inputs {
		toks, err := preprocessFile(cfg, path)
		if err != nil {
			return "", err
		}
		// Parse resets typedefs/structs/enumConsts on entry, giving this file
		// a clean type namespace; it also leaves behind *this* file's
		// enumerators, which we snapshot before the next Parse wipes them.
		prog, err := Parse(toks)
		if err != nil {
			return "", fmt.Errorf("%s: parse error: %w", path, err)
		}
		u := &parsedUnit{path: path, prog: prog, enums: map[string]int64{}}
		for k, v := range enumConsts {
			u.enums[k] = v
		}
		units = append(units, u)
	}

	// Restore every unit's enumerators: Check (identifier resolution,
	// case-label constants) and Gen (constant folding) read the global table.
	for k := range enumConsts {
		delete(enumConsts, k)
	}
	for _, u := range units {
		for k, v := range u.enums {
			if old, dup := enumConsts[k]; dup && old != v {
				return "", fmt.Errorf("%s: enumerator %q is also defined with a different value in another file", u.path, k)
			}
			enumConsts[k] = v
		}
	}

	prog, err := mergeUnits(units)
	if err != nil {
		return "", err
	}
	return emitProgram(prog, cfg, isCC)
}

// preprocessFile reads one source file, injects -D macros and runs the
// target-aware preprocessor over it.
func preprocessFile(cfg buildCfg, path string) ([]Token, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	src = []byte(injectDefines(string(src), cfg.defines))
	toks, err := PreprocessTarget(string(src), path, cfg.linux, cfg.incDirs...)
	if err != nil {
		return nil, fmt.Errorf("%s: preprocess error: %w", path, err)
	}
	return toks, nil
}

// unitSym is one file-scope symbol a translation unit defines.
type unitSym struct {
	name   string
	static bool
	kind   string // "function" or "variable"
	unit   int
	path   string
}

// mergeUnits merges the parsed translation units into one Program, renaming
// clashing static symbols and rejecting duplicate external definitions.
func mergeUnits(units []*parsedUnit) (*Program, error) {
	// 1. Collect every file-scope definition, grouped by name.
	defs := map[string][]unitSym{}
	for i, u := range units {
		for _, f := range u.prog.Funcs {
			defs[f.Name] = append(defs[f.Name], unitSym{f.Name, f.Storage == "static", "function", i, u.path})
		}
		for _, g := range u.prog.Globals {
			if g.Storage == "extern" {
				continue // a declaration, not a definition
			}
			defs[g.Name] = append(defs[g.Name], unitSym{g.Name, g.Storage == "static", "variable", i, u.path})
		}
	}

	// 2. Decide the renames. A name defined by two units is a hard error
	// unless at least one of the definitions is static: then every *static*
	// definition of that name gets a per-unit unique name and the external
	// one keeps its own.
	renames := make([]map[string]string, len(units))
	for i := range units {
		renames[i] = map[string]string{}
	}
	names := make([]string, 0, len(defs))
	for n := range defs {
		names = append(names, n)
	}
	// Sorted iteration keeps diagnostics deterministic.
	sortStrings(names)
	for _, n := range names {
		syms := defs[n]
		if len(syms) < 2 {
			continue
		}
		byUnit := map[int]bool{}
		for _, s := range syms {
			byUnit[s.unit] = true
		}
		if len(byUnit) < 2 {
			// The same file defines the name twice. Check catches a repeated
			// global ("redefinition in the same scope") but not a repeated
			// function, which would silently emit the label twice -- e.g.
			// `goc a.c a.c`.
			if syms[0].kind == "function" {
				return nil, fmt.Errorf("duplicate definition of function %q in %s", n, syms[0].path)
			}
			continue
		}
		anyStatic := false
		for _, s := range syms {
			if s.static {
				anyStatic = true
			}
		}
		if !anyStatic {
			return nil, fmt.Errorf("duplicate definition of %s %q: %s and %s",
				syms[0].kind, n, syms[0].path, syms[len(syms)-1].path)
		}
		for _, s := range syms {
			if s.static {
				renames[s.unit][n] = uniqueRename(n, s.unit, defs)
			}
		}
	}

	// 3. Apply the renames (declaration + every reference) and merge.
	merged := &Program{}
	globals := map[string]*DeclStmt{}
	var globalOrder []string
	protos := map[string]*FuncDecl{}
	for i, u := range units {
		if len(renames[i]) > 0 {
			renameInProgram(u.prog, renames[i])
		}
		merged.Funcs = append(merged.Funcs, u.prog.Funcs...)
		for _, p := range u.prog.Prototypes {
			if prev, dup := protos[p.Name]; dup {
				// Keep the first prototype but remember an import library
				// named on any of them (`extern long MessageBoxA(...), user32`).
				if prev.DLL == "" && p.DLL != "" {
					prev.DLL = p.DLL
				}
				continue
			}
			protos[p.Name] = p
			merged.Prototypes = append(merged.Prototypes, p)
		}
		for _, g := range u.prog.Globals {
			prev, dup := globals[g.Name]
			if !dup {
				globals[g.Name] = g
				globalOrder = append(globalOrder, g.Name)
				continue
			}
			// Same name in two files: two plain declarations of the same
			// header-declared global are one object; a real definition wins
			// over an extern declaration and two definitions were rejected
			// above (or renamed, for statics).
			prevDef := prev.Storage != "extern"
			thisDef := g.Storage != "extern"
			if thisDef && !prevDef {
				globals[g.Name] = g
				for j, nm := range globalOrder {
					if nm == g.Name {
						globalOrder[j] = g.Name // keep the position
						break
					}
				}
			}
		}
	}
	for _, n := range globalOrder {
		merged.Globals = append(merged.Globals, globals[n])
	}
	return merged, nil
}

// uniqueRename picks a file-unique replacement for a static symbol.
func uniqueRename(name string, unit int, defs map[string][]unitSym) string {
	for i := 0; ; i++ {
		cand := fmt.Sprintf("%s__tu%d", name, unit)
		if i > 0 {
			cand = fmt.Sprintf("%s__tu%d_%d", name, unit, i)
		}
		if _, taken := defs[cand]; !taken {
			return cand
		}
	}
}

// renameInProgram renames file-scope symbols throughout one unit's AST: the
// declarations themselves and every Ident that refers to them. The walk is
// reflection-based so it cannot silently miss a node type (an unhandled node
// shape would be a missed rename, which is a wrong-reference bug).
func renameInProgram(prog *Program, renames map[string]string) {
	if len(renames) == 0 {
		return
	}
	for _, f := range prog.Funcs {
		if nn, ok := renames[f.Name]; ok {
			f.Name = nn
		}
		renameInValue(f.Body, renames)
	}
	for _, f := range prog.Prototypes {
		if nn, ok := renames[f.Name]; ok {
			f.Name = nn
		}
	}
	for _, g := range prog.Globals {
		if nn, ok := renames[g.Name]; ok {
			g.Name = nn
		}
		renameInValue(g.Init, renames)
	}
}

func renameInValue(v any, renames map[string]string) {
	if v == nil {
		return
	}
	renameReflect(reflect.ValueOf(v), renames)
}

func renameReflect(v reflect.Value, renames map[string]string) {
	switch v.Kind() {
	case reflect.Invalid:
		return
	case reflect.Ptr, reflect.Interface:
		if v.IsNil() {
			return
		}
		if v.CanInterface() {
			switch x := v.Interface().(type) {
			case *Ident:
				if nn, ok := renames[x.Name]; ok {
					x.Name = nn
				}
				return
			case *Call:
				// A direct call names its callee as a string, not an *Ident.
				// (An indirect call goes through IndirectCall.Fn/UFCS and is
				// reached by the ordinary walk.)
				if nn, ok := renames[x.Name]; ok {
					x.Name = nn
				}
			case *Type:
				return // types carry tags and member names, never symbols
			case Type:
				return
			}
		}
		renameReflect(v.Elem(), renames)
	case reflect.Struct:
		if v.CanInterface() {
			if _, ok := v.Interface().(Type); ok {
				return
			}
		}
		for i := 0; i < v.NumField(); i++ {
			renameReflect(v.Field(i), renames)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			renameReflect(v.Index(i), renames)
		}
	case reflect.Map:
		it := v.MapRange()
		for it.Next() {
			renameReflect(it.Value(), renames)
		}
	}
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
