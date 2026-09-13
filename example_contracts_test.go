package esodm_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/ctolon/esodm"
)

func ExampleReadOptions() {
	repo, recorder := exampleRepository()
	for range 2 {
		recorder.On("GET", "/people/_doc/1").Reply(200, `{"found":true,"_id":"1","_source":{"name":"Ada"}}`)
	}
	realtime := false
	for _, options := range []esodm.ReadOptions{{}, {Realtime: &realtime}} {
		if _, err := repo.GetWith(context.Background(), "1", options); err != nil {
			fmt.Println(err)
			return
		}
	}
	for _, request := range recorder.Requests() {
		parameters, err := url.ParseQuery(request.Query)
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Printf("realtime present=%t value=%q\n", parameters.Has("realtime"), parameters.Get("realtime"))
	}
	// Output:
	// realtime present=false value=""
	// realtime present=true value="false"
}

func ExampleIfMatches() {
	repo, recorder := exampleRepository()
	recorder.On("GET", "/people/_doc/1").Reply(200, `{"found":true,"_id":"1","_seq_no":0,"_primary_term":1,"_source":{"name":"Ada"}}`)
	recorder.On("PUT", "/people/_doc/1").Reply(409, `{"error":{"type":"version_conflict_engine_exception","reason":"another writer changed the document"}}`)
	hit, err := repo.GetByID(context.Background(), "1")
	if err != nil {
		fmt.Println(err)
		return
	}
	hit.Source.Name = "Grace"
	_, err = repo.Replace(context.Background(), hit.ID, hit.Source, esodm.IfMatches(hit.Metadata))
	if errors.Is(err, esodm.ErrConflict) {
		// Re-read and reconcile the user's change instead of replaying a stale replacement.
		fmt.Println("document changed; reconcile before retrying")
	} else if err != nil {
		fmt.Println(err)
	}
	// Output: document changed; reconcile before retrying
}

func ExampleCommittedError() {
	hookFailure := errors.New("audit sink unavailable")
	repo, recorder := exampleRepository(esodm.WithHooks(esodm.Hooks[exampleDocument]{
		AfterWrite: func(context.Context, esodm.Operation, esodm.WriteResult) error { return hookFailure },
	}))
	recorder.On("PUT", "/people/_create/1").Reply(201, `{"_id":"1","result":"created"}`)
	result, err := repo.Create(context.Background(), "1", exampleDocument{Name: "Ada"})
	var committed *esodm.CommittedError
	if errors.As(err, &committed) {
		// Repair or retry the audit action separately. The document is already stored.
		fmt.Println(result.ID, committed.Result.Result, errors.Is(err, hookFailure))
	} else if err != nil {
		fmt.Println(err)
	}
	// Output: 1 created true
}

func ExampleRepository_BulkWithOptions() {
	repo, recorder := exampleRepository()
	recorder.On("POST", "/_bulk").Reply(200, `{"items":[{"create":{"_id":"1","status":201}},{"create":{"_id":"2","status":429,"error":{"type":"es_rejected_execution_exception","reason":"busy"}}}]}`)
	// Only the rejected second item is retried.
	recorder.On("POST", "/_bulk").Reply(200, `{"items":[{"create":{"_id":"2","status":201}}]}`)
	operations := []esodm.BulkOperation[exampleDocument]{
		{Action: esodm.BulkCreate, ID: "1", Document: exampleDocument{Name: "Ada"}},
		{Action: esodm.BulkCreate, ID: "2", Document: exampleDocument{Name: "Grace"}},
	}
	result, err := repo.BulkWithOptions(context.Background(), operations, esodm.BulkOptions{
		Retry: esodm.RetryPolicy{MaxRetries: 1, InitialBackoff: time.Nanosecond, MaxBackoff: time.Nanosecond},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, item := range result.Items {
		fmt.Println(item.ID, item.Status, item.Err == nil)
	}
	fmt.Println("requests:", len(recorder.Requests()))
	// Output:
	// 1 201 true
	// 2 201 true
	// requests: 2
}

func ExampleRepository_MGet() {
	repo, recorder := exampleRepository()
	recorder.On("POST", "/_mget").Reply(200, `{"docs":[{"_id":"1","found":true,"_source":{"name":"Ada"}},{"_id":"missing","found":false}]}`)
	results, err := repo.MGet(context.Background(), []string{"1", "missing", "1"}, "")
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, result := range results {
		if result.Err != nil {
			fmt.Println(result.Err)
			continue
		}
		fmt.Println(result.Ref.ID, result.Found)
	}
	// Output:
	// 1 true
	// missing false
	// 1 true
}

func ExampleNewSchema_options() {
	// Later SchemaConfig replaces the full configuration; this does not keep DynamicFalse.
	schema, err := esodm.NewSchema[exampleDocument]("people",
		esodm.WithDynamic(esodm.DynamicFalse),
		esodm.SchemaConfig{Settings: map[string]any{"number_of_shards": 1}},
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	var mapping map[string]any
	if err := json.Unmarshal(schema.Mapping(), &mapping); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(mapping["dynamic"])
	// Output: strict
}
