//go:build integration

package integration_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ctolon/esodm"
	"github.com/elastic/elastic-transport-go/v8/elastictransport"
	es8 "github.com/elastic/go-elasticsearch/v8"
	search8 "github.com/elastic/go-elasticsearch/v8/typedapi/core/search"
	es9 "github.com/elastic/go-elasticsearch/v9"
	search9 "github.com/elastic/go-elasticsearch/v9/typedapi/core/search"
)

func TestSecureCluster(t *testing.T) {
	url := os.Getenv("ESODM_SECURE_URL")
	if url == "" {
		t.Skip("run scripts/secure.py for TLS and restricted API-key tests")
	}
	ca, err := os.ReadFile(os.Getenv("ESODM_SECURE_CA"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var transport esodm.Transport
	if os.Getenv("ESODM_SECURE_MAJOR") == "8" {
		transport, err = es8.NewClient(es8.Config{Addresses: []string{url}, APIKey: os.Getenv("ESODM_SECURE_API_KEY"), CACert: ca, CompressRequestBody: true, DisableRetry: true})
	} else {
		transport, err = es9.New(es9.WithAddresses(url), es9.WithAPIKey(os.Getenv("ESODM_SECURE_API_KEY")), es9.WithCACert(ca), es9.WithCompression(), es9.WithTransportOptions(elastictransport.WithDisableRetry()))
	}
	if err != nil {
		t.Fatal(err)
	}
	c, err := esodm.Connect(ctx, transport, esodm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	index := fmt.Sprintf("esodm-secure-%d", time.Now().UnixNano())
	type document struct {
		Name string `json:"name"`
	}
	schema, err := esodm.NewSchema[document](index)
	if err != nil {
		t.Fatal(err)
	}
	r, err := esodm.NewRepository(c, schema)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.EnsureIndex(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if err := c.Admin().DeleteIndex(cleanup, index); err != nil {
			t.Error(err)
		}
	})
	if _, err := r.Create(ctx, "1", document{Name: "secure"}, esodm.WithRefresh(esodm.RefreshWaitFor)); err != nil {
		t.Fatal(err)
	}
	if hit, err := r.GetWith(ctx, "1", esodm.ReadOptions{}); err != nil || hit.Source.Name != "secure" {
		t.Fatal(hit, err)
	}
	if p, err := r.Find().Page(ctx, 1, 10); err != nil || p.Total != 1 {
		t.Fatal(p, err)
	}
	for _, err := range r.Find().Each(ctx, 10) {
		if err != nil {
			t.Fatal(err)
		}
		break
	}
	var builder esodm.RequestBuilder = search9.New(nil).Index(index)
	if c.Version().Major == 8 {
		builder = search8.New(nil).Index(index)
	}
	var result esodm.SearchResult[document]
	if err := c.DoTyped(ctx, builder, &result); err != nil || result.Len() != 1 {
		t.Fatal(result, err)
	}
	err = c.Do(ctx, "GET", "/esodm-forbidden/_search", nil, nil, nil)
	var server *esodm.Error
	if !errors.As(err, &server) || server.Status != 403 {
		t.Fatalf("restricted key escaped index permissions: %v", err)
	}
	if _, err := r.Delete(ctx, "1"); err != nil {
		t.Fatal(err)
	}
}
