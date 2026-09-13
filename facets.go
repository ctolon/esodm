package esodm

import (
	"encoding/json"
	"fmt"
	"slices"
)

// Facet combines a bucket/metric aggregation with an optional selected-value filter.
// Within a facet, combine multiple selected values with Or or Terms as appropriate.
type Facet struct {
	// Aggregation defines the facet buckets or metric; it must be a valid aggregation.
	Aggregation Aggregation
	// Selection is this facet selection filter; a zero Query means no selection. Its own aggregation
	// excludes this selection while applying other selections.
	Selection Query
}

// WithFacets builds disjunctive facets: hits use all selections, while each facet
// counts values using every selection except its own. The base query still scopes
// both hits and counts. Facet names must not collide with existing aggregations.
// An existing post_filter is rejected because it would make count semantics ambiguous.
func WithFacets(base Search, facets map[string]Facet) Search {
	if base.err != nil {
		return base
	}
	if _, ok := base.fields["post_filter"]; ok {
		return Search{err: fmt.Errorf("%w: facets own post_filter", ErrValidation)}
	}
	if len(facets) == 0 {
		return base
	}
	var names []string
	for name := range facets {
		names = append(names, name)
	}
	slices.Sort(names)
	aggs := map[string]json.RawMessage{}
	if raw, ok := base.fields["aggs"]; ok {
		if err := json.Unmarshal(raw, &aggs); err != nil {
			return Search{err: err}
		}
	}
	if raw, ok := base.fields["aggregations"]; ok {
		if len(aggs) > 0 {
			return Search{err: fmt.Errorf("%w: ambiguous aggregation keys", ErrValidation)}
		}
		if err := json.Unmarshal(raw, &aggs); err != nil {
			return Search{err: err}
		}
		base = base.without("aggregations")
	}
	if aggs == nil {
		aggs = map[string]json.RawMessage{}
	}
	var selected []Query
	for _, name := range names {
		f := facets[name]
		if name == "" {
			return Search{err: fmt.Errorf("%w: facet name required", ErrValidation)}
		}
		if _, ok := aggs[name]; ok {
			return Search{err: fmt.Errorf("%w: duplicate facet %s", ErrValidation, name)}
		}
		if err := f.Aggregation.Err(); err != nil {
			return Search{err: err}
		}
		if f.Selection.err != nil {
			return Search{err: f.Selection.err}
		}
		if len(f.Selection.data) > 0 {
			selected = append(selected, f.Selection)
		}
	}
	for _, name := range names {
		var others []Query
		for _, other := range names {
			if other != name && len(facets[other].Selection.data) > 0 {
				others = append(others, facets[other].Selection)
			}
		}
		filter := MatchAll()
		if len(others) > 0 {
			filter = And(others...)
		}
		agg := Agg("filter", filter, map[string]Aggregation{"values": facets[name].Aggregation})
		raw, err := json.Marshal(agg)
		if err != nil {
			return Search{err: err}
		}
		aggs[name] = raw
	}
	base = base.With("aggs", aggs)
	if len(selected) > 0 {
		base = base.With("post_filter", And(selected...))
	}
	return base
}

// DecodeFacet decodes the inner aggregation of a WithFacets result, with or without typed_keys.
func DecodeFacet[A any](aggs map[string]json.RawMessage, name string) (A, error) {
	outer, err := DecodeAgg[map[string]json.RawMessage](aggs, name)
	if err != nil {
		var zero A
		return zero, err
	}
	return DecodeAgg[A](outer, "values")
}
