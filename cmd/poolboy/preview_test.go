package main

import (
	"bufio"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPreviewHandlerRequiresLoopbackHostAndToken(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("private preview"), 0o644); err != nil {
		t.Fatal(err)
	}
	handler := previewHandler(dir, "secret")

	request := func(host, path string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "http://"+host+path, nil)
		req.Host = host
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		return res
	}

	if got := request("example.com", "/secret/").Code; got != http.StatusMisdirectedRequest {
		t.Fatalf("external host status = %d, want %d", got, http.StatusMisdirectedRequest)
	}
	if got := request("127.0.0.1", "/wrong/").Code; got != http.StatusNotFound {
		t.Fatalf("wrong token status = %d, want %d", got, http.StatusNotFound)
	}
	res := request("127.0.0.1:49152", "/secret/")
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "private preview") {
		t.Fatalf("preview response = %d %q", res.Code, res.Body.String())
	}
	if got := res.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
}

func TestPreviewTokenIsRandomHex(t *testing.T) {
	first, err := previewToken()
	if err != nil {
		t.Fatal(err)
	}
	second, err := previewToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 32 || len(second) != 32 {
		t.Fatalf("token lengths = %d, %d; want 32", len(first), len(second))
	}
	if first == second {
		t.Fatal("preview tokens unexpectedly repeated")
	}
	if _, err := hex.DecodeString(first); err != nil {
		t.Fatalf("token %q is not hex: %v", first, err)
	}
}

func TestCmdPreviewRejectsInvalidArgumentsAndProject(t *testing.T) {
	t.Cleanup(func() { rootDir = "" })
	rootDir = ""
	t.Chdir(t.TempDir())

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"positional", []string{"extra"}},
		{"negative port", []string{"--port", "-1"}},
		{"large port", []string{"--port", "65536"}},
		{"unknown flag", []string{"--unknown"}},
	} {
		if _, code := capture(t, func() int { return cmdPreview(tc.args) }); code != 2 {
			t.Fatalf("%s exit=%d, want 2", tc.name, code)
		}
	}

	if _, code := capture(t, func() int { return cmdPreview(nil) }); code != 2 {
		t.Fatal("preview outside a project did not return discovery error")
	}
}

func TestCmdPreviewBuildsAndServesPublication(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Interrupt cannot reliably stop the preview on Windows")
	}

	t.Cleanup(func() { rootDir = "" })
	rootDir = ""
	root := t.TempDir()
	write := func(name, contents string, mode os.FileMode) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), mode); err != nil {
			t.Fatal(err)
		}
	}
	write("poolboy.toml", `spec = "0.2"
corpus = "docs"
output = "dist"

[[render]]
template = "template.txt"
data = "data.json"
output = "generated.md"
`, 0o644)
	write("docs/index.md", "---\n"+
		"okf_version: \"0.2\"\n"+
		"---\n"+
		"# Preview Home\n", 0o644)
	write("template.txt", "ignored by the fixture renderer\n", 0o644)
	write("data.json", "{\"title\":\"Preview\"}\n", 0o644)
	write("renderer.sh", `#!/bin/sh
cat >/dev/null
printf '%s\n' '{"ok":true,"output":"---\nokf_version: \"0.2\"\ntype: note\n---\n# Generated\n","warnings":[]}'
`, 0o700)
	rendererPath := filepath.Join(root, "renderer.sh")
	t.Chdir(root)

	oldStdout := os.Stdout
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = stdoutW
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	t.Cleanup(func() { signal.Stop(interrupts) })
	done := make(chan int, 1)
	go func() {
		done <- cmdPreview([]string{"--renderer", rendererPath, "--port", "0"})
	}()
	finished := false
	urlPrinted := false
	t.Cleanup(func() {
		if !finished {
			select {
			case <-done:
				finished = true
			default:
				if urlPrinted {
					if process, err := os.FindProcess(os.Getpid()); err == nil {
						_ = process.Signal(os.Interrupt)
					}
					select {
					case <-done:
						finished = true
					case <-time.After(3 * time.Second):
					}
				}
			}
		}
		os.Stdout = oldStdout
		_ = stdoutW.Close()
		_ = stdoutR.Close()
	})

	lines := make(chan string, 16)
	go func() {
		scanner := bufio.NewScanner(stdoutR)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	var output strings.Builder
	var previewURL string
	for previewURL == "" {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("preview output closed before printing its URL")
			}
			output.WriteString(line)
			output.WriteByte('\n')
			if strings.HasPrefix(line, "Serving: ") {
				previewURL = strings.TrimSpace(strings.TrimPrefix(line, "Serving: "))
				urlPrinted = true
			}
		case code := <-done:
			finished = true
			t.Fatalf("preview exited before printing its URL (exit=%d; output=%q)", code, output.String())
		}
	}

	parsed, err := url.Parse(previewURL)
	if err != nil {
		t.Fatalf("preview URL = %q: %v", previewURL, err)
	}
	token := strings.Trim(parsed.Path, "/")
	if parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" ||
		parsed.Port() == "" || len(token) != 32 || strings.Contains(token, "/") {
		t.Fatalf("preview URL = %q; want loopback URL with a token", previewURL)
	}
	if _, err := hex.DecodeString(token); err != nil {
		t.Fatalf("preview token %q is not hex: %v", token, err)
	}

	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get(previewURL + "index.md")
	if err != nil {
		t.Fatalf("requesting preview publication: %v", err)
	}
	body, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), "# Preview Home") {
		t.Fatalf("preview publication response = %d %q", response.StatusCode, body)
	}
	for header, want := range map[string]string{
		"Cache-Control":          "no-store",
		"Referrer-Policy":        "no-referrer",
		"X-Content-Type-Options": "nosniff",
	} {
		if got := response.Header.Get(header); got != want {
			t.Fatalf("%s = %q, want %q", header, got, want)
		}
	}

	wrongToken := *parsed
	wrongToken.Path = "/" + token + "x/index.md"
	wrongToken.RawPath = ""
	response, err = client.Get(wrongToken.String())
	if err != nil {
		t.Fatalf("requesting preview with an invalid token: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("invalid-token response = %d, want %d", response.StatusCode, http.StatusNotFound)
	}

	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Signal(os.Interrupt); err != nil {
		t.Fatalf("interrupting preview: %v", err)
	}
	select {
	case code := <-done:
		finished = true
		if code != 0 {
			t.Fatalf("preview exit = %d; output=%s", code, output.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("preview did not stop after interrupt")
	}
	if err := stdoutW.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = oldStdout
	for line := range lines {
		output.WriteString(line)
		output.WriteByte('\n')
	}
	if !strings.Contains(output.String(), "Poolboy preview") {
		t.Fatalf("preview output missing heading: %q", output.String())
	}
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = occupied.Close() })
	occupiedPort := occupied.Addr().(*net.TCPAddr).Port
	stderr, restoreStderr := captureStderr(t)
	_, occupiedCode := capture(t, func() int {
		return cmdPreview([]string{"--renderer", rendererPath, "--port", strconv.Itoa(occupiedPort)})
	})
	restoreStderr()
	if occupiedCode != 2 {
		t.Fatalf("occupied-port preview exit = %d, want 2", occupiedCode)
	}
	if diagnostic := <-stderr; !strings.Contains(diagnostic, "start preview") {
		t.Fatalf("occupied-port diagnostic = %q", diagnostic)
	}
}
