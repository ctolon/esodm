//go:build integration

package integration_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/ctolon/esodm"
)

func TestBinaryContainers(t *testing.T) {
	type document struct {
		Plain    []byte   `json:"plain" es:"doc_values=true"`
		Optional *[]byte  `json:"optional,omitempty"`
		Multiple [][]byte `json:"multiple"`
		Numeric  [2]byte  `json:"numeric"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := client(t)
	index := name()
	schema, err := esodm.NewSchema[document](index)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := esodm.NewRepository(c, schema)
	if err != nil {
		t.Fatal(err)
	}
	cleanupIndex(t, c, index)
	if err := repo.EnsureIndex(ctx); err != nil {
		t.Fatal(err)
	}
	bytes := []byte{0, 1, 255}
	want := document{Plain: bytes, Optional: &bytes, Multiple: [][]byte{{2, 3}, {4, 5}}, Numeric: [2]byte{6, 7}}
	if _, err := repo.Create(ctx, "1", want); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetByID(ctx, "1")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Source, want) {
		t.Fatalf("binary round trip: got %#v; want %#v", got.Source, want)
	}
}
