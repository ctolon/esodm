package esodm

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v9/typedapi/esdsl"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types/enums/childscoremode"
)

// Query owns its serialized representation. Building from mutable caller values
// snapshots them immediately; a Query can be reused across goroutines.
type Query struct {
	data json.RawMessage
	err  error
}

// MarshalJSON serializes the snapshot, returning any deferred construction error.
func (q Query) MarshalJSON() ([]byte, error) {
	if q.err != nil {
		return nil, q.err
	}
	if q.data == nil {
		return []byte(`{"match_all":{}}`), nil
	}
	return append([]byte(nil), q.data...), nil
}
func query(v any) Query {
	b, err := json.Marshal(v)
	return Query{b, err}
}

// RawQuery snapshots a JSON query containing exactly one root clause.
func RawQuery(raw json.RawMessage) Query {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || len(obj) != 1 {
		return Query{err: fmt.Errorf("%w: query must contain one root clause", ErrValidation)}
	}
	return Query{data: append(json.RawMessage(nil), raw...)}
}

// FromQuery snapshots an official v9 typed query or esdsl builder. The query is
// serialized immediately; subsequent builder mutations do not affect the snapshot.
// Only clauses supported by the connected server should be used with ES 8.
func FromQuery(value types.QueryVariant) Query {
	if value == nil || nilValue(value) {
		return Query{err: fmt.Errorf("%w: nil typed query", ErrValidation)}
	}
	return query(value.QueryCaster())
}

// typed preserves raw clauses and numeric precision when composing official types.
func (q Query) typed() (*types.Query, error) {
	data, err := q.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var clauses map[string]json.RawMessage
	if err := json.Unmarshal(data, &clauses); err != nil {
		return nil, err
	}
	return &types.Query{AdditionalQueryProperty: clauses}, nil
}

// MatchAll matches every document.
func MatchAll() Query {
	return FromQuery(esdsl.NewMatchAllQuery())
}

// MatchNone matches no documents.
func MatchNone() Query {
	return FromQuery(esdsl.NewMatchNoneQuery())
}

// Term matches an exact field value using the official term query model.
func Term[T any](field string, value T) Query {
	return checkedField(field, &types.Query{Term: map[string]types.TermQuery{field: {Value: value}}})
}

// Terms matches any supplied exact value; an empty list matches nothing.
func Terms[T any](field string, values ...T) Query {
	if len(values) == 0 {
		return MatchNone()
	}
	return checkedField(field, &types.Query{Terms: &types.TermsQuery{TermsQuery: map[string]types.TermsQueryField{field: values}}})
}

// Match analyzes text and matches it against the field.
func Match(field, text string) Query {
	return checkedField(field, esdsl.NewMatchQuery(field, text))
}

// MatchPhrase matches analyzed terms in phrase order.
func MatchPhrase(field, text string) Query {
	return checkedField(field, esdsl.NewMatchPhraseQuery(field, text))
}

// Prefix matches indexed terms beginning with prefix.
func Prefix(field, prefix string) Query {
	return checkedField(field, esdsl.NewPrefixQuery(field, prefix))
}

// Wildcard matches indexed terms against an Elasticsearch wildcard pattern.
func Wildcard(field, pattern string) Query {
	return checkedField(field, esdsl.NewWildcardQuery(field, pattern))
}

// Exists matches documents with an indexed value for the field.
func Exists(field string) Query {
	return checkedField(field, esdsl.NewExistsQuery().Field(field))
}

// IDs matches document IDs; an empty list matches nothing.
func IDs(ids ...string) Query {
	if len(ids) == 0 {
		return MatchNone()
	}
	return FromQuery(esdsl.NewIdsQuery().Values(ids...))
}

// MultiMatch analyzes text across the supplied fields.
func MultiMatch(text string, fields ...string) Query {
	return FromQuery(esdsl.NewMultiMatchQuery(text).Fields(fields...))
}
func checkedField(field string, value types.QueryVariant) Query {
	if field == "" {
		return Query{err: fmt.Errorf("%w: empty field", ErrValidation)}
	}
	return FromQuery(value)
}

// And requires every query to match; an empty list matches all documents.
func And(queries ...Query) Query {
	return boolean("must", queries)
}

// Filter requires every query to match without contributing to the score.
func Filter(queries ...Query) Query {
	return boolean("filter", queries)
}

// Or requires at least one query to match; an empty list matches nothing.
func Or(queries ...Query) Query {
	if len(queries) == 0 {
		return MatchNone()
	}
	return boolean("should", queries)
}

// Not excludes documents matching any supplied query.
func Not(queries ...Query) Query {
	return boolean("must_not", queries)
}
func boolean(kind string, queries []Query) Query {
	if len(queries) == 0 {
		return MatchAll()
	}
	clauses := make([]types.QueryVariant, len(queries))
	for i, q := range queries {
		value, err := q.typed()
		if err != nil {
			return Query{err: err}
		}
		clauses[i] = value
	}
	builder := esdsl.NewBoolQuery()
	switch kind {
	case "must":
		builder.Must(clauses...)
	case "filter":
		builder.Filter(clauses...)
	case "should":
		builder.Should(clauses...).MinimumShouldMatch(esdsl.NewMinimumShouldMatch().Int(1))
	case "must_not":
		builder.MustNot(clauses...)
	}
	return FromQuery(builder)
}

// Nested evaluates q against nested documents at path without scoring.
func Nested(path string, q Query) Query {
	value, err := q.typed()
	if err != nil {
		return Query{err: err}
	}
	return FromQuery(esdsl.NewNestedQuery(value).Path(path).ScoreMode(childscoremode.None))
}

// HasChild matches parents with a child of kind matching q.
func HasChild(kind string, q Query) Query {
	value, err := q.typed()
	if err != nil {
		return Query{err: err}
	}
	return FromQuery(esdsl.NewHasChildQuery(value).Type(kind).ScoreMode(childscoremode.None))
}

// HasParent matches children whose parent of kind matches q.
func HasParent(kind string, q Query) Query {
	value, err := q.typed()
	if err != nil {
		return Query{err: err}
	}
	return FromQuery(esdsl.NewHasParentQuery(value).ParentType(kind).Score(false))
}

// ParentID matches children of kind belonging to the given parent ID.
func ParentID(kind, id string) Query {
	return FromQuery(esdsl.NewParentIdQuery().Type(kind).Id(id))
}

// Field binds a field name to its Go value type for query construction.
type Field[T any] struct {
	name string
}

// NewField creates a typed descriptor for an Elasticsearch field name.
func NewField[T any](name string) Field[T] {
	return Field[T]{name}
}

// Name returns the Elasticsearch field name.
func (f Field[T]) Name() string {
	return f.name
}

// Eq matches the field against an exact value.
func (f Field[T]) Eq(value T) Query {
	return Term(f.name, value)
}

// In matches any supplied field value; an empty list matches nothing.
func (f Field[T]) In(values ...T) Query {
	return Terms(f.name, values...)
}

// Exists matches documents with an indexed value for the field.
func (f Field[T]) Exists() Query {
	return Exists(f.name)
}

// TextField adds analyzed text queries to a string field descriptor.
type TextField struct {
	Field[string]
}

// Text creates a descriptor for an analyzed text field.
func Text(name string) TextField {
	return TextField{NewField[string](name)}
}

// Match analyzes text and matches it against the field.
func (f TextField) Match(value string) Query {
	return Match(f.name, value)
}

// Phrase matches analyzed terms in phrase order.
func (f TextField) Phrase(value string) Query {
	return MatchPhrase(f.name, value)
}

// Ordered permits numeric and string field values in range queries.
type Ordered interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~float32 | ~float64 | ~string
}

// OrderedField adds range comparisons to a typed field descriptor.
type OrderedField[T Ordered] struct {
	Field[T]
}

// OrderedValue creates a descriptor supporting range comparisons.
func OrderedValue[T Ordered](name string) OrderedField[T] {
	return OrderedField[T]{NewField[T](name)}
}

// GT matches field values strictly greater than v.
func (f OrderedField[T]) GT(v T) Query {
	return rangeQuery(f.name, "gt", v)
}

// GTE matches field values greater than or equal to v.
func (f OrderedField[T]) GTE(v T) Query {
	return rangeQuery(f.name, "gte", v)
}

// LT matches field values strictly less than v.
func (f OrderedField[T]) LT(v T) Query {
	return rangeQuery(f.name, "lt", v)
}

// LTE matches field values less than or equal to v.
func (f OrderedField[T]) LTE(v T) Query {
	return rangeQuery(f.name, "lte", v)
}

// Between matches values in the inclusive range [lo, hi].
func (f OrderedField[T]) Between(lo, hi T) Query {
	lower, err := json.Marshal(lo)
	if err != nil {
		return Query{err: err}
	}
	upper, err := json.Marshal(hi)
	if err != nil {
		return Query{err: err}
	}
	return checkedField(f.name, esdsl.NewUntypedRangeQuery(f.name).Gte(lower).Lte(upper))
}
func rangeQuery(field, bound string, value any) Query {
	data, err := json.Marshal(value)
	if err != nil {
		return Query{err: err}
	}
	builder := esdsl.NewUntypedRangeQuery(field)
	switch bound {
	case "gt":
		builder.Gt(data)
	case "gte":
		builder.Gte(data)
	case "lt":
		builder.Lt(data)
	case "lte":
		builder.Lte(data)
	}
	return checkedField(field, builder)
}

// Aggregation supports arbitrary Elasticsearch metric, bucket and pipeline kinds.
// Constructors snapshot parameters; unsupported server options are returned as errors.
type Aggregation struct {
	data json.RawMessage
	err  error
}

// MarshalJSON serializes the snapshot, returning any deferred construction error.
func (a Aggregation) MarshalJSON() ([]byte, error) {
	if a.err != nil {
		return nil, a.err
	}
	if len(a.data) == 0 {
		return nil, fmt.Errorf("%w: empty aggregation", ErrValidation)
	}
	return append([]byte(nil), a.data...), nil
}

// FromAggregation snapshots an official aggregation or esdsl aggregation builder.
func FromAggregation(value types.AggregationsVariant) Aggregation {
	if value == nil || nilValue(value) {
		return Aggregation{err: fmt.Errorf("%w: nil typed aggregation", ErrValidation)}
	}
	data, err := json.Marshal(value.AggregationsCaster())
	return Aggregation{data: data, err: err}
}

// Agg snapshots an arbitrary aggregation kind and its child aggregations.
// Prefer FromAggregation with official types for compile-time option checking.
func Agg(kind string, params any, children map[string]Aggregation) Aggregation {
	if kind == "" {
		return Aggregation{err: fmt.Errorf("%w: empty aggregation kind", ErrValidation)}
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return Aggregation{err: err}
	}
	value := types.Aggregations{AdditionalAggregationsProperty: map[string]json.RawMessage{kind: raw}}
	return aggregationChildren(&value, children)
}
func aggregationChildren(value *types.Aggregations, children map[string]Aggregation) Aggregation {
	if len(children) > 0 {
		value.Aggregations = make(map[string]types.Aggregations, len(children))
		for name, child := range children {
			raw, err := child.MarshalJSON()
			if err != nil {
				return Aggregation{err: err}
			}
			var clauses map[string]json.RawMessage
			if err := json.Unmarshal(raw, &clauses); err != nil {
				return Aggregation{err: err}
			}
			value.Aggregations[name] = types.Aggregations{AdditionalAggregationsProperty: clauses}
		}
	}
	return FromAggregation(value)
}

// TermsAgg groups values into up to size term buckets with optional children.
func TermsAgg(field string, size int, children map[string]Aggregation) Aggregation {
	return aggregationChildren(esdsl.NewTermsAggregation().Field(field).Size(size).AggregationsCaster(), children)
}

// MetricAgg computes a field metric using an official model for known kinds.
// Unknown kinds are passed through for server-side validation.
//
// Deprecated: Use named metric constructors or FromAggregation.
func MetricAgg(kind, field string) Aggregation {
	switch kind {
	case "avg":
		return FromAggregation(esdsl.NewAverageAggregation().Field(field))
	case "sum":
		return FromAggregation(esdsl.NewSumAggregation().Field(field))
	case "min":
		return FromAggregation(esdsl.NewMinAggregation().Field(field))
	case "max":
		return FromAggregation(esdsl.NewMaxAggregation().Field(field))
	case "stats":
		return FromAggregation(esdsl.NewStatsAggregation().Field(field))
	case "cardinality":
		return FromAggregation(esdsl.NewCardinalityAggregation().Field(field))
	case "value_count":
		return FromAggregation(esdsl.NewValueCountAggregation().Field(field))
	default:
		return Agg(kind, map[string]any{"field": field}, nil)
	}
}

// PipelineAgg is the dynamic escape hatch for pipeline aggregation options.
// Prefer FromAggregation with an official pipeline builder for typed options.
func PipelineAgg(kind string, paths any, script string) Aggregation {
	return Agg(kind, map[string]any{"buckets_path": paths, "script": script}, nil)
}

// DecodeAggregation decodes a named aggregation from a raw-document search result.
func DecodeAggregation[T any](result SearchResult[json.RawMessage], name string) (T, error) {
	return DecodeAgg[T](result.Aggregations, name)
}

// DecodeAgg decodes a named aggregation into T, returning ErrNotFound if absent.
func DecodeAgg[T any](aggregations map[string]json.RawMessage, name string) (T, error) {
	var v T
	raw, ok := aggregations[name]
	if !ok {
		for key, value := range aggregations {
			_, suffix, typed := strings.Cut(key, "#")
			if typed && suffix == name {
				if ok {
					return v, fmt.Errorf("%w: ambiguous aggregation name", ErrValidation)
				}
				raw, ok = value, true
			}
		}
	}
	if !ok {
		return v, ErrNotFound
	}
	err := json.Unmarshal(raw, &v)
	return v, err
}

// Err returns a deferred query construction error.
func (q Query) Err() error { return q.err }

// Err returns a deferred aggregation construction error, including an empty snapshot.
func (a Aggregation) Err() error { _, err := a.MarshalJSON(); return err }

// DateField supports range queries on date and date_nanos fields.
type DateField struct{ Field[time.Time] }

// Date creates a descriptor whose values use time.Time JSON encoding.
func Date(name string) DateField { return DateField{NewField[time.Time](name)} }

// GT matches dates after v.
func (f DateField) GT(v time.Time) Query { return rangeQuery(f.name, "gt", v) }

// GTE matches dates on or after v.
func (f DateField) GTE(v time.Time) Query { return rangeQuery(f.name, "gte", v) }

// LT matches dates before v.
func (f DateField) LT(v time.Time) Query { return rangeQuery(f.name, "lt", v) }

// LTE matches dates on or before v.
func (f DateField) LTE(v time.Time) Query { return rangeQuery(f.name, "lte", v) }

// Between matches an inclusive date interval.
func (f DateField) Between(lo, hi time.Time) Query {
	lower, err := json.Marshal(lo)
	if err != nil {
		return Query{err: err}
	}
	upper, err := json.Marshal(hi)
	if err != nil {
		return Query{err: err}
	}
	return checkedField(f.name, esdsl.NewUntypedRangeQuery(f.name).Gte(lower).Lte(upper))
}

// Keyword selects the conventional keyword multi-field; it must exist in the mapping.
func (f TextField) Keyword() Field[string] { return NewField[string](f.name + ".keyword") }

// Asc sorts this field in ascending order.
func (f Field[T]) Asc() Sort { return Sort{Field: f.name} }

// Desc sorts this field in descending order.
func (f Field[T]) Desc() Sort { return Sort{Field: f.name, Desc: true} }

// Assignment is a snapshotted field value for NewPatch. Zero assignments are invalid.
type Assignment struct {
	name  string
	value json.RawMessage
	err   error
}

// Set snapshots a value matching this field's type.
func (f Field[T]) Set(value T) Assignment {
	data, err := json.Marshal(value)
	return Assignment{f.name, data, err}
}

// Clear writes JSON null; it does not remove the field from _source.
func (f Field[T]) Clear() Assignment { return Assignment{name: f.name, value: json.RawMessage("null")} }

// NewPatch builds a partial document, rejecting invalid or duplicate field names.
// Dotted names are rejected: Elasticsearch doc patches require nested objects.
func NewPatch(assignments ...Assignment) (Patch, error) {
	out := Patch{}
	for _, a := range assignments {
		if a.err != nil {
			return nil, fmt.Errorf("%w: %w", ErrValidation, a.err)
		}
		if a.name == "" || strings.Contains(a.name, ".") || len(a.value) == 0 {
			return nil, fmt.Errorf("%w: invalid patch field %q", ErrValidation, a.name)
		}
		if _, ok := out[a.name]; ok {
			return nil, fmt.Errorf("%w: duplicate patch field %s", ErrValidation, a.name)
		}
		out[a.name] = append(json.RawMessage(nil), a.value...)
	}
	return out, nil
}

// Avg constructs an avg metric aggregation using the official API.
func Avg(field string) Aggregation { return MetricAgg("avg", field) }

// Sum constructs a sum metric aggregation using the official API.
func Sum(field string) Aggregation { return MetricAgg("sum", field) }

// Min constructs a min metric aggregation using the official API.
func Min(field string) Aggregation { return MetricAgg("min", field) }

// Max constructs a max metric aggregation using the official API.
func Max(field string) Aggregation { return MetricAgg("max", field) }

// Stats constructs a stats metric aggregation using the official API.
func Stats(field string) Aggregation { return MetricAgg("stats", field) }

// Cardinality constructs a cardinality metric aggregation using the official API.
func Cardinality(field string) Aggregation { return MetricAgg("cardinality", field) }

// ValueCount constructs a value_count metric aggregation using the official API.
func ValueCount(field string) Aggregation { return MetricAgg("value_count", field) }

// Semantic searches a semantic_text field through the official query builder.
// The mapping and inference endpoint must be configured on the server.
func Semantic(field, text string) Query {
	return checkedField(field, esdsl.NewSemanticQuery(field, text))
}
