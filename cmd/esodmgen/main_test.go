package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratedCodeCompilesAndIsStable(t *testing.T) {
	dir := t.TempDir()
	model := "package model\nimport (\"time\"; \"github.com/ctolon/esodm\")\ntype Base struct { Tenant string; Name string `json:\"name\"` }\ntype Detail struct { Text string `json:\"text\" es:\"type=text\"`; Price int `json:\"price\"` }\ntype Product struct { Data *[]byte; *Base; *esodm.Timestamps; Details []Detail `json:\"details\" es:\"type=nested\"`; Name string `json:\"name\" es:\"type=text\"`; Price int `json:\"price\"`; Enabled bool; At time.Time; Parent *esodm.Ref[Product]; Children []esodm.Ref[Product]; Secret string `json:\"-\"` }\n"
	if err := os.WriteFile(filepath.Join(dir, "model.go"), []byte(model), 0644); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	mod := "module generated.test/model\n\ngo 1.26.0\n\nrequire github.com/ctolon/esodm v0.0.0\nreplace github.com/ctolon/esodm => " + root + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0644); err != nil {
		t.Fatal(err)
	}
	if err := run(dir, "Product", "fields.go"); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(filepath.Join(dir, "fields.go"))
	if err != nil {
		t.Fatal(err)
	}
	if err := run(dir, "Product", "fields.go"); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(filepath.Join(dir, "fields.go"))
	if string(first) != string(second) || !strings.Contains(string(first), "RelationField[Product]") || strings.Contains(string(first), "Secret") || !strings.Contains(string(first), "esodm.DateField") || !strings.Contains(string(first), `esodm.Text("details.text")`) || !strings.Contains(string(first), `esodm.Date("created_at")`) {
		t.Fatal(string(first))
	}
	// Compile a caller assignment so a byte slice cannot regress to OrderedField[byte].
	usage := "package model\nvar _ = ProductFields.Data.Set([]byte{1, 2})\n"
	if err := os.WriteFile(filepath.Join(dir, "usage_test.go"), []byte(usage), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-mod=mod", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
func TestGeneratorRejectsInvalidInput(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ types, output string }{{"", "fields.go"}, {"X", "../outside.go"}, {"X", "fields.txt"}, {"Missing", "fields.go"}} {
		if err := run(dir, tc.types, tc.output); err == nil {
			t.Fatal(tc)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "model.go"), []byte("package model\ntype X struct { Value string }"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := run(dir, "X,X", "fields.go"); err == nil {
		t.Fatal("duplicate")
	}
	if err := os.WriteFile(filepath.Join(dir, "fields.go"), []byte("package model\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := run(dir, "X", "fields.go"); err == nil {
		t.Fatal("overwrote user file")
	}
}
