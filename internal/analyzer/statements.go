package analyzer

import (
	"go/ast"
	"go/token"
	"maps"
)

func (w *testWalker) walk(s ast.Stmt, e environment, guard origin) {
	if s == nil {
		return
	}
	switch s := s.(type) {
	case *ast.BlockStmt:
		for _, s := range s.List {
			w.walk(s, e, guard)
		}
	case *ast.AssignStmt:
		w.assign(s, e, guard)
	case *ast.DeclStmt:
		w.declare(s, e, guard)
	case *ast.IfStmt:
		w.branch(s, e, guard)
	case *ast.RangeStmt:
		w.rangeLoop(s, e, guard)
	case *ast.ForStmt:
		w.forLoop(s, e, guard)
	case *ast.ExprStmt:
		w.expression(s, e, guard)
	case *ast.ReturnStmt, *ast.BranchStmt, *ast.IncDecStmt, *ast.EmptyStmt:
		// Early exits and increment operations are not modeled precisely.
		if _, ok := s.(*ast.EmptyStmt); !ok {
			e.markUnknown()
		}
	default:
		// ponytail: switch/select, goroutines and deferred assertions need a CFG;
		// mark their assertions uncertain until a control-flow pass is warranted.
		ast.Inspect(s, func(n ast.Node) bool {
			if b, ok := n.(*ast.BlockStmt); ok {
				w.walk(b, maps.Clone(e), guard|unknown)
				return false
			}
			return true
		})
	}
}

func (w *testWalker) assign(s *ast.AssignStmt, e environment, guard origin) {
	values := make([]origin, len(s.Rhs))
	for i, r := range s.Rhs {
		values[i] = w.analyzer.expressionOrigin(r, e)
	}
	for i, l := range s.Lhs {
		if id, ok := l.(*ast.Ident); ok {
			v := unknown
			if len(values) == len(s.Lhs) {
				v = values[i]
			} else if len(values) == 1 {
				v = values[0]
			}
			o := w.analyzer.objectOf(id)
			if s.Tok != token.ASSIGN && s.Tok != token.DEFINE {
				v |= e[o]
			}
			e[o] = v | guard
		} else { // ponytail: no alias analysis; mutations make later local reads uncertain.
			e.markUnknown()
		}
	}
}

func (w *testWalker) declare(statement *ast.DeclStmt, env environment, guard origin) {
	declaration, ok := statement.Decl.(*ast.GenDecl)
	if !ok {
		return
	}
	for _, spec := range declaration.Specs {
		value, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		if len(value.Values) == 0 {
			for _, name := range value.Names {
				env[w.analyzer.objectOf(name)] = 0
			}
			continue
		}
		lhs := make([]ast.Expr, len(value.Names))
		for i, name := range value.Names {
			lhs[i] = name
		}
		w.assign(&ast.AssignStmt{Lhs: lhs, Rhs: value.Values, Tok: token.DEFINE}, env, guard)
	}
}

func (w *testWalker) branch(s *ast.IfStmt, e environment, guard origin) {
	w.walk(s.Init, e, guard)
	condition := w.analyzer.expressionOrigin(s.Cond, e)
	left, right := maps.Clone(e), maps.Clone(e)
	w.walk(s.Body, left, guard|condition)
	w.walk(s.Else, right, guard|condition)
	for o, v := range left {
		if v != right[o] {
			e[o] = v | right[o] | unknown
		} else {
			e[o] = v
		}
	}
}

func (w *testWalker) rangeLoop(s *ast.RangeStmt, e environment, guard origin) {
	inner := maps.Clone(e)
	v := w.analyzer.expressionOrigin(s.X, e)
	for _, x := range []ast.Expr{s.Key, s.Value} {
		if id, ok := x.(*ast.Ident); ok {
			inner[w.analyzer.objectOf(id)] = v
		}
	}
	w.walk(s.Body, inner, guard|unknown)
	for o, v := range e {
		if inner[o] != v {
			e[o] = v | inner[o] | unknown
		}
	}
}

func (w *testWalker) forLoop(s *ast.ForStmt, e environment, guard origin) {
	w.walk(s.Init, e, guard)
	inner := maps.Clone(e)
	w.walk(s.Body, inner, guard|unknown)
	w.walk(s.Post, inner, guard|unknown)
	for o, v := range e {
		if inner[o] != v {
			e[o] = v | inner[o] | unknown
		}
	}
}
