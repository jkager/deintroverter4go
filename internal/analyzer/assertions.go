package analyzer

import (
	"go/ast"
	"go/types"
	"maps"
	"strings"
)

func (w *testWalker) expression(s *ast.ExprStmt, e environment, guard origin) {
	c, ok := s.X.(*ast.CallExpr)
	if !ok {
		return
	}
	path, method := w.analyzer.callTarget(c)
	if path == "testing" && method == "Run" && len(c.Args) == 2 {
		if fn, ok := c.Args[1].(*ast.FuncLit); ok {
			child := "<dynamic>"
			if lit, ok := c.Args[0].(*ast.BasicLit); ok {
				child = strings.Trim(lit.Value, "\"")
			}
			w.children++
			w.analyzer.analyzeTest(w.name+"/"+child, fn.Body, maps.Clone(e), c.Pos())
			return
		}
	}
	if path == "testing" {
		switch method {
		case "Error", "Errorf", "Fatal", "Fatalf", "Fail", "FailNow":
			// Failure message arguments are diagnostics, not asserted values.
			w.assertions = append(w.assertions, observation{guard, w.analyzer.pkg.Fset.Position(c.Pos()).Line})
		}
		return
	}
	if path == "github.com/stretchr/testify/assert" || path == "github.com/stretchr/testify/require" {
		if method == "New" {
			return
		}
		switch method {
		case "Fail", "FailNow", "Failf", "FailNowf":
			w.assertions = append(w.assertions, observation{guard, w.analyzer.pkg.Fset.Position(c.Pos()).Line})
			return
		}
		v := guard
		// Ignore the testing receiver and optional diagnostic messages. The
		// function signature identifies the variadic message boundary.
		sig, ok := w.analyzer.objectOf(c.Fun).Type().(*types.Signature)
		if !ok {
			return
		}
		limit := len(c.Args)
		if sig.Variadic() && limit >= sig.Params().Len() {
			limit = sig.Params().Len() - 1
		}
		start := 0
		if sig.Recv() == nil {
			start = 1
		}
		for i := start; i < limit; i++ {
			v |= w.analyzer.expressionOrigin(c.Args[i], e)
		}
		w.assertions = append(w.assertions, observation{v, w.analyzer.pkg.Fset.Position(c.Pos()).Line})
		return
	}
	// An opaque call can assert internally or mutate values by reference.
	e.markUnknown()
}
