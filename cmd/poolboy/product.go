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
	b, err := productBundle()
	if err != nil {
		return productError(err)
	}
	inventory, err := source.Scan(b, *accept)
	if err != nil {
		return productError(err)
	}
	return productOutput(*format, []string{"Saved evidence baseline: .poolboy/sources.lock.json"}, inventory)
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
	b, err := productBundle()
	if err != nil {
		return productError(err)
	}
	changes, err := source.Drift(b)
	if err != nil {
		return productError(err)
	}
	lines := make([]string, 0, len(changes))
	for _, change := range changes {
		lines = append(lines, fmt.Sprintf("%s\t%s", change.Status, change.Path))
	}
	return productOutput(*format, lines, changes)
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
