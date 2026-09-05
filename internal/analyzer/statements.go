package analyzer

import (
	"go/ast"
	"go/token"
	"strings"
)

// walk returns true for an unconditional return. Conditional exits retain an
// explicit uncertainty instead of pretending to prove reachability.
func (w *testWalker) walk(s ast.Stmt, e *environment, guard value) bool {
	if s == nil {
		return false
	}
	switch s := s.(type) {
	case *ast.BlockStmt:
		for _, stmt := range s.List {
			if w.walk(stmt, e, guard) {
				return true
			}
		}
	case *ast.AssignStmt:
		w.assign(s, e, guard)
	case *ast.DeclStmt:
		if d, ok := s.Decl.(*ast.GenDecl); ok {
			for _, spec := range d.Specs {
				if v, ok := spec.(*ast.ValueSpec); ok {
					lhs := []ast.Expr{}
					for _, name := range v.Names {
						lhs = append(lhs, name)
					}
					w.assign(&ast.AssignStmt{Lhs: lhs, Rhs: v.Values, Tok: token.DEFINE}, e, guard)
				}
			}
		}
	case *ast.IfStmt:
		w.walk(s.Init, e, guard)
		condition := w.eval(s.Cond, e, guard)
		left, right := e.clone(), e.clone()
		l := w.walk(s.Body, left, merge(guard, condition))
		r := w.walk(s.Else, right, merge(guard, condition))
		w.mergeBranches(e, left, right, s.Pos())
		if l && r {
			return true
		}
		if l || r {
			v := w.diagnostic(s.Pos(), "conditional-exit", "conditional return may affect later assertions")
			for k, old := range e.values {
				e.values[k] = merge(old, v)
			}
		}
	case *ast.RangeStmt:
		inner := e.clone()
		v := w.eval(s.X, e, guard)
		for _, x := range []ast.Expr{s.Key, s.Value} {
			if x != nil {
				if key, ok := w.analyzer.location(x, inner); ok {
					inner.values[key] = v
				}
			}
		}
		// Assertions in a table body can be analyzed per symbolic row. Carrying state
		// between iterations is not modeled, and is kept separate from reachability.
		ast.Inspect(s.Body, func(n ast.Node) bool {
			if _, ok := n.(*ast.FuncLit); ok {
				return false
			}
			assignment, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for _, lhs := range assignment.Lhs {
				if key, ok := w.analyzer.location(lhs, inner); ok {
					if old, exists := e.values[key]; exists {
						inner.values[key] = merge(old, w.diagnostic(s.Pos(), "loop-state", "range loop carries an outer value between iterations"))
					}
				}
			}
			return true
		})
		w.walk(s.Body, inner, guard)
		w.mergeBranches(e, e.clone(), inner, s.Pos())
	case *ast.ForStmt:
		w.walk(s.Init, e, guard)
		inner := e.clone()
		g := merge(guard, w.diagnostic(s.Pos(), "loop-state", "general loop iteration state is not modeled"))
		w.eval(s.Cond, inner, g)
		w.walk(s.Body, inner, g)
		w.walk(s.Post, inner, g)
		w.mergeBranches(e, e.clone(), inner, s.Pos())
	case *ast.ExprStmt:
		v := w.eval(s.X, e, guard)
		if u, ok := s.X.(*ast.UnaryExpr); ok && u.Op == token.ARROW {
			for k, old := range e.values {
				e.values[k] = merge(old, v)
			}
		}
	case *ast.ReturnStmt:
		for i, x := range s.Results {
			v := merge(w.eval(x, e, guard), guard)
			for len(w.returns) <= i {
				w.returns = append(w.returns, value{})
			}
			w.returns[i] = merge(w.returns[i], v)
		}
		return true
	case *ast.IncDecStmt:
		if k, ok := w.analyzer.location(s.X, e); ok {
			e.values[k] = merge(e.values[k], guard)
		}
	case *ast.EmptyStmt:
	case *ast.BranchStmt:
		v := w.diagnostic(s.Pos(), "branch-exit", "break, continue, or goto is not modeled")
		for k, old := range e.values {
			e.values[k] = merge(old, v)
		}
	default:
		g := merge(guard, w.diagnostic(s.Pos(), "unsupported-control", "switch/select, deferred or concurrent execution is not modeled"))
		ast.Inspect(s, func(n ast.Node) bool {
			if b, ok := n.(*ast.BlockStmt); ok {
				w.walk(b, e.clone(), g)
				return false
			}
			if c, ok := n.(*ast.CallExpr); ok {
				w.eval(c, e.clone(), g)
				return false
			}
			return true
		})
		for k, old := range e.values {
			e.values[k] = merge(old, g)
		}
	}
	return false
}
func (w *testWalker) assign(s *ast.AssignStmt, e *environment, guard value) {
	values := []value{}
	for _, x := range s.Rhs {
		values = append(values, w.eval(x, e, guard))
	}
	for i, x := range s.Lhs {
		if id, ok := x.(*ast.Ident); ok && id.Name == "_" {
			continue
		}
		v := value{}
		if len(values) == len(s.Lhs) {
			v = values[i]
		} else if len(values) == 1 {
			if len(values[0].results) == len(s.Lhs) {
				v = values[0].results[i]
			} else {
				v = values[0]
			}
		} else if len(values) > 0 {
			v = w.diagnostic(s.Pos(), "assignment", "unresolved assignment arity")
		}
		if key, ok := w.analyzer.location(x, e); ok {
			if id, ok := x.(*ast.Ident); ok && (s.Tok == token.DEFINE || s.Tok == token.ASSIGN) {
				delete(e.aliases, w.analyzer.objectOf(id))
				key = slot{root: w.analyzer.objectOf(id)}
			}
			if s.Tok != token.ASSIGN && s.Tok != token.DEFINE {
				v = merge(v, e.values[key])
			}
			for old := range e.values {
				if old.root == key.root && strings.HasPrefix(old.field, key.field+".") {
					delete(e.values, old)
				}
			}
			e.values[key] = merge(v, guard)
			e.values[key] = withClosure(e.values[key], v)
			if len(s.Rhs) == len(s.Lhs) && referenceType(w.analyzer.pkg.TypesInfo.TypeOf(x)) {
				if target, ok := w.analyzer.location(s.Rhs[i], e); ok && key.field == "" && target != key {
					e.aliases[key.root] = target
				}
			}
		} else {
			w.invalidate(x, e, w.diagnostic(s.Pos(), "mutation", "indexed or indirect assignment is not modeled"))
		}
	}
}
func withClosure(v, original value) value { v.closure = original.closure; return v }
