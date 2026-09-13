package esodm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

func TestRegistryRejectsNilAtomically(t *testing.T) {
	repo := testRepo(t, nil)
	for _, entry := range []Ensurer{nil, (*Repository[testDoc])(nil)} {
		var registry Registry
		if err := registry.Add(repo, entry); !errors.Is(err, ErrValidation) {
			t.Fatalf("Add returned %v; want ErrValidation", err)
		}
		if got := registry.Indexes(); len(got) != 0 {
			t.Fatalf("failed registration changed registry: %v", got)
		}
		if err := registry.Add(repo); err != nil {
			t.Fatalf("registry unusable after failed registration: %v", err)
		}
	}
}

func TestBulkSeqRejectsNilWithoutCallbacks(t *testing.T) {
	repo := testRepo(t, nil)
	for _, workers := range []int{0, 2} {
		err := repo.BulkSeq(context.Background(), nil, BulkStreamOptions{Workers: workers}, func(BulkBatchResult) error {
			t.Error("callback invoked for invalid producer")
			return nil
		})
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("workers=%d: got %v; want ErrValidation", workers, err)
		}
	}
}

// HookEmbedded intentionally implements neither Routed nor Stamper.
type HookEmbedded struct {
	Name string `json:"name"`
}
type hookDocument struct{ *HookEmbedded }

func TestWriteHooksOwnAnonymousPointers(t *testing.T) {
	for _, operation := range []string{"replace", "bulk", "upsert"} {
		t.Run(operation, func(t *testing.T) {
			schema, err := NewSchema[hookDocument]("hooks")
			if err != nil {
				t.Fatal(err)
			}
			client := testClient(func(*http.Request) (*http.Response, error) { return response(200, `{ "result": "updated" }`), nil })
			repo, err := NewRepository(client, schema, Hooks[hookDocument]{Validate: func(_ context.Context, doc *hookDocument) error {
				doc.Name = "prepared"
				return nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			for _, embedded := range []*HookEmbedded{nil, {Name: "original"}} {
				doc := hookDocument{embedded}
				switch operation {
				case "replace":
					_, err = repo.Replace(context.Background(), "1", doc)
				case "bulk":
					_, _, err = repo.EncodeOperation(context.Background(), BulkOperation[hookDocument]{Action: BulkIndex, ID: "1", Document: doc})
				case "upsert":
					_, err = repo.Upsert(context.Background(), "1", Patch{"name": "patch"}, doc)
				}
				if err != nil {
					t.Fatal(err)
				}
				if doc.HookEmbedded != embedded || embedded != nil && embedded.Name != "original" {
					t.Fatalf("caller model mutated: %+v", doc)
				}
			}
		})
	}
}

func TestPreparedIndexerOwnsConcurrencyTokens(t *testing.T) {
	repo := testRepo(t, nil)
	seq, term := int64(0), int64(1)
	item, err := repo.PrepareIndexerOperation(context.Background(), BulkOperation[testDoc]{
		Action: BulkIndex, ID: "1", Options: WriteOptions{IfSeqNo: &seq, IfPrimaryTerm: &term, Routing: "route"},
	})
	if err != nil {
		t.Fatal(err)
	}
	seq, term = 99, 99
	if item.IfSeqNo == nil || *item.IfSeqNo != 0 || item.IfPrimaryTerm == nil || *item.IfPrimaryTerm != 1 {
		t.Fatal("prepared tokens changed with caller values")
	}
	result := item.Complete(context.Background(), WriteResult{}, 201, nil)
	if result.Action != BulkIndex || result.Routing != "route" || result.Err != nil {
		t.Fatalf("incorrect completion: %+v", result)
	}
}

func TestBulkPreservesKnownOutcomesBeforeMalformedItem(t *testing.T) {
	calls, completed := 0, 0
	repo := testRepo(t, func(*http.Request) (*http.Response, error) {
		calls++
		return response(200, `{"items":[{"index":{"_id":"1","status":201,"result":"created"}},{"delete":{"_id":"2","status":200}}]}`), nil
	})
	repo.hooks.AfterWrite = func(context.Context, Operation, WriteResult) error { completed++; return nil }
	result, err := repo.BulkWithOptions(context.Background(), []BulkOperation[testDoc]{
		{Action: BulkIndex, ID: "1"}, {Action: BulkIndex, ID: "2"},
	}, BulkOptions{Retry: RetryPolicy{MaxRetries: 1}})
	if err == nil {
		t.Fatal("malformed item accepted")
	}
	if calls != 1 || completed != 1 {
		t.Fatalf("requests=%d hooks=%d", calls, completed)
	}
	if len(result.Items) != 2 || result.Items[0].Status != 201 || result.Items[0].Err != nil || result.Items[1].Err == nil {
		t.Fatalf("known success lost or unknown outcome reported as success: %+v", result.Items)
	}
}

func TestMigrationRejectsNegativeIntervalBeforeSideEffects(t *testing.T) {
	admin := testClient(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid migration options reached transport")
		return nil, nil
	}).Admin()
	options := MigrationOptions{WritesPaused: true, PollInterval: -1}
	if _, err := admin.StartMigration(context.Background(), &MigrationPlan{}, options); !errors.Is(err, ErrValidation) {
		t.Fatalf("StartMigration: %v", err)
	}
	if _, err := admin.ResumeMigration(context.Background(), MigrationState{}, options); !errors.Is(err, ErrValidation) {
		t.Fatalf("ResumeMigration: %v", err)
	}
	if _, err := admin.ApplyMigration(context.Background(), &MigrationPlan{}, options); !errors.Is(err, ErrValidation) {
		t.Fatalf("ApplyMigration: %v", err)
	}
}

func TestBinaryInferencePreservesContainersAndOptions(t *testing.T) {
	type document struct {
		Plain    []byte   `json:"plain" es:"doc_values=true"`
		Optional *[]byte  `json:"optional"`
		Multiple [][]byte `json:"multiple"`
		Numeric  [2]byte  `json:"numeric"`
		Explicit []byte   `json:"explicit" es:"type=keyword,index=false"`
	}
	schema, err := NewSchema[document]("binary")
	if err != nil {
		t.Fatal(err)
	}
	var mapping struct {
		Properties map[string]FieldMapping `json:"properties"`
	}
	if err := json.Unmarshal(schema.Mapping(), &mapping); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"plain": "binary", "optional": "binary", "multiple": "binary", "numeric": "unsigned_long", "explicit": "keyword"} {
		if got := mapping.Properties[name].Type; got != want {
			t.Errorf("%s: got %s, want %s", name, got, want)
		}
	}
	if flag := mapping.Properties["plain"].DocValues; flag == nil || !*flag {
		t.Fatal("binary inference discarded explicit doc_values")
	}
	if flag := mapping.Properties["explicit"].Index; flag == nil || *flag {
		t.Fatal("explicit mapping options discarded")
	}
}
