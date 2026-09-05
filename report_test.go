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
		if !strings.Contains(output.String(), "2 tests: 1 extroverted, 1 introverted, 0 cloistered, 0 questionable\n") {
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
