package es9

import (
	"context"
	"errors"
	"github.com/ctolon/esodm"
	"github.com/ctolon/esodm/esodmtest"
	"github.com/elastic/go-elasticsearch/v9/esutil"
	"io"
	"testing"
)

func TestBulkIndexerItem(t *testing.T) {
	ctx := context.Background()
	c, _ := esodmtest.NewClient(t, esodm.Version{Major: 9, Minor: 19, Patch: 7})
	type document struct {
		Name string `json:"name"`
	}
	schema, err := esodm.NewSchema[document]("items")
	if err != nil {
		t.Fatal(err)
	}
	repo, err := esodm.NewRepository(c, schema)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		status    int
		term      int64
		failure   bool
		transport error
	}{
		{"success", 201, 1, false, nil}, {"no token", 201, 0, false, nil}, {"item failure", 429, 0, true, nil}, {"add failure", 0, 0, true, io.ErrUnexpectedEOF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			var got esodm.BulkItem
			item, err := BulkIndexerItem(ctx, repo, esodm.BulkOperation[document]{Action: esodm.BulkCreate, ID: "1", Document: document{Name: "Ada"}}, func(_ context.Context, result esodm.BulkItem) { calls++; got = result })
			if err != nil {
				t.Fatal(err)
			}
			if item.DocumentID != "1" || item.Body == nil {
				t.Fatal(item)
			}
			result := esutil.BulkIndexerResponseItem{DocumentID: "1", Index: "items", Status: tc.status, PrimTerm: tc.term, SeqNo: 0}
			if tc.failure {
				result.Error.Type = "rejected_execution_exception"
				result.Error.Reason = "busy"
				result.Error.Cause.Type = "root"
				result.Error.Cause.Reason = "nested"
				item.OnFailure(ctx, item, result, tc.transport)
			} else {
				item.OnSuccess(ctx, item, result)
			}
			if calls != 1 {
				t.Fatal(calls)
			}
			if (got.PrimaryTerm != nil) != (tc.term > 0) || (got.SeqNo != nil) != (tc.term > 0) {
				t.Fatal(got)
			}
			if tc.transport != nil && !errors.Is(got.Err, tc.transport) {
				t.Fatal(got.Err)
			}
			if tc.failure && tc.transport == nil {
				var server *esodm.Error
				if !errors.As(got.Err, &server) || server.Cause.CausedBy == nil {
					t.Fatal(got.Err)
				}
			}
		})
	}
	if _, err := BulkIndexerItem[document](ctx, nil, esodm.BulkOperation[document]{}, func(context.Context, esodm.BulkItem) {}); err == nil {
		t.Fatal("nil repo")
	}
	if _, err := BulkIndexerItem(ctx, repo, esodm.BulkOperation[document]{}, nil); err == nil {
		t.Fatal("nil callback")
	}
	if _, err := BulkIndexerItem(ctx, repo, esodm.BulkOperation[document]{Action: "invalid"}, func(context.Context, esodm.BulkItem) {}); err == nil {
		t.Fatal("invalid operation")
	}
}
