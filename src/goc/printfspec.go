package compiler

import (
	"goc/frontend"
	"strings"
)

// Constant-format printf specialisation, shared by both back ends.
//
// The native generator and the IR front end must agree on this exactly. If they
// did not, a program's size would depend on which back end built it, and --
// worse -- the two reachability prunes would disagree about what the program
// references: the emitter would call the cheap formatter while the pruner
// still saw printf and pulled vfmt in with it. That is not a hypothetical
// hazard; it is exactly what happened when the IR front end had its own copy
// and the native one had this.
//
// So the decision lives here, parameterised by the two questions a caller has
// to answer about its own symbol tables, and neither back end re-derives it.

// printfQueries are the facts a specialisation needs about the caller's
// program. Both are asked only about the *user's* declarations, never about
// the C runtime's own copies: "is this name already taken" has to mean by the
// program, or every library function would look shadowed.
type printfQueries struct {
	// userDefines reports whether the program defines a function of this name.
	userDefines func(name string) bool
	// shadowedByVar reports whether a *variable* of this name holds a function
	// pointer, which shadows the library function of the same spelling.
	shadowedByVar func(name string) bool
}

// specializePrintfCall rewrites a printf/fprintf call with a literal format
// into the cheapest entry point that reproduces it exactly, or returns nil when
// nothing applies and the ordinary printf must handle it.
//
// Two rewrites, in the order the native generator applies them:
//
//   - A '%'-free literal is a pure echo, so it becomes a direct fwrite. This
//     keeps vfmt, the FILE layer and the whole format engine out.
//   - A "lite" literal -- only %s, %c, the integer conversions, %f and %%, with
//     no width, precision or flag -- goes to __goclib_printf_lite (or its _f
//     variant when a %f is present). Those write straight to the OS handle.
//
// The decision is made here, at compile time, and that is what makes the
// call-graph prune work. A run-time "try lite, fall back to vfmt" probe would
// leave vfmt reachable and nothing would be pruned; rejecting the call instead
// leaves the program correct through the ordinary printf, just larger.
func specializePrintfCall(n *frontend.Call, q printfQueries) *frontend.Call {
	if q.userDefines == nil || q.shadowedByVar == nil {
		return nil
	}
	// fprintf takes the stream first, so the format is the second argument.
	fmtIdx := 0
	switch n.Name {
	case "printf":
	case "fprintf":
		fmtIdx = 1
	default:
		return nil
	}
	if len(n.Args) < fmtIdx+1 {
		return nil
	}
	lit, ok := n.Args[fmtIdx].(*frontend.StrLit)
	if !ok {
		return nil // a run-time format string proves nothing at compile time
	}
	if q.userDefines("fwrite") || q.shadowedByVar("fwrite") {
		return nil
	}
	if !strings.Contains(string(lit.Bytes), "%") {
		// A '%'-free format is a pure echo, but only when nothing else is
		// passed: fwrite takes a fixed byte count, so a trailing argument
		// would be dropped and its evaluation lost. The lite branch below does
		// accept extra arguments -- that is where the values are destined.
		if len(n.Args) != fmtIdx+1 {
			return nil
		}
		stream := frontend.Expr(&frontend.Call{Name: "__goclib_stdout"})
		if fmtIdx == 1 {
			stream = n.Args[0]
		}
		return &frontend.Call{Name: "fwrite", Args: []frontend.Expr{
			lit,
			&frontend.NumLit{Val: 1, Kind: frontend.TInt},
			&frontend.NumLit{Val: int64(len(lit.Bytes)), Kind: frontend.TInt},
			stream,
		}}
	}
	// The lite formatters take no stream, so there is no equivalent for
	// fprintf: routing it to one would write to the wrong FILE.
	if fmtIdx != 0 {
		return nil
	}
	target, ok := liteTargetFor(string(lit.Bytes))
	if !ok {
		return nil
	}
	if q.userDefines(target) || q.shadowedByVar(target) {
		return nil
	}
	args := make([]frontend.Expr, 0, len(n.Args))
	args = append(args, n.Args...)
	return &frontend.Call{Name: target, Args: args}
}
