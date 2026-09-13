package esodm_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ctolon/esodm"
	"github.com/ctolon/esodm/adapter/es8"
	"github.com/ctolon/esodm/adapter/es9"
	"github.com/elastic/elastic-transport-go/v8/elastictransport"
	elastic8 "github.com/elastic/go-elasticsearch/v8"
	util8 "github.com/elastic/go-elasticsearch/v8/esutil"
	elastic9 "github.com/elastic/go-elasticsearch/v9"
	util9 "github.com/elastic/go-elasticsearch/v9/esutil"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestOfficialAdapterContract(t *testing.T) {
	for _, major := range []int{8, 9} {
		t.Run(string(rune('0'+major)), func(t *testing.T) {
			calls := 0
			version := "8.18.1"
			if major == 9 {
				version = "9.4.5"
			}
			rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				status := 200
				body := `{"version":{"number":"` + version + `"}}`
				if req.URL.Path != "/" {
					status = 503
					body = `{"error":{"type":"unavailable","reason":"test"}}`
				}
				return &http.Response{StatusCode: status, Header: http.Header{"X-Elastic-Product": {"Elasticsearch"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			var c *esodm.Client
			var err error
			if major == 8 {
				raw, e := elastic8.NewClient(elastic8.Config{Transport: rt, DisableRetry: true})
				if e != nil {
					t.Fatal(e)
				}
				c, err = es8.Connect(context.Background(), raw, esodm.Config{})
			} else {
				raw, e := elastic9.New(elastic9.WithTransportOptions(elastictransport.WithTransport(rt), elastictransport.WithDisableRetry()))
				if e != nil {
					t.Fatal(e)
				}
				c, err = es9.Connect(context.Background(), raw, esodm.Config{})
			}
			if err != nil || c.Version().Major != major {
				t.Fatal(c, err)
			}
			before := calls
			err = c.Do(context.Background(), "POST", "/test", nil, map[string]bool{"write": true}, nil)
			var e *esodm.Error
			if !errors.As(err, &e) || e.Status != 503 || calls-before != 1 {
				t.Fatal("unexpected retry", err, calls-before)
			}
		})
	}
	if _, err := es8.Connect(context.Background(), nil, esodm.Config{}); !errors.Is(err, esodm.ErrValidation) {
		t.Fatal(err)
	}
	if _, err := es9.Connect(context.Background(), nil, esodm.Config{}); !errors.Is(err, esodm.ErrValidation) {
		t.Fatal(err)
	}
}

func TestIndexerAdapterCallbacks(t *testing.T) {
	ctx := context.Background()
	c, err := esodm.Connect(ctx, esodm.TransportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"version":{"number":"9.5.2"}}`))}, nil
	}), esodm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	type doc struct{ Name string }
	schema, _ := esodm.NewSchema[doc]("items")
	r, _ := esodm.NewRepository(c, schema, esodm.Hooks[doc]{AfterWrite: func(context.Context, esodm.Operation, esodm.WriteResult) error { return io.EOF }})
	var observed esodm.BulkItem
	callback := func(_ context.Context, item esodm.BulkItem) { observed = item }
	op := esodm.BulkOperation[doc]{Action: esodm.BulkCreate, ID: "x", Document: doc{Name: "Ada"}}
	eight, err := es8.BulkIndexerItem(ctx, r, op, callback)
	if err != nil {
		t.Fatal(err)
	}
	eight.OnSuccess(ctx, eight, util8.BulkIndexerResponseItem{DocumentID: "x", Status: 201, PrimTerm: 1})
	var committed *esodm.CommittedError
	if !errors.As(observed.Err, &committed) {
		t.Fatal(observed)
	}
	eight.OnFailure(ctx, eight, util8.BulkIndexerResponseItem{Status: 429}, nil)
	if !errors.Is(observed.Err, esodm.ErrTooManyRequests) {
		t.Fatal(observed)
	}
	nine, err := es9.BulkIndexerItem(ctx, r, op, callback)
	if err != nil {
		t.Fatal(err)
	}
	nine.OnSuccess(ctx, nine, util9.BulkIndexerResponseItem{DocumentID: "x", Status: 201, PrimTerm: 1})
	if !errors.As(observed.Err, &committed) {
		t.Fatal(observed)
	}
	nine.OnFailure(ctx, nine, util9.BulkIndexerResponseItem{}, io.ErrUnexpectedEOF)
	if !errors.Is(observed.Err, io.ErrUnexpectedEOF) {
		t.Fatal(observed)
	}
	for _, bad := range []esodm.BulkOperation[doc]{{Action: esodm.BulkIndex, ID: "x", Options: esodm.WriteOptions{Pipeline: "p"}}, {Action: "bad", ID: "x"}} {
		if _, err = es8.BulkIndexerItem(ctx, r, bad, callback); err == nil {
			t.Fatal(bad)
		}
		if _, err = es9.BulkIndexerItem(ctx, r, bad, callback); err == nil {
			t.Fatal(bad)
		}
	}
	if _, err = es8.BulkIndexerItem(ctx, r, op, nil); !errors.Is(err, esodm.ErrValidation) {
		t.Fatal(err)
	}
	if _, err = es9.BulkIndexerItem[doc](ctx, nil, op, callback); !errors.Is(err, esodm.ErrValidation) {
		t.Fatal(err)
	}
}
