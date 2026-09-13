package esodm

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func BenchmarkGetNoJoin(b *testing.B) {
	s, err := NewSchema[testDoc]("bench")
	if err != nil {
		b.Fatal(err)
	}
	r, err := NewRepository(testClient(func(*http.Request) (*http.Response, error) {
		return response(200, `{"found":true,"_source":{"name":"Go"}}`), nil
	}), s)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := r.Get(context.Background(), "1", ""); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBulk(b *testing.B) {
	s, err := NewSchema[testDoc]("bench")
	if err != nil {
		b.Fatal(err)
	}
	reply := `{"items":[` + strings.TrimSuffix(strings.Repeat(`{"index":{"status":201}},`, 100), ",") + `]}`
	r, err := NewRepository(testClient(func(*http.Request) (*http.Response, error) { return response(200, reply), nil }), s)
	if err != nil {
		b.Fatal(err)
	}
	ops := make([]BulkOperation[testDoc], 100)
	for i := range ops {
		ops[i] = BulkOperation[testDoc]{Action: BulkIndex, ID: "1", Document: testDoc{Name: "Go"}}
	}
	b.Run("Batch", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := r.Bulk(context.Background(), ops, ""); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("IndexerItem", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := r.PrepareIndexerOperation(context.Background(), ops[0]); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkQuery(b *testing.B) {
	field := OrderedValue[int]("age")
	b.ReportAllocs()
	for b.Loop() {
		_, err := json.Marshal(NewSearch(Filter(field.GTE(18), Text("name").Match("go"))).Size(20))
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCodec(b *testing.B) {
	raw := []byte(`{"name":"Ada","age":30,"active":true}`)
	doc := testDoc{Name: "Ada", Age: 30, Active: true}
	b.Run("Decode", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var d testDoc
			if err := json.Unmarshal(raw, &d); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("Encode", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := json.Marshal(doc); err != nil {
				b.Fatal(err)
			}
		}
	})
}
