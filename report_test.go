package main

import (
	"deintroverter4go/internal/analyzer"

	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestWriteReport(t *testing.T) {
	findings := []analyzer.Finding{
		{File: "sample_test.go", Line: 10, Test: "TestProduction", Verdict: "extroverted", Reason: "production result"},
		{File: "sample_test.go", Line: 20, Test: "TestLiteral", Verdict: "introverted", Reason: "literal comparison"},
	}
	for _, verbose := range []bool{false, true} {
		var output bytes.Buffer
		if err := writeReport(&output, findings, "text", verbose); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(output.String(), "TestProduction") != verbose {
			t.Errorf("verbose=%t: unexpected visibility: %s", verbose, &output)
		}
		if !strings.Contains(output.String(), "sample_test.go:20  TestLiteral  introverted\n  literal comparison\n") {
			t.Errorf("missing finding: %s", &output)
		}
		if !strings.Contains(output.String(), "2 entries: 1 extroverted, 1 introverted, 0 cloistered, 0 questionable, 0 containers\n") {
			t.Errorf("wrong summary: %s", &output)
		}
	}
	var output bytes.Buffer
	if err := writeReport(&output, findings, "json", false); err != nil {
		t.Fatal(err)
	}
	var decoded []analyzer.Finding
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, findings) {
		t.Errorf("JSON lost findings: %+v", decoded)
	}
}

func TestMixedFindingRemainsVisible(t *testing.T) {
	findings := []analyzer.Finding{{
		File: "sample_test.go", Line: 10, Test: "TestMixed", Verdict: "extroverted", Kind: "test",
		Assertions: []analyzer.Assertion{
			{File: "sample_test.go", Line: 11, Verdict: "extroverted", Sources: []analyzer.Source{{Symbol: "Add", File: "prod.go", Line: 3}}},
			{File: "sample_test.go", Line: 12, Verdict: "introverted", Reason: "literal check"},
		},
		Diagnostics: []analyzer.Diagnostic{{Code: "helper-limit", File: "helper_test.go", Line: 4, Message: "recursion"}},
	}}
	for _, format := range []string{"text", "json"} {
		var out bytes.Buffer
		if err := writeReport(&out, findings, format, false); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "TestMixed") || !strings.Contains(out.String(), "literal check") || !strings.Contains(out.String(), "helper-limit") {
			t.Fatalf("%s hides evidence: %s", format, &out)
		}
		if format == "json" {
			var got []analyzer.Finding
			if err := json.Unmarshal(out.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, findings) {
				t.Fatalf("JSON lost assertion evidence: %+v", got)
			}
		}
	}
	var verbose bytes.Buffer
	if err := writeReport(&verbose, findings, "text", true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(verbose.String(), "production: Add (prod.go:3)") {
		t.Fatalf("missing endpoint: %s", &verbose)
	}
}
