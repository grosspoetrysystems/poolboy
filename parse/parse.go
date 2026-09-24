// Package parse provides targeted extractors for Poolboy Markdown:
// frontmatter, links, tasks, and headings. It intentionally avoids a full
// Markdown AST.
package parse

import (
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Link is a root-absolute internal markdown link [Text](/target.md).
type Link struct {
	Text   string `json:"text"`
	Target string `json:"target"` // anchor stripped, e.g. /finance/income/x.md
	Line   int    `json:"line"`
}

// Checkbox is a GFM checkbox item.
type Checkbox struct {
	Done bool   `json:"done"`
	Text string `json:"text"`
	Line int    `json:"line"`
}

// Heading is an ATX heading.
type Heading struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
	Line  int    `json:"line"`
}

// Table is a parsed GFM table: the header cells and the data rows (each row
// padded or truncated to the header's width).
type Table struct {
	Header []string   `json:"header"`
	Rows   [][]string `json:"rows"`
}

// Frontmatter splits a leading YAML frontmatter block from the body. Scalars
// remain strings (including numbers, booleans, and dates); nested mappings and
// sequences are retained so OKF provenance is inspectable.
func Frontmatter(content string) (map[string]any, string) {
	block, body, ok := frontmatterParts(content)
	if !ok {
		return map[string]any{}, content
	}
	fm, err := decodeYAML(block)
	if err != nil {
		return map[string]any{}, body
	}
	return fm, body
}

func frontmatterParts(content string) (block, body string, ok bool) {
	rest, opened := strings.CutPrefix(content, "---\n")
	if !opened {
		rest, opened = strings.CutPrefix(content, "---\r\n")
	}
	if !opened {
		return "", content, false
	}
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", content, false
	}
	// A fence must occupy the complete line. This avoids treating a prose line
	// such as "\n---not-a-fence" as the closing delimiter.
	closing := rest[end+1:]
	if len(closing) > 3 && closing[3] != '\n' && closing[3] != '\r' {
		return "", content, false
	}
	block = strings.ReplaceAll(rest[:end], "\r\n", "\n")
	if nl := strings.IndexByte(closing, '\n'); nl >= 0 {
		body = closing[nl+1:]
	} else {
		body = ""
	}
	return block, body, true
}

const (
	maxYAMLNodes           = 16_384
	maxYAMLAliasExpansions = 1_024
	maxYAMLDepth           = 128
)

type yamlBudget struct {
	nodes      int
	aliasNodes int
}

func (b *yamlBudget) visit(depth int, alias bool) error {
	if depth > maxYAMLDepth {
		return fmt.Errorf("YAML nesting exceeds %d levels", maxYAMLDepth)
	}
	b.nodes++
	if b.nodes > maxYAMLNodes {
		return fmt.Errorf("YAML expansion exceeds %d nodes", maxYAMLNodes)
	}
	if alias {
		b.aliasNodes++
		if b.aliasNodes > maxYAMLAliasExpansions {
			return fmt.Errorf("YAML alias expansion exceeds %d aliases", maxYAMLAliasExpansions)
		}
	}
	return nil
}

func decodeYAML(block string) (map[string]any, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(block), &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) == 0 {
		return map[string]any{}, nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("frontmatter must be a YAML mapping")
	}
	value, err := yamlValue(root, map[*yaml.Node]bool{}, &yamlBudget{}, 0)
	if err != nil {
		return nil, err
	}
	fm, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("frontmatter must be a YAML mapping")
	}
	return fm, nil
}

// yamlValue deliberately uses Node.Value rather than decoding into interface{}:
// yaml.v3 would otherwise coerce dates, booleans, and numbers to native types,
// changing the established opaque-frontmatter contract.
func yamlValue(n *yaml.Node, active map[*yaml.Node]bool, budget *yamlBudget, depth int) (any, error) {
	if n == nil {
		return nil, nil
	}
	alias := n.Kind == yaml.AliasNode
	if err := budget.visit(depth, alias); err != nil {
		return nil, err
	}
	if active[n] {
		return nil, fmt.Errorf("YAML alias cycle")
	}
	active[n] = true
	defer delete(active, n)

	if alias {
		return yamlValue(n.Alias, active, budget, depth+1)
	}

	switch n.Kind {
	case yaml.ScalarNode:
		value := n.Value
		if n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
			value = strings.TrimSuffix(value, "\n")
		} else {
			value = strings.TrimSpace(value)
		}
		return value, nil
	case yaml.SequenceNode:
		values := make([]any, len(n.Content))
		allStrings := true
		for i, child := range n.Content {
			value, err := yamlValue(child, active, budget, depth+1)
			if err != nil {
				return nil, err
			}
			values[i] = value
			if _, ok := values[i].(string); !ok {
				allStrings = false
			}
		}
		if allStrings {
			out := make([]string, len(values))
			for i, v := range values {
				out[i] = v.(string)
			}
			return out, nil
		}
		return values, nil
	case yaml.MappingNode:
		out := make(map[string]any, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i]
			if key.Kind != yaml.ScalarNode {
				continue
			}
			value, err := yamlValue(n.Content[i+1], active, budget, depth+1)
			if err != nil {
				return nil, err
			}
			out[key.Value] = value
		}
		return out, nil
	default:
		return nil, nil
	}
}

// Issue is one OKF 0.2 document-conformance finding. Unknown types and keys
// are intentionally allowed; the validator only enforces required structure.
type Issue struct {
	Level   string `json:"level"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidateOKF validates one document's OKF 0.2 frontmatter. canonicalPath is
// the root-relative identity (for example "/index.md"). Cross-document link
// validation belongs to index/publication validation.
func ValidateOKF(canonicalPath, content string) []Issue {
	block, _, hasFrontmatter := frontmatterParts(content)
	if !hasFrontmatter {
		if isReserved(canonicalPath) {
			return nil
		}
		return []Issue{errorIssue("type", "ordinary documents require a nonempty type")}
	}
	fm, err := decodeYAML(block)
	if err != nil {
		return []Issue{errorIssue("frontmatter", "invalid YAML: "+err.Error())}
	}
	if isReserved(canonicalPath) {
		return validateReserved(canonicalPath, fm)
	}

	var issues []Issue
	if strings.TrimSpace(stringValue(fm["type"])) == "" {
		issues = append(issues, errorIssue("type", "ordinary documents require a nonempty type"))
	}
	if v, ok := fm["status"]; ok {
		status := stringValue(v)
		switch status {
		case "draft", "stable", "deprecated":
		default:
			issues = append(issues, errorIssue("status", "status must be draft, stable, or deprecated"))
		}
	}
	if v, ok := fm["sources"]; ok {
		issues = append(issues, validateSources(v)...)
	}
	if v, ok := fm["generated"]; ok {
		issues = append(issues, validateEvent(v, "generated", false)...)
	}
	if v, ok := fm["verified"]; ok {
		issues = append(issues, validateEvent(v, "verified", true)...)
	}
	return issues
}

func errorIssue(field, message string) Issue {
	return Issue{Level: "error", Field: field, Message: message}
}

func isReserved(canonicalPath string) bool {
	base := path.Base(strings.TrimSuffix(strings.ReplaceAll(canonicalPath, "\\", "/"), "/"))
	return base == "index.md" || base == "log.md"
}

func validateReserved(canonicalPath string, fm map[string]any) []Issue {
	base := path.Base(strings.TrimSuffix(strings.ReplaceAll(canonicalPath, "\\", "/"), "/"))
	if base == "log.md" {
		if len(fm) > 0 {
			return []Issue{errorIssue("frontmatter", "reserved log.md must not carry frontmatter")}
		}
		return nil
	}
	var issues []Issue
	for key, value := range fm {
		if key != "okf_version" {
			issues = append(issues, errorIssue(key, "reserved index.md permits only okf_version"))
			continue
		}
		if strings.TrimSpace(stringValue(value)) == "" {
			issues = append(issues, errorIssue(key, "okf_version must be nonempty"))
		}
	}
	return issues
}

func validateSources(value any) []Issue {
	items, ok := value.([]any)
	if !ok {
		return []Issue{errorIssue("sources", "sources must be a list of mappings")}
	}
	var issues []Issue
	for i, item := range items {
		field := fmt.Sprintf("sources[%d]", i)
		m, ok := item.(map[string]any)
		if !ok {
			issues = append(issues, errorIssue(field, "source must be a mapping"))
			continue
		}
		if strings.TrimSpace(stringValue(m["resource"])) == "" {
			issues = append(issues, errorIssue(field+".resource", "source resource must be nonempty"))
		}
	}
	return issues
}

func validateEvent(value any, field string, allowList bool) []Issue {
	if allowList {
		if items, ok := value.([]any); ok {
			var issues []Issue
			for i, item := range items {
				m, ok := item.(map[string]any)
				if !ok {
					issues = append(issues, errorIssue(fmt.Sprintf("%s[%d]", field, i), "verification event must be a mapping"))
					continue
				}
				issues = append(issues, validateEventMapping(m, fmt.Sprintf("%s[%d]", field, i))...)
			}
			return issues
		}
	}
	m, ok := value.(map[string]any)
	if !ok {
		return []Issue{errorIssue(field, "event must be a mapping")}
	}
	return validateEventMapping(m, field)
}

func validateEventMapping(m map[string]any, field string) []Issue {
	var issues []Issue
	if strings.TrimSpace(stringValue(m["by"])) == "" {
		issues = append(issues, errorIssue(field+".by", "event by must be nonempty"))
	}
	at := strings.TrimSpace(stringValue(m["at"]))
	if !offsetTimestamp(at) {
		issues = append(issues, errorIssue(field+".at", "event at must be an RFC3339 timestamp with an offset"))
	}
	return issues
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func offsetTimestamp(value string) bool {
	if value == "" {
		return false
	}
	if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
		return false
	}
	// RFC3339 accepts Z and numeric offsets; both carry timezone information.
	return strings.HasSuffix(value, "Z") || offsetRe.MatchString(value)
}

// Unquote trims surrounding whitespace and a matching pair of quotes. It is
// retained for callers that used the old parser helper.
func Unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		s = s[1 : len(s)-1]
	}
	return strings.TrimSpace(s)
}

func list(s string) []string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '[' && s[len(s)-1] == ']' {
		s = s[1 : len(s)-1]
	}
	var out []string
	var item strings.Builder
	var quote byte
	for i := range s {
		c := s[i]
		if quote != 0 {
			item.WriteByte(c)
			if c == quote && (i == 0 || s[i-1] != '\\') {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
			item.WriteByte(c)
		case ',':
			if value := Unquote(item.String()); value != "" {
				out = append(out, value)
			}
			item.Reset()
		default:
			item.WriteByte(c)
		}
	}
	if value := Unquote(item.String()); value != "" {
		out = append(out, value)
	}
	return out
}

// String returns a frontmatter key as a string ("" if absent or not scalar).
func String(fm map[string]any, key string) string {
	return stringValue(fm[key])
}

// Strings returns a frontmatter key as a string slice. A lone scalar is
// treated as a single-element list.
func Strings(fm map[string]any, key string) []string {
	switch x := fm[key].(type) {
	case []string:
		return x
	case string:
		if x == "" {
			return nil
		}
		return []string{x}
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

var (
	linkRe       = regexp.MustCompile(`\[([^\]]*)\]\(([^)]+)\)`)
	checkboxRe   = regexp.MustCompile(`^\s*[-*+] \[([ xX])\]\s+(.*)$`)
	headingRe    = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	inlineCodeRe = regexp.MustCompile("`[^`]*`")
	offsetRe     = regexp.MustCompile(`[+-][0-9]{2}:[0-9]{2}$`)
	// tableDelimRe matches a GFM table's delimiter row: dash cells with optional
	// alignment colons (e.g. `---|:--:|---`), with optional outer pipes.
	tableDelimRe = regexp.MustCompile(`^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)*\|?\s*$`)
)

// scanLinks returns every markdown link in body (outside fenced/inline code),
// with the optional title stripped but the anchor kept.
func scanLinks(body string) []Link {
	var out []Link
	for i, line := range codeFreeLines(body) {
		for _, m := range linkRe.FindAllStringSubmatch(line, -1) {
			target := strings.TrimSpace(m[2])
			if strings.HasPrefix(target, "<") {
				// Angle-bracketed destination, the markdown way to allow spaces:
				// take everything up to the closing '>', ignoring a title after it.
				if gt := strings.IndexByte(target, '>'); gt >= 0 {
					target = target[1:gt]
				} else {
					target = target[1:] // unterminated; drop the '<' rather than leak it
				}
			} else if sp := strings.IndexAny(target, " \t"); sp >= 0 {
				target = target[:sp] // bare destination: a space begins the title
			}
			out = append(out, Link{Text: m[1], Target: target, Line: i + 1})
		}
	}
	return out
}

// LinkSet holds a body's internal markdown links, classified by target form.
// Targets are as written (title stripped, anchor kept); resolving them to
// canonical root-absolute form is the index's job.
type LinkSet struct {
	Absolute    []Link // root-absolute, e.g. /finance/income.md
	Relative    []Link // relative or bare, e.g. ../x.md, ./x.md, sibling.md
	External    []Link // carries a URL scheme, e.g. https://, mailto:
	SelfAnchors []Link // pure #anchor (intra-document): not a cross-entry edge, but check validates it against the entry's own headings
}

// Links classifies every markdown link in body (outside code) by target form.
// Empty targets are omitted; a pure #anchor lands in SelfAnchors (an intra-document
// reference, not a cross-entry edge). Both Absolute and Relative are valid internal
// links per OKF; External is the leftover bucket, returned for completeness.
func Links(body string) LinkSet {
	var set LinkSet
	for _, l := range scanLinks(body) {
		switch t := l.Target; {
		case t == "":
			// empty target: not a link
		case strings.HasPrefix(t, "#"):
			set.SelfAnchors = append(set.SelfAnchors, l) // same-page anchor; check validates it against own headings
		case hasURLScheme(t):
			set.External = append(set.External, l)
		case strings.HasPrefix(t, "/"):
			set.Absolute = append(set.Absolute, l)
		default:
			set.Relative = append(set.Relative, l)
		}
	}
	return set
}

// hasURLScheme reports whether target begins with an RFC 3986 scheme
// (ALPHA *( ALPHA / DIGIT / "+" / "-" / "." ) ":"), e.g. http: or mailto:.
func hasURLScheme(target string) bool {
	colon := strings.IndexByte(target, ':')
	if colon <= 0 {
		return false
	}
	if slash := strings.IndexByte(target, '/'); slash >= 0 && slash < colon {
		return false // the ':' sits inside a path/anchor, not a scheme
	}
	for i := 0; i < colon; i++ {
		c := target[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case i > 0 && (c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.'):
		default:
			return false
		}
	}
	return true
}

// Checkboxes returns checkbox items, ignoring fenced code.
func Checkboxes(body string) []Checkbox {
	var checkboxes []Checkbox
	for i, line := range maskedLines(body) {
		if m := checkboxRe.FindStringSubmatch(line); m != nil {
			checkboxes = append(checkboxes, Checkbox{
				Done: m[1] == "x" || m[1] == "X",
				Text: strings.TrimSpace(m[2]),
				Line: i + 1,
			})
		}
	}
	return checkboxes
}

// Headings returns ATX headings, ignoring fenced code.
func Headings(body string) []Heading {
	var hs []Heading
	for i, line := range maskedLines(body) {
		if m := headingRe.FindStringSubmatch(line); m != nil {
			hs = append(hs, Heading{Level: len(m[1]), Text: strings.TrimSpace(m[2]), Line: i + 1})
		}
	}
	return hs
}

// maskedLines returns body split into lines with fenced code blocks blanked
// (line numbers preserved) so block-scanning parsers skip code. Inline code
// spans are left intact because task and heading text keeps them verbatim.
func maskedLines(body string) []string {
	lines := strings.Split(body, "\n")
	inFence := false
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = !inFence
			lines[i] = ""
			continue
		}
		if inFence {
			lines[i] = ""
		}
	}
	return lines
}

// WithoutCode returns body with fenced blocks and inline-code spans blanked.
// Newlines remain intact so callers can still report source line numbers.
func WithoutCode(body string) string {
	return strings.Join(codeFreeLines(body), "\n")
}

func codeFreeLines(body string) []string {
	lines := maskedLines(body)
	for i := range lines {
		lines[i] = inlineCodeRe.ReplaceAllString(lines[i], "")
	}
	return lines
}

// Tables returns the GitHub-flavored markdown tables in body, in document order,
// skipping fenced code. A table is a header row, a delimiter row (e.g. `---|:--:`),
// then the contiguous data rows. Each data row is padded or truncated to the
// header's width so the result is rectangular (GFM ignores surplus cells).
func Tables(body string) []Table {
	lines := strings.Split(body, "\n")
	var tables []Table
	inFence := false
	for i := 0; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence || !strings.Contains(lines[i], "|") {
			continue
		}
		// A header line is only a table if a delimiter row follows it.
		if i+1 >= len(lines) || !tableDelimRe.MatchString(lines[i+1]) {
			continue
		}
		header := splitRow(lines[i])
		var rows [][]string
		j := i + 2
		for ; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "" || !strings.Contains(lines[j], "|") {
				break
			}
			rows = append(rows, fitRow(splitRow(lines[j]), len(header)))
		}
		tables = append(tables, Table{Header: header, Rows: rows})
		i = j - 1
	}
	return tables
}

// splitRow splits a markdown table row into trimmed cells, dropping the optional
// outer pipes. It honors `\|` as a literal pipe; a `|` inside an inline-code span
// is not special-cased (uncommon in dataset tables).
func splitRow(line string) []string {
	s := strings.TrimSpace(line)
	s = strings.TrimPrefix(s, "|")
	s = strings.TrimSuffix(s, "|")
	var cells []string
	var cur strings.Builder
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\' && i+1 < len(s) && s[i+1] == '|':
			cur.WriteByte('|')
			i++
		case s[i] == '|':
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(s[i])
		}
	}
	return append(cells, strings.TrimSpace(cur.String()))
}

// fitRow pads (with empty cells) or truncates row to exactly n columns so a
// table's rows stay rectangular against its header.
func fitRow(row []string, n int) []string {
	if len(row) < n {
		return append(row, make([]string, n-len(row))...)
	}
	return row[:n]
}
