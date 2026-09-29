package main

// print: a Python-style print builtin, lowered to printf at check time.
//
// C values carry no runtime type tags, but the checker sees every
// argument's static type -- so print(expr, ...) is a compiler builtin that
// builds one format string from those types and renames the call to printf
// with the literal prepended. The generated call rides the existing printf
// machinery (variadic GP-slot marshalling, the LC string pool) untouched.
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
	fs := make([]byte, 0, 8*len(n.Args)+2)
	for i, a := range n.Args {
		t := c.checkExpr(a, fn)
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
