package analyzer

import (
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"reflect"
	"slices"
	"strings"
)

type origin uint8

const (
	sut origin = 1 << iota
	helper
	unknown
)

// Keep representative evidence bounded; the report explicitly marks truncation.
const evidenceLimit = 8

type value struct {
	truncated   bool
	flags       origin
	sources     []Source
	diagnostics []Diagnostic
	closure     *ast.FuncLit
	results     []value
}

func merge(values ...value) value {
	var out value
	for _, v := range values {
		out.flags |= v.flags
		out.truncated = out.truncated || v.truncated
		for _, s := range v.sources {
			if !slices.Contains(out.sources, s) {
				if len(out.sources) < evidenceLimit {
					out.sources = append(out.sources, s)
				} else {
					out.truncated = true
				}
			}
		}
		for _, d := range v.diagnostics {
			if !slices.Contains(out.diagnostics, d) {
				if len(out.diagnostics) < evidenceLimit {
					out.diagnostics = append(out.diagnostics, d)
				} else {
					out.truncated = true
				}
			}
		}
	}
	if len(values) == 1 {
		out.closure = values[0].closure
		out.results = values[0].results
	}
	return out
}

// Slots distinguish fields on different receivers. Aliases cover direct reference
// bindings only; arbitrary pointer effects remain explicitly uncertain.
type slot struct {
	root  types.Object
	field string
}
type environment struct {
	values  map[slot]value
	aliases map[types.Object]slot
}

func newEnvironment() *environment { return &environment{map[slot]value{}, map[types.Object]slot{}} }
func (e *environment) clone() *environment {
	return &environment{maps.Clone(e.values), maps.Clone(e.aliases)}
}
func (a *analyzer) objectOf(e ast.Expr) types.Object {
	switch e := e.(type) {
	case *ast.Ident:
		return a.pkg.TypesInfo.ObjectOf(e)
	case *ast.SelectorExpr:
		return a.pkg.TypesInfo.ObjectOf(e.Sel)
	case *ast.IndexExpr:
		return a.objectOf(e.X)
	case *ast.IndexListExpr:
		return a.objectOf(e.X)
	case *ast.ParenExpr:
		return a.objectOf(e.X)
	}
	return nil
}
func (a *analyzer) location(x ast.Expr, e *environment) (slot, bool) {
	switch x := x.(type) {
	case *ast.Ident:
		o := a.objectOf(x)
		if o == nil {
			return slot{}, false
		}
		if s, ok := e.aliases[o]; ok {
			return s, true
		}
		return slot{root: o}, true
	case *ast.SelectorExpr:
		if sel := a.pkg.TypesInfo.Selections[x]; sel != nil && sel.Kind() == types.FieldVal {
			s, ok := a.location(x.X, e)
			s.field += "." + x.Sel.Name
			return s, ok
		}
	case *ast.ParenExpr:
		return a.location(x.X, e)
	case *ast.StarExpr:
		return a.location(x.X, e)
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			return a.location(x.X, e)
		}
	}
	return slot{}, false
}
func (a *analyzer) source(o types.Object) value {
	if o == nil || o.Pkg() == nil {
		return value{}
	}
	switch o.(type) {
	case *types.TypeName, *types.PkgName:
		return value{}
	}
	path := o.Pkg().Path()
	if a.module == "" || !(strings.TrimSuffix(path, "_test") == a.module || strings.HasPrefix(path, a.module+"/")) {
		return value{}
	}
	p := a.pkg.Fset.Position(o.Pos())
	if strings.HasSuffix(p.Filename, "_test.go") || strings.HasSuffix(path, "_test") || a.isHelperPackage(path) {
		return value{flags: helper}
	}
	isDeclaration := o.Parent() == o.Pkg().Scope()
	if f, ok := o.(*types.Func); ok && f.Type().(*types.Signature).Recv() != nil {
		isDeclaration = true
	}
	if !isDeclaration {
		return value{}
	}
	symbol := path + "." + o.Name()
	if fn, ok := o.(*types.Func); ok {
		symbol = fn.FullName()
	}
	return value{flags: sut, sources: []Source{{symbol, p.Filename, p.Line}}}
}
func (w *testWalker) eval(x ast.Expr, e *environment, guard value) value {
	if x == nil {
		return value{}
	}
	a := w.analyzer
	if a.pkg.TypesInfo.Types[x].IsType() {
		return value{}
	}
	if s, ok := a.location(x, e); ok {
		if v, ok := e.values[s]; ok {
			return v
		}
	}
	switch x := x.(type) {
	case *ast.BasicLit:
		return value{}
	case *ast.Ident:
		return a.source(a.objectOf(x))
	case *ast.FuncLit:
		return value{closure: x}
	case *ast.CallExpr:
		return w.call(x, e, guard)
	case *ast.SelectorExpr:
		return merge(w.eval(x.X, e, guard), a.source(a.objectOf(x)))
	case *ast.ParenExpr:
		return w.eval(x.X, e, guard)
	case *ast.UnaryExpr:
		v := w.eval(x.X, e, guard)
		if x.Op == token.ARROW {
			v = merge(v, w.diagnostic(x.Pos(), "channel-effect", "channel receive and temporal dependencies are not modeled"))
		}
		return v
	case *ast.StarExpr:
		return w.eval(x.X, e, guard)
	case *ast.BinaryExpr:
		return merge(w.eval(x.X, e, guard), w.eval(x.Y, e, guard))
	case *ast.IndexExpr:
		return merge(w.eval(x.X, e, guard), w.eval(x.Index, e, guard))
	case *ast.IndexListExpr:
		return w.eval(x.X, e, guard)
	case *ast.SliceExpr:
		return merge(w.eval(x.X, e, guard), w.eval(x.Low, e, guard), w.eval(x.High, e, guard), w.eval(x.Max, e, guard))
	case *ast.KeyValueExpr:
		return w.eval(x.Value, e, guard)
	case *ast.CompositeLit:
		var v value
		for _, el := range x.Elts {
			v = merge(v, w.eval(el, e, guard))
		}
		return v
	case *ast.TypeAssertExpr:
		return w.eval(x.X, e, guard)
	default:
		return w.diagnostic(x.Pos(), "expression", "unsupported expression")
	}
}
func (a *analyzer) callTarget(c *ast.CallExpr) (string, string) {
	o := a.objectOf(c.Fun)
	if o == nil || o.Pkg() == nil {
		return "", ""
	}
	return o.Pkg().Path(), o.Name()
}
func referenceType(t types.Type) bool {
	if t == nil {
		return false
	}
	switch t := t.Underlying().(type) {
	case *types.Pointer, *types.Slice, *types.Map, *types.Chan, *types.Interface:
		return true
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if referenceType(t.Field(i).Type()) {
				return true
			}
		}
	case *types.Array:
		return referenceType(t.Elem())
	}
	return false
}
func (w *testWalker) invalidate(x ast.Expr, e *environment, v value) {
	if index, ok := x.(*ast.IndexExpr); ok {
		w.invalidate(index.X, e, v)
		return
	}
	if s, ok := w.analyzer.location(x, e); ok {
		e.values[s] = merge(e.values[s], v)
		for k, old := range e.values {
			if k.root == s.root && strings.HasPrefix(k.field, s.field+".") {
				e.values[k] = merge(old, v)
			}
		}
		return
	}
	ast.Inspect(x, func(n ast.Node) bool {
		exp, ok := n.(ast.Expr)
		if !ok {
			return true
		}
		if _, ok := w.analyzer.location(exp, e); ok && referenceType(w.analyzer.pkg.TypesInfo.TypeOf(exp)) {
			w.invalidate(exp, e, v)
			return false
		}
		return true
	})
}
func (w *testWalker) mergeBranches(e, left, right *environment, pos token.Pos) {
	keys := map[slot]bool{}
	for k := range left.values {
		keys[k] = true
	}
	for k := range right.values {
		keys[k] = true
	}
	for k := range keys {
		l, r := left.values[k], right.values[k]
		if reflect.DeepEqual(l, r) {
			e.values[k] = l
		} else {
			e.values[k] = merge(l, r, w.diagnostic(pos, "branch-merge", "value differs across control-flow paths"))
		}
	}
}
