package commanddata

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCommandsReturnsEmbeddedCatalog(t *testing.T) {
	catalog := Commands()
	if strings.TrimSpace(catalog) == "" {
		t.Fatal("embedded command catalog is empty")
	}
	var entries any
	if err := json.Unmarshal([]byte(catalog), &entries); err != nil {
		t.Fatalf("embedded command catalog is not JSON: %v", err)
	}
}
