// Package analyzer traces Go test assertions to production code.
package analyzer

import (
	"fmt"
	"go/ast"
	"go/types"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/packages"
)

type analyzer struct {
	pkg      *packages.Package
	module   string
	findings []Finding
}

// Analyze loads Go package patterns relative to dir and returns findings sorted
// by file and line. It does not execute tests. Package loading errors are returned.
func Analyze(dir string, patterns []string) ([]Finding, error) {
	cfg := &packages.Config{Dir: dir, Tests: true, Mode: packages.LoadSyntax | packages.NeedModule}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, err
	}
	result := []Finding{}
	seen := map[string]bool{}
	for _, p := range pkgs {
		if len(p.Errors) > 0 {
			return nil, fmt.Errorf("%s", p.Errors[0])
		}
		a := analyzer{pkg: p}
		if p.Module != nil {
			a.module = p.Module.Path
		}
		for _, f := range p.Syntax {
			filename := p.Fset.Position(f.Pos()).Filename
			if !strings.HasSuffix(filename, "_test.go") || seen[filename] {
				continue
			}
			seen[filename] = true
			for _, d := range f.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || !isTestFunction(p.TypesInfo, fn) {
					continue
				}
				a.analyzeTest(fn.Name.Name, fn.Body, environment{}, fn.Pos())
			}
		}
		result = append(result, a.findings...)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].File == result[j].File {
			return result[i].Line < result[j].Line
		}
		return result[i].File < result[j].File
	})
	return result, nil
}

func isTestFunction(info *types.Info, fn *ast.FuncDecl) bool {
	if fn.Recv != nil || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, "Test") {
		return false
	}
	suffix := strings.TrimPrefix(fn.Name.Name, "Test")
	first, _ := utf8.DecodeRuneInString(suffix)
	if suffix != "" && unicode.IsLower(first) {
		return false
	}
	signature, ok := info.Defs[fn.Name].Type().(*types.Signature)
	return ok && signature.Params().Len() == 1 && signature.Results().Len() == 0 &&
		types.TypeString(signature.Params().At(0).Type(), nil) == "*testing.T"
}
