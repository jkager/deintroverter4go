package analyzer

import (
	"fmt"
	"testing"
)

func TestEvidenceLimitPreservesUncertainty(t *testing.T) {
	var v value
	for i := 0; i < evidenceLimit+5; i++ {
		v = merge(v, value{flags: sut, sources: []Source{{Symbol: fmt.Sprint(i)}}, diagnostics: []Diagnostic{{Code: fmt.Sprint(i)}}})
	}
	// Uncertainty must survive even when its diagnostic falls beyond the limit.
	v = merge(v, value{flags: unknown, diagnostics: []Diagnostic{{Code: "last"}}})
	verdict, _ := classifyValue(v)
	if !v.truncated || len(v.sources) != evidenceLimit || len(v.diagnostics) != evidenceLimit || verdict != "questionable" {
		t.Fatalf("lost bounded evidence or uncertainty: %+v (%s)", v, verdict)
	}
}
