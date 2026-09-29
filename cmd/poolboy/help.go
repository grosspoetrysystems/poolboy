package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	commanddata "github.com/grosspoetrysystems/poolboy/data"
)

const helpSchema = 1

var (
	helpBaseURL = "https://poolboy.sh/cli/help"
	helpClient  = &http.Client{
		Timeout: 2 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 || len(via) > 0 && req.URL.Hostname() != via[0].URL.Hostname() {
				return errors.New("help redirect refused")
			}
			return nil
		},
	}
)

type rawHelpCatalog struct {
	Schema      int               `json:"schema"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Intro       string            `json:"intro"`
	Commands    []json.RawMessage `json:"commands"`
}

type helpCommandIdentity struct {
	Name    string `json:"name"`
	Aliases string `json:"aliases"`
}

type helpSupport struct {
	Schema           int      `json:"schema"`
	Latest           string   `json:"latest"`
	MinimumSupported string   `json:"minimum_supported"`
	Versions         []string `json:"versions"`
}

func commandHelpRequested(args []string) bool {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			return true
		}
	}
	return false
}

func cmdHelp(command string) int {
	catalog, err := embeddedHelpCatalog()
	if err != nil {
		return productError(err)
	}
	canonical := ""
	if command != "" {
		canonical = canonicalHelpCommand(catalog, command)
		if canonical == "" {
			return productError(fmt.Errorf("unknown help command %q", command))
		}
	}

	remote, status, remoteErr := fetchHelp(canonical)
	if remoteErr == nil && status == http.StatusOK && validateHelpDocument(remote, canonical) == nil {
		return writeHelp(remote)
	}
	if remoteErr == nil && (status == http.StatusNotFound || status == http.StatusGone) {
		warnUnsupportedHelp()
	}

	if canonical == "" {
		return writeHelp(embeddedCatalogDocument(catalog))
	}
	for _, raw := range catalog.Commands {
		var identity helpCommandIdentity
		if json.Unmarshal(raw, &identity) == nil && identity.Name == canonical {
			return writeHelp(embeddedCommandDocument(raw))
		}
	}
	return productError(fmt.Errorf("embedded help missing command %q", canonical))
}

func embeddedHelpCatalog() (rawHelpCatalog, error) {
	var catalog rawHelpCatalog
	if err := json.Unmarshal([]byte(commanddata.Commands()), &catalog); err != nil {
		return catalog, fmt.Errorf("decode embedded help: %w", err)
	}
	if catalog.Schema != helpSchema || len(catalog.Commands) == 0 {
		return catalog, errors.New("embedded help has an unsupported schema")
	}
	return catalog, nil
}

func canonicalHelpCommand(catalog rawHelpCatalog, requested string) string {
	for _, raw := range catalog.Commands {
		var identity helpCommandIdentity
		if json.Unmarshal(raw, &identity) != nil {
			continue
		}
		if requested == identity.Name {
			return identity.Name
		}
		for alias := range strings.SplitSeq(identity.Aliases, ",") {
			alias = strings.TrimSpace(alias)
			if alias != "" && alias != "none" && requested == alias {
				return identity.Name
			}
		}
	}
	return ""
}

func embeddedCatalogDocument(catalog rawHelpCatalog) []byte {
	return mustJSON(struct {
		Schema      int               `json:"schema"`
		CLIVersion  string            `json:"cli_version"`
		Title       string            `json:"title"`
		Description string            `json:"description"`
		Intro       string            `json:"intro"`
		Commands    []json.RawMessage `json:"commands"`
	}{catalog.Schema, Version, catalog.Title, catalog.Description, catalog.Intro, catalog.Commands})
}

func embeddedCommandDocument(command json.RawMessage) []byte {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(command, &fields); err != nil {
		return nil
	}
	fields["schema"] = json.RawMessage(strconv.Itoa(helpSchema))
	version, _ := json.Marshal(Version)
	fields["cli_version"] = version
	return mustJSON(fields)
}

func mustJSON(value any) []byte {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		panic(err)
	}
	return data
}

func fetchHelp(command string) ([]byte, int, error) {
	if _, ok := semanticVersion(Version); !ok {
		return nil, 0, errors.New("development build")
	}
	path := "v" + Version + "/index.json"
	if command != "" {
		path = "v" + Version + "/commands/" + url.PathEscape(command) + ".json"
	}
	return fetchHelpURL(helpBaseURL + "/" + path)
}

func fetchHelpURL(address string) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), helpClient.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := helpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	return body, resp.StatusCode, err
}

func validateHelpDocument(data []byte, command string) error {
	var envelope struct {
		Schema     int               `json:"schema"`
		CLIVersion string            `json:"cli_version"`
		Name       string            `json:"name"`
		Usage      string            `json:"usage"`
		Commands   []json.RawMessage `json:"commands"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return err
	}
	if envelope.Schema != helpSchema || envelope.CLIVersion != Version {
		return errors.New("help document does not match this CLI")
	}
	if command == "" && len(envelope.Commands) == 0 {
		return errors.New("help index has no commands")
	}
	if command != "" && (envelope.Name != command || envelope.Usage == "") {
		return errors.New("help document names a different command or has no usage")
	}
	return nil
}

func writeHelp(data []byte) int {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return productError(fmt.Errorf("decode help: %w", err))
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return productError(fmt.Errorf("encode help: %w", err))
	}
	fmt.Println(string(encoded))
	return 0
}

func warnUnsupportedHelp() {
	current, ok := semanticVersion(Version)
	if !ok {
		return
	}
	data, status, err := fetchHelpURL(helpBaseURL + "/index.json")
	if err != nil || status != http.StatusOK {
		return
	}
	var support helpSupport
	if json.Unmarshal(data, &support) != nil || support.Schema != helpSchema {
		return
	}
	minimum, ok := semanticVersion(support.MinimumSupported)
	if ok && compareVersion(current, minimum) < 0 {
		fmt.Fprintf(os.Stderr, "poolboy: hosted help no longer supports v%s; update to at least v%s\n", Version, support.MinimumSupported)
	}
}

func semanticVersion(value string) ([3]int, bool) {
	var parsed [3]int
	value = strings.TrimPrefix(value, "v")
	core, _, _ := strings.Cut(value, "-")
	parts := strings.Split(core, ".")
	if len(parts) != len(parsed) {
		return parsed, false
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return parsed, false
		}
		parsed[i] = n
	}
	return parsed, true
}

func compareVersion(left, right [3]int) int {
	for i := range left {
		if left[i] < right[i] {
			return -1
		}
		if left[i] > right[i] {
			return 1
		}
	}
	return 0
}
