package esodm_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ctolon/esodm/esodmtest"

	"github.com/ctolon/esodm"
	"github.com/elastic/go-elasticsearch/v9/typedapi/esdsl"
)

func ExampleNewSearch() {
	age := esodm.OrderedValue[int]("age")
	search := esodm.NewSearch(esodm.Filter(age.GTE(18))).Size(10)
	data, err := json.Marshal(search)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(data))
	// Output: {"query":{"bool":{"filter":[{"range":{"age":{"gte":18}}}]}},"seq_no_primary_term":true,"size":10,"track_total_hits":true}
}

func ExampleFromQuery() {
	query := esodm.FromQuery(esdsl.NewMatchQuery("title", "Go"))
	data, err := json.Marshal(query)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(data))
	// Output: {"match":{"title":{"query":"Go"}}}
}

type exampleDocument struct {
	Name string `json:"name"`
}

func exampleRepository(options ...esodm.RepositoryOption[exampleDocument]) (*esodm.Repository[exampleDocument], *esodmtest.Recorder) {
	recorder := &esodmtest.Recorder{}
	client, err := esodm.ConnectVersion(context.Background(), recorder, esodm.Version{Major: 9, Minor: 5, Patch: 2}, esodm.Config{})
	if err != nil {
		panic(err)
	}
	schema, err := esodm.NewSchema[exampleDocument]("people")
	if err != nil {
		panic(err)
	}
	repo, err := esodm.NewRepository(client, schema, options...)
	if err != nil {
		panic(err)
	}
	return repo, recorder
}
func ExampleRepository_Find() {
	repo, recorder := exampleRepository()
	recorder.On("POST", "/people/_search").Reply(200, `{"hits":{"hits":[{"_id":"1","_source":{"name":"Ada"}}]}}`)
	hits, err := repo.Find(esodm.Match("name", "Ada")).Size(10).All(context.Background())
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(hits[0].ID, hits[0].Source.Name)
	// Output: 1 Ada
}
func ExampleRepository_Create() {
	repo, recorder := exampleRepository()
	recorder.On("PUT", "/people/_create/1").Reply(201, `{"_id":"1","result":"created"}`)
	result, err := repo.Create(context.Background(), "1", exampleDocument{Name: "Ada"}, esodm.WithRefresh(esodm.RefreshWaitFor))
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(result.ID, result.Result)
	// Output: 1 created
}
func ExampleFinder_Page() {
	repo, recorder := exampleRepository()
	recorder.On("POST", "/people/_search").Reply(200, `{"hits":{"total":{"value":3,"relation":"eq"},"hits":[{"_id":"1","_source":{"name":"Ada"}}]}}`)
	page, err := repo.Find().Page(context.Background(), 1, 1)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(page.Number, page.Total, page.Exact, page.HasNext)
	// Output: 1 3 true true
}
func ExampleRepository_Each() {
	repo, recorder := exampleRepository()
	recorder.On("POST", "/people/_pit").Reply(200, `{"id":"pit"}`)
	recorder.On("POST", "/_search").Reply(200, `{"hits":{"hits":[{"_id":"1","_source":{"name":"Ada"},"sort":[1]}]}}`)
	recorder.On("DELETE", "/_pit").Reply(200, `{"succeeded":true}`)
	for hit, err := range repo.Each(context.Background(), esodm.NewSearch(esodm.MatchAll()), 100, "1m") {
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println(hit.Source.Name)
		break
	}
	fmt.Println(recorder.Requests()[2].Method)
	// Output:
	// Ada
	// DELETE
}
func ExampleRepository_BulkSeq() {
	repo, recorder := exampleRepository()
	recorder.On("POST", "/_bulk").Reply(200, `{"items":[{"create":{"_id":"1","status":201}}]}`)
	seq := func(yield func(esodm.BulkOperation[exampleDocument]) bool) {
		yield(esodm.BulkOperation[exampleDocument]{Action: esodm.BulkCreate, ID: "1", Document: exampleDocument{Name: "Ada"}})
	}
	err := repo.BulkSeq(context.Background(), seq, esodm.BulkStreamOptions{BatchSize: 100}, func(batch esodm.BulkBatchResult) error { fmt.Println(len(batch.Result.Items)); return batch.Err })
	if err != nil {
		fmt.Println(err)
	}
	// Output: 1
}
func ExampleNewPatch() {
	name := esodm.Text("name")
	patch, err := esodm.NewPatch(name.Set("Grace"))
	if err != nil {
		fmt.Println(err)
		return
	}
	data, err := json.Marshal(patch)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(string(data))
	// Output: {"name":"Grace"}
}
func ExampleSearchResult_Aggregates() {
	result := esodm.SearchResult[exampleDocument]{Aggregations: map[string]json.RawMessage{"avg#price": json.RawMessage(`{"value":25}`)}}
	aggregates, err := result.Aggregates()
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(len(aggregates))
	// Output: 1
}
