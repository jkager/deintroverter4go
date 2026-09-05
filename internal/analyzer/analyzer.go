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

// Options identifies test infrastructure in ordinary .go packages. Patterns are
// module-relative or full import paths, optionally ending in /... . They change
// provenance, not test selection, and are loaded so their helpers can be traced.
type Options struct{ HelperPackages []string }
type declaration struct {
	pkg *packages.Package
	fn  *ast.FuncDecl
}
type analyzer struct {
	pkg          *packages.Package
	module       string
	options      Options
	declarations map[*types.Func]declaration
	findings     []Finding
}

func Analyze(dir string, patterns []string) ([]Finding, error) {
	return AnalyzeWithOptions(dir, patterns, Options{})
}
func AnalyzeWithOptions(dir string, patterns []string, options Options) ([]Finding, error) {
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	cfg := &packages.Config{Dir: dir, Tests: true, Mode: packages.LoadSyntax | packages.NeedModule}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, err
	}
	selectedFiles := map[string]bool{}
	for _, p := range pkgs {
		for _, f := range p.Syntax {
			selectedFiles[p.Fset.Position(f.Pos()).Filename] = true
		}
	}
	// Load configured helper packages together with test roots to keep type object
	// identity consistent, without requesting syntax for every third-party dependency.
	if len(options.HelperPackages) > 0 {
		module := ""
		for _, p := range pkgs {
			if p.Module != nil {
				module = p.Module.Path
				break
			}
		}
		load := append([]string{}, patterns...)
		for _, p := range options.HelperPackages {
			if p == "" || strings.ContainsAny(p, "*?[]") || (strings.Contains(p, "...") && !strings.HasSuffix(p, "/...")) {
				return nil, fmt.Errorf("invalid helper package pattern %q: use a package path or path/...", p)
			}
			if !strings.HasPrefix(p, "./") && p != module && !strings.HasPrefix(p, module+"/") {
				p = "./" + p
			}
			load = append(load, p)
		}
		pkgs, err = packages.Load(cfg, load...)
		if err != nil {
			return nil, err
		}
	}
	declarations := map[*types.Func]declaration{}
	for _, p := range pkgs {
		if len(p.Errors) > 0 {
			return nil, fmt.Errorf("%s", p.Errors[0])
		}
		for _, f := range p.Syntax {
			for _, d := range f.Decls {
				if fn, ok := d.(*ast.FuncDecl); ok && fn.Body != nil {
					if obj, ok := p.TypesInfo.Defs[fn.Name].(*types.Func); ok {
						declarations[obj] = declaration{p, fn}
					}
				}
			}
		}
	}
	result := []Finding{}
	seen := map[string]bool{}
	for _, p := range pkgs {
		a := &analyzer{pkg: p, options: options, declarations: declarations}
		if p.Module != nil {
			a.module = p.Module.Path
		}
		for _, f := range p.Syntax {
			filename := p.Fset.Position(f.Pos()).Filename
			if !selectedFiles[filename] || !strings.HasSuffix(filename, "_test.go") || seen[filename] {
				continue
			}
			seen[filename] = true
			for _, d := range f.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				if isTestFunction(p.TypesInfo, fn) {
					a.analyzeTest(fn.Name.Name, fn.Body, newEnvironment(), fn.Pos(), "test", nil)
				} else if fn.Recv == nil && (testName(fn.Name.Name, "Fuzz") || testName(fn.Name.Name, "Example")) {
					w := &testWalker{analyzer: a, name: fn.Name.Name}
					w.diagnostic(fn.Pos(), "unsupported-entry", "fuzz tests and examples are discovered but not analyzed")
					a.findings = append(a.findings, w.finding(fn.Pos(), "unsupported"))
				}
			}
		}
		result = append(result, a.findings...)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].File != result[j].File {
			return result[i].File < result[j].File
		}
		if result[i].Line != result[j].Line {
			return result[i].Line < result[j].Line
		}
		return result[i].Test < result[j].Test
	})
	return result, nil
}
func (a *analyzer) isHelperPackage(path string) bool {
	for _, p := range a.options.HelperPackages {
		p = strings.TrimPrefix(p, "./")
		if p != a.module && !strings.HasPrefix(p, a.module+"/") {
			p = a.module + "/" + p
		}
		if strings.HasSuffix(p, "/...") {
			base := strings.TrimSuffix(p, "/...")
			if path == base || strings.HasPrefix(path, base+"/") {
				return true
			}
		} else if path == p {
			return true
		}
	}
	return false
}
func testName(name, prefix string) bool {
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	suffix := strings.TrimPrefix(name, prefix)
	first, _ := utf8.DecodeRuneInString(suffix)
	return suffix == "" || !unicode.IsLower(first)
}
func isTestFunction(info *types.Info, fn *ast.FuncDecl) bool {
	if fn.Recv != nil || fn.Body == nil || !testName(fn.Name.Name, "Test") {
		return false
	}
	sig, ok := info.Defs[fn.Name].Type().(*types.Signature)
	return ok && sig.Params().Len() == 1 && sig.Results().Len() == 0 && types.TypeString(sig.Params().At(0).Type(), nil) == "*testing.T"
}
