package output

import (
	"bytes"
	"io"
	"reflect"
	"strings"
	"testing"
)

type coverageRow struct {
	Tagged   string `json:"tagged,omitempty"`
	Untagged int
	Ignored  string `json:"-"`
	private  bool
	Float    float64 `json:"float"`
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, io.ErrClosedPipe
}

func TestDelimitedHandlesNilPointersAndFallbackColumns(t *testing.T) {
	rows := []*coverageRow{nil, {Tagged: "value", Untagged: 4, Ignored: "hidden", private: true, Float: 1.5}}
	var b bytes.Buffer
	if err := Emit(&b, "csv", nil, rows); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	if !strings.Contains(got, "tagged,Untagged,float\n") || !strings.Contains(got, "value,4,1.5\n") {
		t.Fatalf("nil-pointer CSV = %q", got)
	}
	if strings.Contains(got, "Ignored") || strings.Contains(got, "private") {
		t.Fatalf("unexported/ignored columns leaked: %q", got)
	}

	cols := structColumns(reflect.TypeOf(coverageRow{}))
	if len(cols) != 3 || cols[0].name != "tagged" || cols[1].name != "Untagged" || cols[2].name != "float" {
		t.Fatalf("columns = %#v", cols)
	}
}

func TestTableHandlesRaggedRowsAndCellFallback(t *testing.T) {
	header := []string{"first", "second", "third"}
	rows := [][]string{{"one"}, {"two", "2", "3"}}
	var jsonb bytes.Buffer
	if err := Table(&jsonb, "json", header, rows); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(jsonb.String(), `"second": ""`) || !strings.Contains(jsonb.String(), `"third": ""`) {
		t.Fatalf("ragged JSON = %q", jsonb.String())
	}
	var text bytes.Buffer
	if err := Table(&text, "text", header, rows); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text.String(), "one") || !strings.Contains(text.String(), "two") {
		t.Fatalf("ragged text = %q", text.String())
	}
	if got := cell(reflect.ValueOf(float64(1.25))); got != "1.25" {
		t.Fatalf("float cell = %q", got)
	}
	if got := cell(reflect.ValueOf([]int{1, 2})); got != "1; 2" {
		t.Fatalf("integer slice cell = %q", got)
	}
}

func TestDelimitedFallbackForNilValue(t *testing.T) {
	var b bytes.Buffer
	if err := Emit(&b, "csv", []string{"one", "two"}, nil); err != nil {
		t.Fatal(err)
	}
	if got := b.String(); got != "one\ntwo\n" {
		t.Fatalf("nil CSV fallback = %q", got)
	}
}

func TestEmitAndTablePropagateWriterErrors(t *testing.T) {
	for _, format := range []string{"text", "json", "csv", "tsv"} {
		if err := Emit(failingWriter{}, format, []string{"line"}, []coverageRow{{Tagged: "value"}}); err == nil {
			t.Fatalf("Emit(%q) accepted a failing writer", format)
		}
	}
	for _, format := range []string{"text", "json", "csv", "tsv"} {
		if err := Table(failingWriter{}, format, []string{"column"}, [][]string{{"value"}}); err == nil {
			t.Fatalf("Table(%q) accepted a failing writer", format)
		}
	}
}
