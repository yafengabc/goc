package main

// print: a Python-style print builtin, lowered to printf at check time.
//
// C values carry no runtime type tags, but the checker sees every
// argument's static type -- so print(expr, ...) is a compiler builtin that
// builds one format string from those types and renames the call to printf
// with the literal prepended. The generated call rides the existing printf
// machinery (variadic GP-slot marshalling, the LC string pool) untouched.
//
// Single-argument calls on common shapes are dispatched to thin goclib
// primitives instead, so the weight of the vfmt interpreter is not linked
// in when only print("str") / print(int) / print(long) / print() are used:
//   - no arguments            -> print_str("")   (just the newline)
//   - char*                   -> print_str(s)
//   - int / char / _Bool      -> print_int_line(v)
//   - long                    -> print_long_line(v)
//   - anything else (floats, other pointers, structs) -> printf, below
//
// Argument conversion, deliberately honest about C semantics:
//   - integers (and _Bool)  -> %d   (a char prints as its numeric value)
//   - float / double        -> %g   (fixed point, trailing zeros stripped)
//   - char*                 -> %s   (string literals and char arrays decay)
//   - any other pointer     -> %p   (an int-array name decays to elem* first)
//   - struct / union        -> rejected: define a T_print method and call
//     it as x.print() (UFCS) instead
//
// Arguments are separated by one space and the line ends with a newline;
// print() with no arguments is just the newline. The result type is
// printf's (int): the number of characters the line would have written.

import "fmt"

// rewritePrint rewrites a print(...) Call in place into the equivalent
// printf call and type-checks it against printf's goclib prototype. It
// keeps going after a bad argument (with %d substituted) so one erroneous
// argument reports exactly once.
func (c *checker) rewritePrint(n *Call, fn *FuncDecl) *Type {
	// Static dispatch: a single argument whose static type is a printable
	// scalar or string lowers to the thin print_str / print_int_line /
	// print_long_line primitives -- no format interpreter linked. Anything
	// else (multiple arguments, floats, non-string pointers, structs, an
	// already-errored argument) falls through to the printf lowering.
	typed := make([]*Type, len(n.Args))
	if len(n.Args) <= 1 {
		var t *Type
		if len(n.Args) == 1 {
			t = c.checkExpr(n.Args[0], fn)
			typed[0] = t
		}
		switch {
		case len(n.Args) == 0:
			// print() is just the newline: print_str("") writes "\n".
			return c.rewritePrintThin(n, "print_str", []Expr{&StrLit{Bytes: []byte{}}}, fn)
		case t == nil:
			// errored argument: keep going through the printf path so the
			// %d stand-in reports exactly once.
		case t.Kind == KBool || (t.Kind == KInt && t.Width <= 4):
			return c.rewritePrintThin(n, "print_int_line", n.Args, fn)
		case t.Kind == KInt && t.Width == 8:
			return c.rewritePrintThin(n, "print_long_line", n.Args, fn)
		case t.Kind == KPtr && t.Elem != nil && t.Elem.Kind == KInt && t.Elem.Width == 1:
			return c.rewritePrintThin(n, "print_str", n.Args, fn)
		}
	}
	fs := make([]byte, 0, 8*len(n.Args)+2)
	for i, a := range n.Args {
		t := typed[i]
		if t == nil {
			t = c.checkExpr(a, fn)
		}
		spec, ok := printSpec(t)
		if !ok {
			hint := ""
			if t != nil && (t.Kind == KStruct || t.Kind == KUnion) && t.Tag != "" {
				hint = fmt.Sprintf(" (define a %s_print method and call it as x.print())", t.Tag)
			}
			c.errf(0, "print: cannot print a value of type %s%s", t, hint)
			spec = "%d"
		}
		if i > 0 {
			fs = append(fs, ' ')
		}
		fs = append(fs, spec...)
	}
	fs = append(fs, '\n')
	args := make([]Expr, 0, len(n.Args)+1)
	args = append(args, &StrLit{Bytes: fs})
	args = append(args, n.Args...)
	n.Name = "printf"
	n.Args = args
	if pd, ok := c.protos["printf"]; ok {
		return c.checkArgs("print", pd.ParamTypes, pd.Variadic, args, fn, pd.Ret)
	}
	return IntType() // unreachable: goclib.h always feeds the prototype table
}

// rewritePrintThin renames a print call in place to one of the thin
// single-argument primitives and type-checks it against the goclib
// prototype.
func (c *checker) rewritePrintThin(n *Call, name string, args []Expr, fn *FuncDecl) *Type {
	n.Name = name
	n.Args = args
	if pd, ok := c.protos[name]; ok {
		return c.checkArgs("print", pd.ParamTypes, pd.Variadic, args, fn, pd.Ret)
	}
	return IntType() // unreachable: goclib.h always feeds the prototype table
}

// printSpec picks the printf conversion for a print() argument of static
// type t. char is modelled as KInt with width 1, so "char*" is the KPtr
// whose element is a width-1 int.
func printSpec(t *Type) (string, bool) {
	if t == nil {
		return "", false
	}
	switch t.Kind {
	case KInt, KBool:
		return "%d", true
	case KFloat, KDouble:
		return "%g", true
	case KPtr:
		if t.Elem != nil && t.Elem.Kind == KInt && t.Elem.Width == 1 {
			return "%s", true
		}
		return "%p", true
	}
	return "", false // structs, unions, anything else
}
