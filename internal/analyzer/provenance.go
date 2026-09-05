package analyzer

import (
	"go/ast"
	"go/types"
	"strings"
)

type origin uint8

const (
	sut origin = 1 << iota
	helper
	unknown
)

type environment map[types.Object]origin

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

func (a *analyzer) sourceOrigin(o types.Object) origin {
	if o == nil || o.Pkg() == nil {
		return 0
	}
	// Types alone do not establish that a test exercises production behavior.
	switch o.(type) {
	case *types.TypeName, *types.PkgName:
		return 0
	}
	path := o.Pkg().Path()
	if a.module == "" || !(path == a.module || strings.HasPrefix(path, a.module+"/")) {
		return 0
	}
	if strings.HasSuffix(a.pkg.Fset.Position(o.Pos()).Filename, "_test.go") || strings.HasSuffix(path, "_test") {
		return helper
	}
	if o.Parent() == o.Pkg().Scope() {
		return sut
	}
	if f, ok := o.(*types.Func); ok && f.Type().(*types.Signature).Recv() != nil {
		return sut
	}
	return 0
}

func (a *analyzer) expressionOrigin(e ast.Expr, env environment) origin {
	if e == nil {
		return 0
	}
	var result origin
	ast.Inspect(e, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			result |= unknown
			return false
		case *ast.Ident:
			o := a.pkg.TypesInfo.ObjectOf(n)
			if v, ok := env[o]; ok {
				result |= v
			} else {
				result |= a.sourceOrigin(o)
			}
		case *ast.CallExpr:
			o := a.objectOf(n.Fun)
			if o == nil {
				result |= unknown
			} else if _, ok := o.(*types.Var); ok {
				result |= unknown
			}
		}
		return true
	})
	return result
}

func (a *analyzer) callTarget(c *ast.CallExpr) (string, string) {
	o := a.objectOf(c.Fun)
	if o == nil || o.Pkg() == nil {
		return "", ""
	}
	return o.Pkg().Path(), o.Name()
}

func (e environment) markUnknown() {
	for object, value := range e {
		e[object] = value | unknown
	}
}
