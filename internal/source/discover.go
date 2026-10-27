package source

import (
	"bytes"
	"regexp"
	"sort"
	"strings"

	"github.com/grosspoetrysystems/poolboy/bundle"
)

// DiscoveryLink is a literal HTTP(S) URL found in a source file that passed
// the same admission boundary as source.Current. Content is not retained.
type DiscoveryLink struct {
	URL  string
	Path string
	Line int
}

var sourceDiscoveryURL = regexp.MustCompile(`https?://[^\s"'<>]+`)

// DiscoveryLinks rereads only currently admitted, bounded source files and
// returns literal URL evidence with path/line provenance. It deliberately does
// not read symlinks, ignored files, binary files, secret-looking files, files
// over the scanner limit, or the configured corpus/output/config boundaries.
func DiscoveryLinks(b *bundle.Bundle) ([]DiscoveryLink, error) {
	root, err := projectRoot(b)
	if err != nil {
		return nil, err
	}
	dirs, files, err := buildExclusions(b, root)
	if err != nil {
		return nil, err
	}
	var links []DiscoveryLink
	s := &scanner{
		root:          root,
		excludedDirs:  dirs,
		excludedFiles: files,
		entries:       make(map[string]Entry),
		quarantined:   make(map[string]Exclusion),
		onFile: func(path string, data []byte) {
			lineNumber := 0
			for line := range bytes.Lines(data) {
				lineNumber++
				for _, match := range sourceDiscoveryURL.FindAll(line, -1) {
					raw := trimDiscoveryURL(string(match))
					if raw != "" {
						links = append(links, DiscoveryLink{URL: raw, Path: path, Line: lineNumber})
					}
				}
			}
		},
	}
	if err := s.walk(root, nil, nil); err != nil {
		return nil, err
	}
	sort.SliceStable(links, func(i, j int) bool {
		if links[i].Path != links[j].Path {
			return links[i].Path < links[j].Path
		}
		if links[i].Line != links[j].Line {
			return links[i].Line < links[j].Line
		}
		return links[i].URL < links[j].URL
	})
	return links, nil
}

func trimDiscoveryURL(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimRight(raw, ".,;:!?]")
	for strings.HasSuffix(raw, ")") && strings.Count(raw, ")") > strings.Count(raw, "(") {
		raw = strings.TrimSuffix(raw, ")")
	}
	return raw
}
