package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	commanddata "github.com/grosspoetrysystems/poolboy/data"
)

func TestCommandRegistryMatchesCatalog(t *testing.T) {
	var catalog struct {
		Commands []helpCommandIdentity `json:"commands"`
	}
	if err := json.Unmarshal([]byte(commanddata.Commands()), &catalog); err != nil {
		t.Fatal(err)
	}

	registered := make(map[string][]string, len(commandRegistry))
	for _, command := range commandRegistry {
		if _, exists := registered[command.name]; exists {
			t.Fatalf("duplicate registered command %q", command.name)
		}
		registered[command.name] = command.aliases
	}
	if len(registered) != len(catalog.Commands) {
		t.Fatalf("registry has %d commands; catalog has %d", len(registered), len(catalog.Commands))
	}

	for _, documented := range catalog.Commands {
		aliases, exists := registered[documented.Name]
		if !exists {
			t.Errorf("documented command %q is not registered", documented.Name)
			continue
		}
		want := catalogAliases(documented.Aliases)
		if !slices.Equal(aliases, want) {
			t.Errorf("%s aliases=%v; catalog aliases=%v", documented.Name, aliases, want)
		}
		delete(registered, documented.Name)
	}
	for name := range registered {
		t.Errorf("registered command %q is undocumented", name)
	}
}

func catalogAliases(value string) []string {
	if value == "" || value == "none" {
		return nil
	}
	aliases := strings.Split(value, ",")
	for i := range aliases {
		aliases[i] = strings.TrimSpace(aliases[i])
	}
	return aliases
}
