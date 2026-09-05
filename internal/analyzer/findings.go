package analyzer

import (
	"fmt"
	"go/ast"
	"go/token"
	"slices"
)

// Source is a production declaration contributing to an assertion's value.
type Source struct {
	Symbol string `json:"symbol"`
	File   string `json:"file,omitempty"`
	Line   int    `json:"line,omitempty"`
}

// Diagnostic identifies an analysis limit, rather than a test defect.
type Diagnostic struct {
	Code    string `json:"code"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	Message string `json:"message"`
}

// Assertion reports each recognized failure condition, including helper assertions.
type Assertion struct {
	EvidenceTruncated bool         `json:"evidence_truncated,omitempty"`
	File              string       `json:"file"`
	Line              int          `json:"line"`
	Verdict           string       `json:"verdict"`
	Reason            string       `json:"reason"`
	Sources           []Source     `json:"sources,omitempty"`
	Diagnostics       []Diagnostic `json:"diagnostics,omitempty"`
}

// Finding describes a test, subtest, suite method, container, or unsupported entry.
type Finding struct {
	DiagnosticsTruncated bool         `json:"diagnostics_truncated,omitempty"`
	File                 string       `json:"file"`
	Line                 int          `json:"line"`
	Test                 string       `json:"test"`
	Verdict              string       `json:"verdict"`
	Reason               string       `json:"reason"`
	Kind                 string       `json:"kind,omitempty"`
	Children             int          `json:"children,omitempty"`
	Assertions           []Assertion  `json:"assertions,omitempty"`
	Diagnostics          []Diagnostic `json:"diagnostics,omitempty"`
}

type testWalker struct {
	analyzer             *analyzer
	name                 string
	assertions           []Assertion
	diagnostics          []Diagnostic
	diagnosticsTruncated bool
	children             int
	returns              []value
	stack                map[*ast.FuncDecl]bool
	depth                int
	suite                *suiteContext
}

func (w *testWalker) diagnostic(pos token.Pos, code, message string) value {
	p := w.analyzer.pkg.Fset.Position(pos)
	d := Diagnostic{code, p.Filename, p.Line, message}
	if !slices.Contains(w.diagnostics, d) {
		if len(w.diagnostics) < 32 {
			w.diagnostics = append(w.diagnostics, d)
		} else {
			w.diagnosticsTruncated = true
		}
	}
	return value{flags: unknown, diagnostics: []Diagnostic{d}}
}

func (w *testWalker) observe(pos token.Pos, v value) {
	p := w.analyzer.pkg.Fset.Position(pos)
	verdict, reason := classifyValue(v)
	w.assertions = append(w.assertions, Assertion{v.truncated, p.Filename, p.Line, verdict, reason, v.sources, v.diagnostics})
}

func classifyValue(v value) (string, string) {
	if v.flags&unknown != 0 {
		return "questionable", "assertion has unresolved dependencies; see diagnostics"
	}
	if v.flags&sut != 0 {
		return "extroverted", "assertion traces to production code"
	}
	if v.flags&helper != 0 {
		return "cloistered", "assertion reaches untraced test infrastructure"
	}
	return "introverted", "no assertion traced to production code"
}

func (w *testWalker) finding(pos token.Pos, kind string) Finding {
	verdict, reason := "questionable", "no recognized assertions"
	counts := map[string]int{}
	for _, a := range w.assertions {
		counts[a.Verdict]++
	}
	if len(w.assertions) > 0 {
		verdict = "introverted"
		for _, v := range []string{"cloistered", "questionable", "extroverted"} {
			if counts[v] > 0 {
				verdict = v
			}
		}
		reason = fmt.Sprintf("%d assertions: %d extroverted, %d introverted, %d cloistered, %d questionable", len(w.assertions), counts["extroverted"], counts["introverted"], counts["cloistered"], counts["questionable"])
	}
	if w.children > 0 {
		reason = fmt.Sprintf("%d child entries; %s", w.children, reason)
	}
	if len(w.assertions) == 0 && w.children > 0 {
		verdict = "container"
		reason = "assertions are reported in child entries"
	}
	p := w.analyzer.pkg.Fset.Position(pos)
	return Finding{w.diagnosticsTruncated, p.Filename, p.Line, w.name, verdict, reason, kind, w.children, w.assertions, w.diagnostics}
}

func (a *analyzer) analyzeTest(name string, body *ast.BlockStmt, env *environment, pos token.Pos, kind string, suite *suiteContext) {
	w := &testWalker{analyzer: a, name: name, stack: map[*ast.FuncDecl]bool{}, suite: suite}
	w.walk(body, env, value{})
	a.findings = append(a.findings, w.finding(pos, kind))
}
