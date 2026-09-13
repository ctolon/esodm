// Package testingexample_test demonstrates testing application behavior without a cluster.
package testingexample_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ctolon/esodm"
	"github.com/ctolon/esodm/esodmtest"
	"github.com/ctolon/esodm/examples/guide"
)

func TestRepositoryRead(t *testing.T) {
	client, recorder := esodmtest.NewClient(t, esodm.Version{Major: 9, Minor: 5, Patch: 2})
	type product struct {
		Name string `json:"name"`
	}
	schema, err := esodm.NewSchema[product]("products")
	if err != nil {
		t.Fatal(err)
	}
	repo, err := esodm.NewRepository(client, schema)
	if err != nil {
		t.Fatal(err)
	}
	recorder.On("GET", "/products/_doc/1").Reply(200,
		`{"found":true,"_id":"1","_index":"products","_source":{"name":"Go"}}`)
	hit, err := repo.GetByID(context.Background(), "1")
	if err != nil {
		t.Fatal(err)
	}
	if hit.Source.Name != "Go" || hit.ID != "1" {
		t.Fatalf("unexpected product: %+v", hit)
	}
	recorder.AssertAll(t)
}

func TestApplicationHooks(t *testing.T) {
	client, recorder := esodmtest.NewClient(t, esodm.Version{Major: 9, Minor: 5, Patch: 2})
	schema, err := esodm.NewSchemaFor[guide.Product]()
	if err != nil {
		t.Fatal(err)
	}
	repo, err := esodm.NewRepository(client, schema, esodm.WithHooks(guide.ProductHooks()))
	if err != nil {
		t.Fatal(err)
	}
	// Application validation rejects the input before any HTTP request.
	if _, err := repo.Create(context.Background(), "invalid", guide.Product{Name: "  "}); !errors.Is(err, esodm.ErrValidation) {
		t.Fatalf("expected validation failure, got %v", err)
	}
	recorder.On("PUT", "/products/_create/1").Reply(201, `{"_id":"1","result":"created"}`)
	if _, err := repo.Create(context.Background(), "1", guide.Product{Name: "  Go  ", Price: 10}); err != nil {
		t.Fatal(err)
	}
	requests := recorder.Requests()
	if len(requests) != 1 {
		t.Fatalf("expected only the valid write, got %d requests", len(requests))
	}
	var stored guide.Product
	if err := json.Unmarshal(requests[0].Body, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Name != "Go" {
		t.Fatalf("normalization did not reach the request: %q", stored.Name)
	}
	recorder.AssertAll(t)
}
