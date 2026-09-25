package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
