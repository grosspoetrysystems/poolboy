// Package health reports deterministic documentation review evidence.
package health

import (
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/grosspoetrysystems/poolboy/bundle"
	"github.com/grosspoetrysystems/poolboy/index"
	"github.com/grosspoetrysystems/poolboy/internal/source"
)

const (
	// Finding marks an observable document review candidate.
	Finding = "finding"
	// Unavailable marks a check whose required input does not exist.
	Unavailable = "unavailable"
)

// Item is one deterministic finding or unavailable check.
type Item struct {
	Document string `json:"document,omitempty"`
	Signal   string `json:"signal"`
	Source   string `json:"source,omitempty"`
	Status   string `json:"status"`
}

// Current inspects the worktree against the accepted source baseline.
func Current(b *bundle.Bundle) ([]Item, error) {
	current, err := source.Current(b)
	if err != nil {
		return nil, err
	}
	baseline, err := source.ReadLock(b)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return Inspect(b, baseline, current)
}

// Inspect joins corpus metadata and graph evidence with two source snapshots.
// A nil baseline means drift evidence is unavailable, not clean.
func Inspect(b *bundle.Bundle, baseline, current *source.Inventory) ([]Item, error) {
	items := make([]Item, 0)
	if baseline == nil {
		items = append(items, Item{Signal: "source_baseline", Status: Unavailable})
	}

	idx, err := index.Build(b)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return append(items, Item{Signal: "corpus", Status: Unavailable}), nil
		}
		return nil, err
	}
	orphans := make(map[string]bool)
	for _, entry := range idx.Orphans() {
		orphans[entry.Path] = true
	}
	for _, entry := range idx.Entries {
		resources := resources(entry.Frontmatter()["sources"])
		if entry.Path != "/index.md" && len(resources) == 0 {
			items = append(items, Item{Document: entry.Path, Signal: "missing_sources", Status: Finding})
		}
		document := filepath.Join(b.Dir, filepath.FromSlash(strings.TrimPrefix(entry.Path, "/")))
		for _, raw := range resources {
			resource, ok := projectResource(b.Root, document, raw)
			if !ok {
				continue
			}
			_, statErr := os.Lstat(filepath.Join(b.Root, filepath.FromSlash(resource)))
			switch {
			case errors.Is(statErr, fs.ErrNotExist):
				items = append(items, Item{Document: entry.Path, Signal: "source_missing", Source: resource, Status: Finding})
			case statErr != nil:
				return nil, statErr
			case baseline == nil:
				continue
			default:
				before, ok := baseline.Files[resource]
				if !ok {
					items = append(items, Item{Document: entry.Path, Signal: "source_not_in_baseline", Source: resource, Status: Finding})
					continue
				}
				now, ok := current.Files[resource]
				if ok && now.SHA256 != before.SHA256 {
					items = append(items, Item{Document: entry.Path, Signal: "source_changed", Source: resource, Status: Finding})
				}
			}
		}
		if orphans[entry.Path] {
			items = append(items, Item{Document: entry.Path, Signal: "orphan", Status: Finding})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Document != items[j].Document {
			return items[i].Document < items[j].Document
		}
		pi, pj := signalOrder(items[i].Signal), signalOrder(items[j].Signal)
		if pi != pj {
			return pi < pj
		}
		return items[i].Source < items[j].Source
	})
	return items, nil
}

func projectResource(root, document, raw string) (string, bool) {
	raw = bundle.NormalizeResource(raw)
	u, err := url.Parse(raw)
	if raw == "" || err != nil || u.Scheme != "" {
		return "", false
	}
	resolved, err := bundle.ResolveResourcePath(root, document, raw)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func resources(value any) []string {
	var result []string
	var visit func(any)
	visit = func(value any) {
		switch value := value.(type) {
		case map[string]any:
			if resource, ok := value["resource"].(string); ok {
				if resource = bundle.NormalizeResource(resource); resource != "" {
					result = append(result, resource)
				}
			}
		case []any:
			for _, item := range value {
				visit(item)
			}
		case []string:
			for _, item := range value {
				if item = bundle.NormalizeResource(item); item != "" {
					result = append(result, item)
				}
			}
		}
	}
	visit(value)
	return result
}

func signalOrder(signal string) int {
	switch signal {
	case "missing_sources":
		return 0
	case "source_missing":
		return 1
	case "source_not_in_baseline":
		return 2
	case "source_changed":
		return 3
	case "orphan":
		return 4
	default:
		return 5
	}
}
