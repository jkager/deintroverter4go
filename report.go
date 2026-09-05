package main

import (
	"deintroverter4go/internal/analyzer"

	"encoding/json"
	"fmt"
	"io"
)

func writeReport(output io.Writer, findings []analyzer.Finding, format string, verbose bool) error {
	if format == "json" {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(findings)
	}
	counts := map[string]int{}
	for _, finding := range findings {
		counts[finding.Verdict]++
		if verbose || finding.Verdict != "extroverted" {
			if _, err := fmt.Fprintf(output, "%s:%d  %s  %s\n  %s\n", finding.File, finding.Line, finding.Test, finding.Verdict, finding.Reason); err != nil {
				return err
			}
		}
	}
	_, err := fmt.Fprintf(output, "%d tests: %d extroverted, %d introverted, %d cloistered, %d questionable\n", len(findings), counts["extroverted"], counts["introverted"], counts["cloistered"], counts["questionable"])
	return err
}
