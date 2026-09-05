package main

import (
	"deintroverter4go/internal/analyzer"

	"flag"
	"fmt"
	"os"
)

func main() {
	dir := flag.String("dir", ".", "directory of the Go module to inspect")
	format := flag.String("format", "text", "output format: text or json")
	verbose := flag.Bool("verbose", false, "include extroverted tests in text output")
	var helpers []string
	flag.Func("helper-package", "test infrastructure package or path/... (repeatable; remains loaded)", func(s string) error { helpers = append(helpers, s); return nil })
	flag.Parse()
	if *format != "text" && *format != "json" {
		fmt.Fprintln(os.Stderr, "format must be text or json")
		os.Exit(2)
	}
	patterns := flag.Args()
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	findings, err := analyzer.AnalyzeWithOptions(*dir, patterns, analyzer.Options{HelperPackages: helpers})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := writeReport(os.Stdout, findings, *format, *verbose); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
