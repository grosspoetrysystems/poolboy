package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/grosspoetrysystems/poolboy/bundle"
	"github.com/grosspoetrysystems/poolboy/internal/compiler"
	"github.com/grosspoetrysystems/poolboy/internal/health"
	"github.com/grosspoetrysystems/poolboy/internal/output"
	"github.com/grosspoetrysystems/poolboy/internal/source"
)

func productFlags(fs *flag.FlagSet, args []string) int {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	return -1
}

func productBundle() (*bundle.Bundle, error) {
	start := rootDir
	if start == "" {
		start = "."
	}
	return bundle.Discover(start)
}
func productPartialBundle() (*bundle.Bundle, error) {
	start := rootDir
	if start == "" {
		start = "."
	}
	return bundle.DiscoverPartial(start)
}

func productError(err error) int {
	fmt.Fprintln(os.Stderr, "poolboy:", err)
	return 2
}

func productOutput(format string, lines []string, value any) int {
	if err := output.Emit(os.Stdout, format, lines, value); err != nil {
		return productError(err)
	}
	return 0
}

func cmdBuild(args []string) int {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	renderer := fs.String("renderer", "", "trusted Knap companion path (default: poolboy-knap.mjs beside poolboy)")
	format := fs.String("format", "text", "output format: text|json|csv|tsv")
	if code := productFlags(fs, args); code >= 0 {
		return code
	}
	if fs.NArg() != 0 {
		return productError(errors.New("build accepts no positional arguments"))
	}
	b, err := productBundle()
	if err != nil {
		return productError(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := compiler.Build(ctx, b, *renderer); err != nil {
		return productError(err)
	}
	return productOutput(*format, []string{"Built static corpus: " + b.Output}, struct {
		Output string `json:"output"`
	}{b.Output})
}

func cmdScan(args []string) int {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	accept := fs.Bool("accept", false, "replace an existing evidence baseline after completing review")
	format := fs.String("format", "text", "output format: text|json|csv|tsv")
	if code := productFlags(fs, args); code >= 0 {
		return code
	}
	if fs.NArg() != 0 {
		return productError(errors.New("scan accepts no positional arguments"))
	}
	b, err := productPartialBundle()
	if err != nil {
		return productError(err)
	}
	previous, inventory, err := source.ScanEvidence(b, *accept)
	if err != nil {
		return productError(err)
	}
	items, healthErr := health.Inspect(b, previous, inventory)
	healthError := ""
	lines := []string{"Saved source baseline: .poolboy/sources.lock.json", ""}
	if healthErr != nil {
		healthError = healthErr.Error()
		lines = append(lines, "Documentation smell test unavailable: "+healthError)
	} else {
		lines = append(lines, healthSummary(items)...)
		lines = append(lines, "", "Nothing was excluded or rewritten.")
		if len(items) > 0 {
			lines = append(lines, "Run `poolboy health` to inspect.")
		}
	}
	return productOutput(*format, lines, struct {
		Inventory   *source.Inventory `json:"inventory"`
		Health      []health.Item     `json:"health"`
		HealthError string            `json:"health_error,omitempty"`
	}{inventory, items, healthError})
}

func cmdDrift(args []string) int {
	fs := flag.NewFlagSet("drift", flag.ContinueOnError)
	format := fs.String("format", "text", "output format: text|json|csv|tsv")
	if code := productFlags(fs, args); code >= 0 {
		return code
	}
	if fs.NArg() != 0 {
		return productError(errors.New("drift accepts no positional arguments"))
	}
	b, err := productPartialBundle()
	if err != nil {
		return productError(err)
	}
	changes, baseline, current, err := source.DriftEvidence(b)
	if err != nil {
		return productError(err)
	}
	items, healthErr := health.Inspect(b, baseline, current)
	healthError := ""
	lines := make([]string, 0, len(changes)+8)
	for _, change := range changes {
		lines = append(lines, fmt.Sprintf("%s\t%s", change.Status, change.Path))
	}
	if len(lines) > 0 {
		lines = append(lines, "")
	}
	if healthErr != nil {
		healthError = healthErr.Error()
		lines = append(lines, "Documentation smell test unavailable: "+healthError)
	} else {
		lines = append(lines, healthSummary(items)...)
	}
	value := any(struct {
		Changes     []source.Change `json:"changes"`
		Health      []health.Item   `json:"health"`
		HealthError string          `json:"health_error,omitempty"`
	}{changes, items, healthError})
	if *format == "csv" || *format == "tsv" {
		value = changes
	}
	return productOutput(*format, lines, value)
}

func cmdHealth(args []string) int {
	fs := flag.NewFlagSet("health", flag.ContinueOnError)
	format := fs.String("format", "text", "output format: text|json|csv|tsv")
	if code := productFlags(fs, args); code >= 0 {
		return code
	}
	if fs.NArg() != 0 {
		return productError(errors.New("health accepts no positional arguments"))
	}
	b, err := productPartialBundle()
	if err != nil {
		return productError(err)
	}
	items, err := health.Current(b)
	if err != nil {
		return productError(err)
	}
	lines := make([]string, 0, len(items))
	for _, item := range items {
		if item.Status == health.Unavailable {
			lines = append(lines, fmt.Sprintf("unavailable\t%s", item.Signal))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s\t%s\t%s", item.Signal, item.Document, item.Source))
	}
	if len(lines) == 0 {
		lines = append(lines, "No documentation health findings.")
	}
	return productOutput(*format, lines, items)
}

func healthSummary(items []health.Item) []string {
	counts := map[string]int{}
	for _, item := range items {
		counts[item.Signal]++
	}
	lines := []string{"Documentation smell test:"}
	for _, signal := range []string{"orphan", "missing_sources", "source_missing", "source_not_in_baseline", "source_changed"} {
		if counts[signal] > 0 {
			lines = append(lines, fmt.Sprintf("  %d %s", counts[signal], signal))
		}
	}
	for _, signal := range []string{"corpus", "source_baseline"} {
		if counts[signal] > 0 {
			lines = append(lines, fmt.Sprintf("  %s unavailable", signal))
		}
	}
	if len(lines) == 1 {
		lines = append(lines, "  no findings")
	}
	return lines
}

func cmdAffected(args []string) int {
	fs := flag.NewFlagSet("affected", flag.ContinueOnError)
	format := fs.String("format", "text", "output format: text|json|csv|tsv")
	if code := productFlags(fs, args); code >= 0 {
		return code
	}
	if fs.NArg() >= 1 {
		path := fs.Arg(0)
		if code := productFlags(fs, fs.Args()[1:]); code >= 0 {
			return code
		}
		if fs.NArg() != 0 {
			return productError(errors.New("affected requires exactly one project-relative source path"))
		}
		b, err := productBundle()
		if err != nil {
			return productError(err)
		}
		paths, err := source.Affected(b, path)
		if err != nil {
			return productError(err)
		}
		return productOutput(*format, paths, paths)
	}
	return productError(errors.New("usage: poolboy affected SOURCE [--format json]"))
}
