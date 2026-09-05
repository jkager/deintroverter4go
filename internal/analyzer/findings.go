package analyzer

import (
	"fmt"
	"go/ast"
	"go/token"
)

// Finding describes the classification of a test or inline subtest.
type Finding struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Test    string `json:"test"`
	Verdict string `json:"verdict"`
	Reason  string `json:"reason"`
}

type observation struct {
	value origin
	line  int
}

type testWalker struct {
	analyzer   *analyzer
	name       string
	assertions []observation
	children   int
}

func (a *analyzer) analyzeTest(name string, body *ast.BlockStmt, env environment, pos token.Pos) {
	walker := testWalker{analyzer: a, name: name}
	walker.walk(body, env, 0)
	if len(walker.assertions) == 0 && walker.children > 0 {
		return
	}
	verdict, reason := classify(walker.assertions)
	position := a.pkg.Fset.Position(pos)
	a.findings = append(a.findings, Finding{
		File: position.Filename, Line: position.Line, Test: name, Verdict: verdict, Reason: reason,
	})
}

func classify(assertions []observation) (string, string) {
	if len(assertions) == 0 {
		return "questionable", "no recognized assertions (possibly a custom helper or smoke test)"
	}
	var combined origin
	for _, assertion := range assertions {
		if assertion.value&sut != 0 && assertion.value&unknown == 0 {
			return "extroverted", fmt.Sprintf("assertion at line %d traces to production code", assertion.line)
		}
		combined |= assertion.value
	}
	if combined&unknown != 0 {
		return "questionable", "assertion depends on unsupported control flow, mutation, or an opaque call"
	}
	if combined&helper != 0 {
		return "cloistered", "assertion reaches test code; helper bodies are not traced"
	}
	return "introverted", "no assertion traced to production code"
}
