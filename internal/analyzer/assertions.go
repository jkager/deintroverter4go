package analyzer

import (
	"go/ast"
	"go/types"
	"strconv"
	"strings"
)

func (w *testWalker) call(c *ast.CallExpr, e *environment, guard value) value {
	a := w.analyzer
	path, method := a.callTarget(c)
	if a.pkg.TypesInfo.Types[c.Fun].IsType() {
		var v value
		for _, arg := range c.Args {
			v = merge(v, w.eval(arg, e, guard))
		}
		return v
	}
	if path == "github.com/stretchr/testify/suite" && method == "Run" && len(c.Args) == 2 && a.objectOf(c.Fun).Type().(*types.Signature).Recv() == nil {
		w.runSuite(c, e, guard)
		return value{}
	}
	if (path == "testing" || path == "github.com/stretchr/testify/suite") && method == "Run" && len(c.Args) == 2 {
		fn := w.eval(c.Args[1], e, guard).closure
		if fn != nil {
			name := "<dynamic>"
			if lit, ok := c.Args[0].(*ast.BasicLit); ok {
				if s, err := strconv.Unquote(lit.Value); err == nil {
					name = s
				}
			}
			child := &testWalker{analyzer: a, name: w.name + "/" + name, stack: w.stack, depth: w.depth, suite: w.suite}
			inner := e.clone()
			if path == "github.com/stretchr/testify/suite" && w.suite != nil {
				child.setupSuiteMethod("SetupSubTest", inner)
			}
			child.walk(fn.Body, inner, guard)
			if path == "github.com/stretchr/testify/suite" && w.suite != nil {
				child.setupSuiteMethod("TearDownSubTest", inner)
			}
			a.findings = append(a.findings, child.finding(c.Pos(), "subtest"))
			w.children++
			return value{}
		}
		return w.diagnostic(c.Pos(), "subtest-callback", "subtest callback is not an inline or locally bound closure")
	}
	if path == "pgregory.net/rapid" && (method == "Check" || method == "Run") && len(c.Args) >= 2 {
		return w.callback(c.Args[1], e, guard, false)
	}
	if path == "pgregory.net/rapid" && (method == "Repeat" || method == "StateMachineActions") {
		return w.diagnostic(c.Pos(), "rapid-state-machine", "Rapid state-machine dispatch is not modeled; action methods require manual review")
	}
	if path == "testing" || path == "pgregory.net/rapid" {
		switch method {
		case "Error", "Errorf", "Fatal", "Fatalf", "Fail", "FailNow":
			w.observe(c.Pos(), guard)
			return guard
		case "Helper", "Log", "Logf", "Name", "Parallel":
			return value{}
		case "Skip", "Skipf", "SkipNow":
			return w.diagnostic(c.Pos(), "skip", "test skipping and conditional execution are not proven")
		}
	}
	if path == "github.com/stretchr/testify/assert" || path == "github.com/stretchr/testify/require" {
		if method == "New" {
			return value{}
		}
		switch method {
		case "Fail", "FailNow", "Failf", "FailNowf":
			w.observe(c.Pos(), guard)
			return guard
		}
		obj := a.objectOf(c.Fun)
		if obj == nil {
			return w.diagnostic(c.Pos(), "assertion-signature", "assertion signature unavailable")
		}
		sig, ok := obj.Type().(*types.Signature)
		if !ok {
			return w.diagnostic(c.Pos(), "assertion-signature", "assertion signature unavailable")
		}
		start, limit := 0, len(c.Args)
		if sig.Recv() == nil {
			start = 1
		}
		if sig.Variadic() && limit >= sig.Params().Len() {
			limit = sig.Params().Len() - 1
		}
		v := guard
		callbackAssertion := strings.HasPrefix(method, "Eventually") || strings.HasPrefix(method, "Never") || strings.HasPrefix(method, "Condition")
		for i := start; i < limit; i++ {
			if callbackAssertion && i == start {
				v = merge(v, w.callback(c.Args[i], e, guard, true))
			} else {
				arg := w.eval(c.Args[i], e, guard)
				if arg.closure != nil {
					arg = merge(arg, w.diagnostic(c.Args[i].Pos(), "callback", "this assertion callback is not modeled"))
				}
				v = merge(v, arg)
			}
		}
		if !strings.Contains(method, "WithT") {
			w.observe(c.Pos(), v)
		}
		return v
	}
	if path == "github.com/stretchr/testify/suite" && (method == "T" || method == "Assert" || method == "Require") {
		return value{}
	}
	if sel, ok := c.Fun.(*ast.SelectorExpr); ok {
		if selection := a.pkg.TypesInfo.Selections[sel]; selection != nil && selection.Kind() == types.MethodExpr {
			v := w.diagnostic(c.Pos(), "method-expression", "explicit method-expression receiver binding is not modeled")
			for _, arg := range c.Args {
				v = merge(v, w.eval(arg, e, guard))
			}
			return v
		}
	}
	if fn, ok := a.objectOf(c.Fun).(*types.Func); ok {
		sig := fn.Type().(*types.Signature)
		if sig.Recv() != nil {
			if _, ok := sig.Recv().Type().Underlying().(*types.Interface); ok {
				v := merge(a.source(fn), w.diagnostic(c.Pos(), "interface-dispatch", "interface method implementation is not resolved"))
				if r := receiver(c); r != nil {
					v = merge(v, w.eval(r, e, guard))
					w.invalidate(r, e, v)
				}
				for _, arg := range c.Args {
					v = merge(v, w.eval(arg, e, guard))
					if referenceType(a.pkg.TypesInfo.TypeOf(arg)) {
						w.invalidate(arg, e, v)
					}
				}
				return v
			}
		}
		if d, ok := a.declarations[fn]; ok && a.source(fn).flags&helper != 0 {
			return w.invoke(d, c.Args, receiver(c), e, guard)
		}
	}
	if fn := w.evalFunction(c.Fun, e); fn != nil {
		// Function-valued calls are followed only for a directly known local closure.
		return w.invokeClosure(fn, c.Args, e, guard)
	}
	v := a.source(a.objectOf(c.Fun))
	if recv := receiver(c); recv != nil {
		v = merge(v, w.eval(recv, e, guard))
	}
	for _, arg := range c.Args {
		argValue := w.eval(arg, e, guard)
		if argValue.closure != nil {
			argValue = merge(argValue, w.diagnostic(arg.Pos(), "callback", "opaque call may execute this callback"))
		}
		v = merge(v, argValue)
	}
	obj := a.objectOf(c.Fun)
	if _, ok := obj.(*types.Var); ok || obj == nil {
		v = merge(v, w.diagnostic(c.Pos(), "dynamic-call", "function value or dynamic dispatch cannot be resolved"))
	}
	if a.source(obj).flags&helper != 0 {
		v = merge(v, w.diagnostic(c.Pos(), "helper-unavailable", "test helper body is unavailable"))
	}
	// Calls may change only reference-bearing arguments/receivers here. This is
	// deliberately conservative: no blanket invalidation of unrelated scalar locals.
	if _, ok := obj.(*types.TypeName); !ok {
		if _, ok := obj.(*types.Builtin); !ok {
			args := append([]ast.Expr{}, c.Args...)
			if r := receiver(c); r != nil {
				args = append(args, r)
			}
			for _, arg := range args {
				if referenceType(a.pkg.TypesInfo.TypeOf(arg)) {
					issue := w.diagnostic(c.Pos(), "reference-effect", "call may mutate a reference argument or receiver; effects are not traced")
					w.invalidate(arg, e, issue)
				}
			}
		}
	}
	return v
}
func receiver(c *ast.CallExpr) ast.Expr {
	if s, ok := c.Fun.(*ast.SelectorExpr); ok {
		return s.X
	}
	return nil
}
func (w *testWalker) evalFunction(x ast.Expr, e *environment) *ast.FuncLit {
	if fn, ok := x.(*ast.FuncLit); ok {
		return fn
	}
	if key, ok := w.analyzer.location(x, e); ok {
		return e.values[key].closure
	}
	return nil
}
func (w *testWalker) callback(x ast.Expr, e *environment, guard value, predicate bool) value {
	if fn := w.evalFunction(x, e); fn != nil {
		return w.invokeClosure(fn, nil, e, guard)
	}
	if fn, ok := w.analyzer.objectOf(x).(*types.Func); ok {
		if d, ok := w.analyzer.declarations[fn]; ok && w.analyzer.source(fn).flags&helper != 0 {
			var recv ast.Expr
			if s, ok := x.(*ast.SelectorExpr); ok {
				recv = s.X
			}
			return w.invoke(d, nil, recv, e, guard)
		}
		if predicate {
			return w.analyzer.source(fn)
		}
	}
	return w.diagnostic(x.Pos(), "callback", "callback body is unavailable or dynamically selected")
}
