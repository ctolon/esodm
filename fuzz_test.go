package esodm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"
)

func FuzzTag(f *testing.F) {
	for _, s := range []string{"", "-", "type=text,analyzer=standard", "type=nested", "type="} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 65536 {
			t.Skip()
		}
		_, _, _ = parseTag(s)
	})
}
func FuzzQuery(f *testing.F) {
	for _, s := range []string{`{"term":{"x":1}}`, `{}`, `[]`, `{"bool":{"must":[]}}`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 65536 {
			t.Skip()
		}
		q := RawQuery([]byte(s))
		b, err := json.Marshal(NewSearch(q))
		if err == nil && !json.Valid(b) {
			t.Fatal("invalid serialized query")
		}
		finder := Finder[testDoc]{base: NewSearch(MatchAll())}.Filter(q).Not(MatchNone())
		compiled, compileErr := json.Marshal(finder.Search())
		if compileErr == nil && !json.Valid(compiled) {
			t.Fatal("invalid finder serialization")
		}
		v := []string{s}
		snapshot := Terms("x", v...)
		before, err := json.Marshal(snapshot)
		v[0] = "mutated"
		after, err2 := json.Marshal(snapshot)
		if (err == nil) != (err2 == nil) || string(before) != string(after) {
			t.Fatal("query aliasing")
		}
	})
}
func FuzzCodec(f *testing.F) {
	f.Add("Ada", int64(30), true)
	f.Add("\xff", int64(-1), false)
	f.Fuzz(func(t *testing.T, name string, age int64, active bool) {
		if len(name) > 65536 {
			t.Skip()
		}
		type doc struct {
			Name   string
			Age    int64
			Active bool
		}
		in := doc{name, age, active}
		b, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		var out doc
		if err = json.Unmarshal(b, &out); err != nil {
			t.Fatal(err)
		}
		b2, err := json.Marshal(out)
		if err != nil {
			t.Fatal(err)
		}
		var again doc
		if err = json.Unmarshal(b2, &again); err != nil || again != out {
			t.Fatal("unstable round trip")
		}
		if out.Age != age || out.Active != active {
			t.Fatal("scalar loss")
		}
	})
}
func FuzzResponse(f *testing.F) {
	f.Add(200, `{"hits":{"hits":[]}}`)
	f.Add(409, `{"error":{"type":"conflict","reason":"x"}}`)
	f.Fuzz(func(t *testing.T, status int, body string) {
		if len(body) > 65536 {
			t.Skip()
		}
		if status < 100 || status > 599 {
			status = 500
		}
		c := testClient(func(*http.Request) (*http.Response, error) { return response(status, body), nil })
		var out SearchResult[testDoc]
		_ = c.Do(context.Background(), "GET", "/", nil, nil, &out)
	})
}
func FuzzCursor(f *testing.F) {
	f.Add("e30")
	f.Add("!")
	f.Fuzz(func(t *testing.T, s string) {
		c, err := DecodeCursor(s)
		if err != nil {
			return
		}
		encoded, err := EncodeCursor(c)
		if err != nil {
			t.Fatal(err)
		}
		again, err := DecodeCursor(encoded)
		if err != nil || !reflect.DeepEqual(c, again) {
			t.Fatal("cursor roundtrip")
		}
	})
}
func FuzzBulk(f *testing.F) {
	f.Add("index", "1", "Ada")
	f.Add("delete", "x\ny", "z")
	f.Fuzz(func(t *testing.T, action, id, name string) {
		if len(action)+len(id)+len(name) > 65536 {
			t.Skip()
		}
		r := testRepo(t, nil)
		b, err := r.captureBulk(context.Background(), []BulkOperation[testDoc]{{Action: BulkAction(action), ID: id, Document: testDoc{Name: name}}})
		if err != nil {
			return
		}
		if len(b) == 0 || b[len(b)-1] != '\n' {
			t.Fatal("missing NDJSON terminator")
		}
		for _, line := range bytes.Split(bytes.TrimSuffix(b, []byte("\n")), []byte("\n")) {
			if !json.Valid(line) {
				t.Fatal("invalid NDJSON line")
			}
		}
	})
}

func FuzzSchema(f *testing.F) {
	f.Add("type=text", uint8(0))
	f.Add("type=nested", uint8(1))
	f.Add(`{"x":{"type":"dense_vector","dims":3}}`, uint8(2))
	f.Fuzz(func(t *testing.T, tag string, kind uint8) {
		if len(tag) > 4096 {
			t.Skip()
		}
		var supplied map[string]FieldMapping
		if json.Unmarshal([]byte(tag), &supplied) == nil {
			_ = validateProperties(supplied, 0)
		}
		// reflect.StructOf interns types permanently. Keep this cardinality finite
		// so hour-long fuzz runs test the schema instead of exhausting that cache.
		tags := []string{"", "type=text", "type=nested", "type=dense_vector", "type=sparse_vector", "type=flattened", "type=join", "type=boolean"}
		chosenTag := tags[int(kind)/4%len(tags)]
		types := []reflect.Type{reflect.TypeFor[string](), reflect.TypeFor[int](), reflect.TypeFor[[]float32](), reflect.TypeFor[map[string]any]()}
		typ := reflect.StructOf([]reflect.StructField{{Name: "Value", Type: types[int(kind)%len(types)], Tag: reflect.StructTag(fmt.Sprintf("json:%q es:%q", "value", chosenTag))}})
		props, _, err := inferProperties(typ, map[reflect.Type]bool{}, 0)
		if err != nil {
			return
		}
		_ = validateProperties(props, 0)
		raw, err := json.Marshal(props)
		if err != nil || !json.Valid(raw) {
			t.Fatal("invalid inferred mapping", err)
		}
	})
}
func FuzzBulkResponse(f *testing.F) {
	f.Add(`{"items":[{"delete":{"status":200,"_id":"1"}}]}`)
	f.Add(`{"items":[{"delete":{"status":409,"error":{"type":"conflict","reason":"test"}}}]}`)
	f.Fuzz(func(t *testing.T, body string) {
		if len(body) > 65536 {
			t.Skip()
		}
		r := testRepo(t, func(*http.Request) (*http.Response, error) { return response(200, body), nil })
		result, err := r.Bulk(context.Background(), []BulkOperation[testDoc]{{Action: BulkDelete, ID: "1"}}, "")
		if err == nil && (len(result.Items) != 1 || result.Items[0].Status < 200 || result.Items[0].Status >= 300) {
			t.Fatal("accepted incomplete/failed bulk")
		}
	})
}

func FuzzFacets(f *testing.F) {
	f.Add("category", "books", true)
	f.Add("", "", false)
	f.Fuzz(func(t *testing.T, name, value string, selected bool) {
		if len(name)+len(value) > 65536 {
			t.Skip()
		}
		facet := Facet{Aggregation: TermsAgg("category", 10, nil)}
		if selected {
			facet.Selection = Term("category", value)
		}
		search := WithFacets(NewSearch(MatchAll()), map[string]Facet{name: facet})
		raw, err := json.Marshal(search)
		if err == nil && !json.Valid(raw) {
			t.Fatal("invalid facet JSON")
		}
		again, err2 := json.Marshal(search)
		if (err == nil) != (err2 == nil) || string(raw) != string(again) {
			t.Fatal("unstable facet snapshot")
		}
	})
}

func FuzzMigrationCheckpoint(f *testing.F) {
	f.Add(`{"version":1,"summary":{"alias":"live","source":"old","target":"new"},"mapping":{},"source_mapping":{},"settings":null,"phase":"copying","task_id":"node:1"}`)
	f.Add(`{}`)
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 65536 {
			t.Skip()
		}
		var state MigrationState
		if json.Unmarshal([]byte(raw), &state) != nil {
			return
		}
		if state.validate() != nil {
			return
		}
		copy := state.clone()
		data, err := json.Marshal(copy)
		if err != nil || !json.Valid(data) {
			t.Fatal(err)
		}
		_ = validateTaskID(copy.TaskID)
	})
}
