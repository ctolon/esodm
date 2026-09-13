package main

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestPublicFieldContracts(t *testing.T) {
	for _, tt := range []struct{ name, source, want string }{
		{"missing", "type Options struct { Limit int }", "Options.Limit"},
		{"nested", "type Result struct {\n // Shards is the response.\n Shards struct { Failed int }\n}", "Result.Shards.Failed"},
		{"documented", "type Options struct {\n // Limit is positive; zero selects ten.\n Limit int\n}", ""},
		{"private", "type private struct { Limit int }; type Public struct { hidden int }", ""},
		{"embedded", "type Public struct { Other }", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", "package fixture\n"+tt.source, parser.ParseComments)
			if err != nil {
				t.Fatal(err)
			}
			err = checkPublicFields(file)
			if tt.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v; want %s", err, tt.want)
			}
		})
	}
}
