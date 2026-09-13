package esodm

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"
	"testing"

	search8 "github.com/elastic/go-elasticsearch/v8/typedapi/core/search"
	search9 "github.com/elastic/go-elasticsearch/v9/typedapi/core/search"
	"github.com/elastic/go-elasticsearch/v9/typedapi/esdsl"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
)

func TestOfficialBuildersSnapshotAndPrecision(t *testing.T) {
	builder := esdsl.NewMatchQuery("name", "before")
	snapshot := FromQuery(builder)
	builder.Query("after")
	raw, err := json.Marshal(snapshot)
	if err != nil || !strings.Contains(string(raw), "before") || strings.Contains(string(raw), "after") {
		t.Fatalf("snapshot changed: %s, %v", raw, err)
	}
	q := And(Term("large", uint64(math.MaxUint64)), OrderedValue[int64]("age").GTE(math.MaxInt64), snapshot)
	raw, err = json.Marshal(q)
	if err != nil || !strings.Contains(string(raw), "18446744073709551615") || !strings.Contains(string(raw), "9223372036854775807") {
		t.Fatalf("numeric precision lost: %s, %v", raw, err)
	}
	agg := esdsl.NewTermsAggregation().Field("name").Size(10)
	saved := FromAggregation(agg)
	agg.Size(20)
	raw, err = json.Marshal(saved)
	if err != nil || !strings.Contains(string(raw), `"size":10`) {
		t.Fatalf("aggregation changed: %s %v", raw, err)
	}
	for _, invalid := range []Query{FromQuery(nil), FromQuery((*types.Query)(nil)), And(RawQuery(nil)), Nested("x", RawQuery(nil)), HasChild("x", RawQuery(nil)), HasParent("x", RawQuery(nil)), OrderedValue[float64]("x").Between(math.NaN(), 1), OrderedValue[float64]("x").Between(1, math.NaN()), OrderedValue[float64]("x").GT(math.NaN())} {
		if _, err := json.Marshal(invalid); err == nil {
			t.Fatal("invalid query accepted")
		}
	}
	for _, invalid := range []Aggregation{FromAggregation(nil), FromAggregation((*types.Aggregations)(nil)), Agg("x", math.NaN(), nil), TermsAgg("name", 2, map[string]Aggregation{"bad": {}})} {
		if _, err := json.Marshal(invalid); err == nil {
			t.Fatal("invalid aggregation accepted")
		}
	}
	for _, kind := range []string{"avg", "sum", "min", "max", "stats", "cardinality", "value_count", "extended_stats"} {
		raw, err := json.Marshal(MetricAgg(kind, "age"))
		if err != nil || !strings.Contains(string(raw), `"`+kind+`"`) {
			t.Fatalf("%s: %s %v", kind, raw, err)
		}
	}
}

func TestOfficialSearchRequestSnapshot(t *testing.T) {
	size := 3
	no := false
	req := &search9.Request{Size: &size, Query: esdsl.NewMatchAllQuery().QueryCaster(), SeqNoPrimaryTerm: &no}
	saved := SearchFromRequest(req)
	size = 99
	req.Query = esdsl.NewMatchNoneQuery().QueryCaster()
	raw, err := json.Marshal(saved)
	if err != nil || !strings.Contains(string(raw), `"size":3`) || !strings.Contains(string(raw), `"seq_no_primary_term":false`) || !strings.Contains(string(raw), `"track_total_hits":true`) || !strings.Contains(string(raw), "match_all") {
		t.Fatalf("request snapshot/defaults: %s %v", raw, err)
	}
	if _, err := json.Marshal(SearchFromRequest(nil)); err == nil {
		t.Fatal("nil request accepted")
	}
	bad := &search9.Request{Query: &types.Query{Term: map[string]types.TermQuery{"x": {Value: math.NaN()}}}}
	if _, err := json.Marshal(SearchFromRequest(bad)); err == nil {
		t.Fatal("bad query accepted")
	}
	for _, s := range []Search{
		Search{}.KNN(KNN{Field: "v", Vector: []float32{1}, K: 1, Candidates: 1, Filter: ptrQuery(RawQuery(nil))}),
		Search{}.HybridRRF(RawQuery(nil), KNN{Field: "v", Vector: []float32{1}, K: 1, Candidates: 1}, 10, 60),
	} {
		if _, err := json.Marshal(s); err == nil {
			t.Fatal("invalid vector filter accepted")
		}
	}
}

func ptrQuery(q Query) *Query { return &q }

type requestBuilderFunc func(context.Context) (*http.Request, error)

func (f requestBuilderFunc) HttpRequest(ctx context.Context) (*http.Request, error) { return f(ctx) } //nolint:staticcheck // Match the official RequestBuilder interface.

func TestDoTypedUsesODMBoundariesForBothMajors(t *testing.T) {
	for _, major := range []int{8, 9} {
		var builder RequestBuilder = search8.New(nil).Index("people")
		if major == 9 {
			builder = search9.New(nil).Index("people")
		}
		observed := 0
		closed := false
		c := testClient(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/people/_search" {
				t.Fatal(req.URL.Path)
			}
			if !strings.Contains(req.Header.Get("Accept"), "compatible-with=") {
				t.Fatal("official headers lost")
			}
			return &http.Response{StatusCode: 200, Body: closeBody{Reader: strings.NewReader(`{"value":9223372036854775807}`), closed: &closed}}, nil
		})
		c.version.Major = major
		c.config.Observer = func(_ context.Context, e Event) {
			observed++
			if e.Method != "POST" || e.Status != 200 {
				t.Fatal(e)
			}
		}
		var out map[string]any
		if err := c.DoTyped(context.Background(), builder, &out); err != nil {
			t.Fatal(err)
		}
		if out["value"] != json.Number("9223372036854775807") || !closed || observed != 1 {
			t.Fatalf("boundary failure: %v %v %d", out, closed, observed)
		}
	}
	c := testClient(func(*http.Request) (*http.Response, error) { return response(409, `{"error":"conflict"}`), nil })
	if err := c.DoTyped(context.Background(), search9.New(nil), nil); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	c.config.MaxResponseBytes = 1
	if err := c.DoTyped(context.Background(), search9.New(nil), nil); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatal(err)
	}
	for _, builder := range []RequestBuilder{nil, (*search9.Search)(nil), requestBuilderFunc(func(context.Context) (*http.Request, error) { return nil, errors.New("build failed") }), requestBuilderFunc(func(context.Context) (*http.Request, error) {
		return http.NewRequest("GET", "https://unrelated.invalid/", nil)
	}), requestBuilderFunc(func(context.Context) (*http.Request, error) { return nil, nil })} {
		if err := c.DoTyped(context.Background(), builder, nil); err == nil {
			t.Fatal("invalid builder accepted")
		}
	}
	//lint:ignore SA1012 Exercise the explicit nil-context validation boundary.
	if err := c.DoTyped(nil, search9.New(nil), nil); !errors.Is(err, ErrValidation) { //nolint:staticcheck // Verify explicit rejection of a nil context.
		t.Fatal(err)
	}
}

func TestSchemaFromOfficialMapping(t *testing.T) {
	mapping := esdsl.NewTypeMapping().Properties(map[string]types.Property{"vector": esdsl.NewDenseVectorProperty().Dims(3).DenseVectorPropertyCaster()}).TypeMappingCaster()
	schema, err := SchemaFromMapping[testDoc]("native", mapping, nil)
	if err != nil {
		t.Fatal(err)
	}
	delete(mapping.Properties, "vector")
	if !schema.hasVectors || !strings.Contains(string(schema.Mapping()), "vector") {
		t.Fatal("mapping snapshot or vector hydration lost")
	}
	if _, err := SchemaFromMapping[int]("native", mapping, nil); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	if _, err := SchemaFromMapping[testDoc]("bad/name", mapping, nil); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	if _, err := SchemaFromMapping[testDoc]("native", nil, nil); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
}
