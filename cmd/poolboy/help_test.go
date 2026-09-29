package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestHelpUsesBundledTargetedDocumentsWithoutProject(t *testing.T) {
	t.Chdir(t.TempDir())
	setHelpTestGlobals(t, "dev", "http://127.0.0.1:1")

	out, code := capture(t, func() int { return run([]string{"help"}) })
	if code != 0 {
		t.Fatalf("root help exit=%d output=%q", code, out)
	}
	var catalog struct {
		Schema     int    `json:"schema"`
		CLIVersion string `json:"cli_version"`
		Commands   []any  `json:"commands"`
	}
	if err := json.Unmarshal([]byte(out), &catalog); err != nil {
		t.Fatalf("root help is not JSON: %v\n%s", err, out)
	}
	if catalog.Schema != 1 || catalog.CLIVersion != "dev" || len(catalog.Commands) == 0 {
		t.Fatalf("root help=%+v", catalog)
	}

	out, code = capture(t, func() int { return run([]string{"mv", "--help"}) })
	if code != 0 {
		t.Fatalf("move help exit=%d output=%q", code, out)
	}
	var command struct {
		Schema     int    `json:"schema"`
		CLIVersion string `json:"cli_version"`
		Name       string `json:"name"`
		Usage      string `json:"usage"`
	}
	if err := json.Unmarshal([]byte(out), &command); err != nil {
		t.Fatalf("move help is not JSON: %v\n%s", err, out)
	}
	if command.Schema != 1 || command.CLIVersion != "dev" || command.Name != "move" || command.Usage == "" {
		t.Fatalf("move help=%+v", command)
	}
	if strings.Contains(out, `"name": "check"`) {
		t.Fatalf("targeted help leaked another command: %s", out)
	}

	out, code = capture(t, func() int { return run([]string{"help", "move"}) })
	if code != 0 || !strings.Contains(out, `"name": "move"`) {
		t.Fatalf("help subcommand target exit=%d output=%q", code, out)
	}
	if _, code = capture(t, func() int { return run([]string{"none", "--help"}) }); code != 2 {
		t.Fatalf("sentinel alias exit=%d want 2", code)
	}
}

func TestHelpUsesMatchingHostedCommandDocument(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1.2.3/commands/move.json" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"schema":1,"cli_version":"1.2.3","name":"move","usage":"hosted move","summary":"corrected online"}`)
	}))
	t.Cleanup(server.Close)
	setHelpTestGlobals(t, "1.2.3", server.URL)

	out, code := capture(t, func() int { return run([]string{"move", "-h"}) })
	if code != 0 || !strings.Contains(out, `"summary": "corrected online"`) {
		t.Fatalf("hosted help exit=%d output=%q", code, out)
	}
}

func TestHelpWarnsWhenHostedDocsPrunedAndUsesBundle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0.4.0/commands/move.json":
			http.NotFound(w, r)
		case "/index.json":
			_, _ = io.WriteString(w, `{"schema":1,"latest":"0.9.0","minimum_supported":"0.7.0","versions":["0.7.0","0.9.0"]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	setHelpTestGlobals(t, "0.4.0", server.URL)

	stderr, restore := captureStderr(t)
	out, code := capture(t, func() int { return run([]string{"move", "--help"}) })
	restore()
	if code != 0 || !strings.Contains(out, `"name": "move"`) {
		t.Fatalf("fallback help exit=%d output=%q", code, out)
	}
	if got := <-stderr; !strings.Contains(got, "update to at least v0.7.0") {
		t.Fatalf("unsupported warning=%q", got)
	}
}

func setHelpTestGlobals(t *testing.T, version, baseURL string) {
	t.Helper()
	oldVersion, oldURL := Version, helpBaseURL
	Version, helpBaseURL = version, baseURL
	t.Cleanup(func() {
		Version, helpBaseURL = oldVersion, oldURL
	})
}

func captureStderr(t *testing.T) (<-chan string, func()) {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	result := make(chan string, 1)
	return result, func() {
		_ = w.Close()
		os.Stderr = old
		data, readErr := io.ReadAll(r)
		_ = r.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		result <- string(data)
	}
}
