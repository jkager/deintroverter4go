package analyzer

import (
	"go/ast"
	"go/types"
)

const helperDepth = 6

func (w *testWalker) invoke(d declaration, args []ast.Expr, recv ast.Expr, e *environment, guard value) value {
	if w.depth >= helperDepth || w.stack[d.fn] {
		return merge(value{flags: helper}, w.diagnostic(d.fn.Pos(), "helper-limit", "helper recursion or depth limit reached"))
	}
	caller := w.analyzer.pkg
	inner := e.clone()
	bind := func(obj types.Object, arg ast.Expr) {
		v := w.eval(arg, e, guard)
		inner.values[slot{root: obj}] = v
		if referenceType(obj.Type()) {
			if key, ok := w.analyzer.location(arg, e); ok {
				inner.aliases[obj] = key
			}
		}
	}
	sig := d.pkg.TypesInfo.Defs[d.fn.Name].Type().(*types.Signature)
	for i := 0; i < sig.Params().Len(); i++ {
		if i < len(args) {
			bind(sig.Params().At(i), args[i])
			if sig.Variadic() && i == sig.Params().Len()-1 {
				delete(inner.aliases, sig.Params().At(i))
			}
			if sig.Variadic() && i == sig.Params().Len()-1 && len(args) > i+1 {
				key := slot{root: sig.Params().At(i)}
				for _, arg := range args[i+1:] {
					inner.values[key] = merge(inner.values[key], w.eval(arg, e, guard))
				}
			}
		}
	}
	if sig.Recv() != nil && recv != nil {
		bind(sig.Recv(), recv)
	}
	w.stack[d.fn] = true
	w.depth++
	previousReturns := w.returns
	w.returns = nil
	w.analyzer.pkg = d.pkg
	w.walk(d.fn.Body, inner, guard)
	returnedValues := append([]value(nil), w.returns...)
	for len(returnedValues) < sig.Results().Len() {
		returnedValues = append(returnedValues, value{})
	}
	// Named result variables also participate in bare returns.
	for i := 0; i < sig.Results().Len(); i++ {
		if obj := sig.Results().At(i); obj.Name() != "" {
			returnedValues[i] = merge(returnedValues[i], inner.values[slot{root: obj}])
		}
	}
	returned := merge(returnedValues...)
	returned.results = returnedValues
	w.returns = previousReturns
	w.analyzer.pkg = caller
	w.depth--
	delete(w.stack, d.fn)
	// Propagate writes to caller slots, including suite fields introduced by setup.
	for key, v := range inner.values {
		if _, ok := e.values[key]; ok {
			e.values[key] = v
			continue
		}
		for _, alias := range inner.aliases {
			if key.root == alias.root {
				e.values[key] = v
				break
			}
		}
	}
	return returned
}
func (w *testWalker) invokeClosure(fn *ast.FuncLit, args []ast.Expr, e *environment, guard value) value {
	if w.depth >= helperDepth {
		return w.diagnostic(fn.Pos(), "helper-limit", "callback depth limit reached")
	}
	inner := e.clone()
	i := 0
	if fn.Type.Params != nil {
		for _, field := range fn.Type.Params.List {
			for _, name := range field.Names {
				if i < len(args) {
					inner.values[slot{root: w.analyzer.objectOf(name)}] = w.eval(args[i], e, guard)
				}
				i++
			}
		}
	}
	saved := w.returns
	w.returns = nil
	w.depth++
	w.walk(fn.Body, inner, guard)
	result := merge(w.returns...)
	result.results = append([]value(nil), w.returns...)
	w.depth--
	w.returns = saved
	for key := range e.values {
		e.values[key] = inner.values[key]
	}
	return result
}

type suiteContext struct {
	typ  types.Type
	root slot
}

func (w *testWalker) runSuite(c *ast.CallExpr, e *environment, guard value) {
	if w.depth >= helperDepth {
		w.diagnostic(c.Pos(), "helper-limit", "nested suite depth limit reached")
		return
	}
	typ := w.analyzer.pkg.TypesInfo.TypeOf(c.Args[1])
	if typ == nil {
		w.diagnostic(c.Pos(), "suite-type", "suite receiver type unavailable")
		return
	}
	root, ok := w.analyzer.location(c.Args[1], e)
	if !ok {
		root = slot{root: types.NewVar(c.Pos(), nil, "suite", typ)}
	}
	ctx := &suiteContext{typ, root}
	methods := types.NewMethodSet(typ)
	found := false
	for i := 0; i < methods.Len(); i++ {
		obj, ok := methods.At(i).Obj().(*types.Func)
		if !ok || !testName(obj.Name(), "Test") {
			continue
		}
		sig := obj.Type().(*types.Signature)
		if sig.Params().Len() != 0 || sig.Results().Len() != 0 {
			continue
		}
		d, ok := w.analyzer.declarations[obj]
		if !ok {
			w.diagnostic(c.Pos(), "suite-method", "suite method body unavailable")
			continue
		}
		found = true
		child := &testWalker{analyzer: w.analyzer, name: w.name + "/" + obj.Name(), stack: map[*ast.FuncDecl]bool{}, depth: w.depth + 1, suite: ctx}
		inner := e.clone()
		if _, exists := inner.values[root]; !exists {
			inner.values[root] = w.eval(c.Args[1], e, guard)
		}
		child.setupSuiteMethod("SetupSuite", inner)
		child.setupSuiteMethod("SetupTest", inner)
		child.setupSuiteMethod("BeforeTest", inner)
		inner.aliases[sig.Recv()] = root
		child.invokeSuite(d, inner, guard)
		child.setupSuiteMethod("AfterTest", inner)
		child.setupSuiteMethod("TearDownTest", inner)
		child.setupSuiteMethod("TearDownSuite", inner)
		w.analyzer.findings = append(w.analyzer.findings, child.finding(d.fn.Pos(), "suite-method"))
		w.children++
	}
	if !found {
		w.diagnostic(c.Pos(), "suite-discovery", "no analyzable suite methods found")
	}
}
func (w *testWalker) invokeSuite(d declaration, e *environment, guard value) {
	if w.depth >= helperDepth || w.stack[d.fn] {
		v := w.diagnostic(d.fn.Pos(), "helper-limit", "suite lifecycle recursion or depth limit reached")
		for key, old := range e.values {
			e.values[key] = merge(old, v)
		}
		return
	}
	w.depth++
	saved := w.analyzer.pkg
	w.analyzer.pkg = d.pkg
	sig := d.pkg.TypesInfo.Defs[d.fn.Name].Type().(*types.Signature)
	e.aliases[sig.Recv()] = w.suite.root
	w.stack[d.fn] = true
	w.walk(d.fn.Body, e, guard)
	delete(w.stack, d.fn)
	w.analyzer.pkg = saved
	w.depth--
}
func (w *testWalker) setupSuiteMethod(name string, e *environment) {
	if w.suite == nil {
		return
	}
	sel := types.NewMethodSet(w.suite.typ).Lookup(nil, name)
	if sel == nil {
		return
	}
	obj, ok := sel.Obj().(*types.Func)
	if !ok {
		return
	}
	d, ok := w.analyzer.declarations[obj]
	if !ok {
		w.diagnostic(obj.Pos(), "suite-hook", "suite lifecycle body unavailable")
		return
	}
	// Lifecycle assertions remain visible: their source locations distinguish them
	// from the method's behavioral assertions in the per-assertion report.
	w.invokeSuite(d, e, value{})
}
