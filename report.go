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
	assertions := 0
	unsupported := 0
	for _, f := range findings {
		counts[f.Verdict]++
		assertions += len(f.Assertions)
		if f.Kind == "unsupported" {
			unsupported++
		}
		show := verbose || f.Verdict != "extroverted" || len(f.Diagnostics) > 0
		for _, a := range f.Assertions {
			if a.Verdict != "extroverted" {
				show = true
			}
		}
		if !show {
			continue
		}
		if _, err := fmt.Fprintf(output, "%s:%d  %s  %s\n  %s\n", f.File, f.Line, f.Test, f.Verdict, f.Reason); err != nil {
			return err
		}
		for _, a := range f.Assertions {
			if !verbose && a.Verdict == "extroverted" {
				continue
			}
			if _, err := fmt.Fprintf(output, "  %s:%d  assertion %s: %s\n", a.File, a.Line, a.Verdict, a.Reason); err != nil {
				return err
			}
			if a.EvidenceTruncated {
				if _, err := fmt.Fprintln(output, "    evidence truncated (representative paths shown)"); err != nil {
					return err
				}
			}
			for _, s := range a.Sources {
				if _, err := fmt.Fprintf(output, "    production: %s (%s:%d)\n", s.Symbol, s.File, s.Line); err != nil {
					return err
				}
			}
			for _, d := range a.Diagnostics {
				if _, err := fmt.Fprintf(output, "    [%s] %s:%d: %s\n", d.Code, d.File, d.Line, d.Message); err != nil {
					return err
				}
			}
		}
		if f.DiagnosticsTruncated {
			if _, err := fmt.Fprintln(output, "  diagnostics truncated (representative limits shown)"); err != nil {
				return err
			}
		}
		for _, d := range f.Diagnostics {
			if _, err := fmt.Fprintf(output, "  [%s] %s:%d: %s\n", d.Code, d.File, d.Line, d.Message); err != nil {
				return err
			}
		}
	}
	_, err := fmt.Fprintf(output, "%d entries: %d extroverted, %d introverted, %d cloistered, %d questionable, %d containers\n%d recognized assertions; %d unsupported entries (static discovery, not executed test counts)\n", len(findings), counts["extroverted"], counts["introverted"], counts["cloistered"], counts["questionable"], counts["container"], assertions, unsupported)
	return err
}
