package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/grosspoetrysystems/poolboy/bundle"
	"github.com/grosspoetrysystems/poolboy/internal/checkout"
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
		if len(items) > 0 {
			lines = append(lines, "Run `poolboy health` to inspect.")
		}
	}
	lines = append(lines, "")
	if len(inventory.Quarantined) > 0 {
		lines = append(lines, fmt.Sprintf("Filed %d likely-sensitive path(s) in .poolboyignore:", len(inventory.Quarantined)))
		for _, exclusion := range inventory.Quarantined {
			lines = append(lines, fmt.Sprintf("%s\t%s", exclusion.Reason, exclusion.Path))
		}
		lines = append(lines,
			"Filename matches were not opened. Content matches were read once in memory to classify, but were not logged, retained, or hashed.",
			"If any filed path contains a real credential, revoke or rotate it.",
		)
	}
	lines = append(lines, "Practice repository hygiene; Poolboy's secret check is only a narrow backstop.")
	return productOutput(*format, lines, struct {
		Inventory      *source.Inventory  `json:"inventory"`
		Health         []health.Item      `json:"health"`
		HealthError    string             `json:"health_error,omitempty"`
		Quarantined    []source.Exclusion `json:"quarantined,omitempty"`
		SecurityNotice string             `json:"security_notice"`
	}{
		inventory,
		items,
		healthError,
		inventory.Quarantined,
		"Practice repository hygiene. Content matches are read once to classify; if a filed path contains a real credential, revoke or rotate it.",
	})
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

func cmdCheckout(args []string) int {
	fs := flag.NewFlagSet("checkout", flag.ContinueOnError)
	format := fs.String("format", "text", "output format: text|json|csv|tsv")
	if code := productFlags(fs, args); code >= 0 {
		return code
	}
	if fs.NArg() < 1 {
		return productError(errors.New("usage: poolboy checkout DIR [--format json]"))
	}
	path := fs.Arg(0)
	if code := productFlags(fs, fs.Args()[1:]); code >= 0 {
		return code
	}
	if fs.NArg() != 0 {
		return productError(errors.New("checkout requires exactly one target directory"))
	}
	b, err := productBundle()
	if err != nil {
		return productError(err)
	}
	result, err := checkout.Create(b, path)
	if err != nil {
		return productError(err)
	}
	lines := []string{
		"Created checkout: " + result.Path,
		fmt.Sprintf("Base: %d Markdown documents", result.Documents),
		"Edit the checkout, then preview check-in with: poolboy checkin " + result.Path,
	}
	if len(result.Quarantined) > 0 {
		lines = append(lines, fmt.Sprintf("Security review required: %d likely-sensitive source path(s) were quarantined from the checkout observation.", len(result.Quarantined)))
	}
	return productOutput(*format, lines, result)
}

func cmdCheckin(args []string) int {
	fs := flag.NewFlagSet("checkin", flag.ContinueOnError)
	apply := fs.Bool("apply", false, "apply a conflict-free check-in plan to the canonical corpus")
	format := fs.String("format", "text", "output format: text|json|csv|tsv")
	if code := productFlags(fs, args); code >= 0 {
		return code
	}
	if fs.NArg() < 1 {
		return productError(errors.New("usage: poolboy checkin DIR [--apply] [--format json]"))
	}
	path := fs.Arg(0)
	if code := productFlags(fs, fs.Args()[1:]); code >= 0 {
		return code
	}
	if fs.NArg() != 0 {
		return productError(errors.New("checkin requires exactly one checkout directory"))
	}
	b, err := productBundle()
	if err != nil {
		return productError(err)
	}
	plan, err := checkout.Checkin(b, path, *apply)
	if err != nil {
		return productError(err)
	}
	lines := []string{
		fmt.Sprintf("Draft changes: %d", len(plan.DraftChanges)),
		fmt.Sprintf("Concurrent workspace changes: %d", len(plan.WorkspaceChanges)),
		fmt.Sprintf("Source changes since checkout: %d", len(plan.SourceChanges)),
		fmt.Sprintf("Generator changes since checkout: %d", len(plan.GeneratorChanges)),
		fmt.Sprintf("Likely-sensitive source paths: %d", len(plan.SourceQuarantined)),
		fmt.Sprintf("Conflicts: %d", len(plan.Conflicts)),
	}
	for _, conflict := range plan.Conflicts {
		lines = append(lines, "Conflict "+conflict.Path+": "+conflict.Reason)
	}
	for _, quarantined := range plan.SourceQuarantined {
		lines = append(lines, "Quarantined "+quarantined.Path+": "+quarantined.Reason)
	}
	if plan.RequiresSourceReview {
		lines = append(lines, "Source review remains required; check-in does not accept the source baseline.")
	}
	switch {
	case plan.Applied:
		lines = append(lines, "Applied check-in to the canonical corpus.")
	case plan.CanApply:
		lines = append(lines, "Preview only; rerun with --apply to update the canonical corpus.")
	default:
		lines = append(lines, "Not applicable until conflicts are resolved.")
	}
	if code := productOutput(*format, lines, plan); code != 0 {
		return code
	}
	if !plan.CanApply {
		return 1
	}
	return 0
}
