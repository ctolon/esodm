package esodm

import (
	"fmt"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
)

// GeoPoint is the official latitude/longitude source representation.
type GeoPoint = types.LatLonGeoLocation

// GeoField provides geo query wrappers around official request types.
type GeoField struct{ Field[GeoPoint] }

// Geo creates a descriptor for a geo_point mapping.
func Geo(name string) GeoField { return GeoField{NewField[GeoPoint](name)} }

// WithinDistance selects points within an Elasticsearch distance, such as "5km".
func (f GeoField) WithinDistance(distance string, point types.GeoLocation) Query {
	if distance == "" || point == nil {
		return Query{err: fmt.Errorf("%w: distance and point required", ErrValidation)}
	}
	return checkedField(f.name, &types.Query{GeoDistance: &types.GeoDistanceQuery{Distance: distance, GeoDistanceQuery: map[string]types.GeoLocation{f.name: point}}})
}

// WithinBox selects points inside official geo bounds.
func (f GeoField) WithinBox(bounds types.GeoBounds) Query {
	return checkedField(f.name, &types.Query{GeoBoundingBox: &types.GeoBoundingBoxQuery{GeoBoundingBoxQuery: map[string]types.GeoBounds{f.name: bounds}}})
}

// GeoShape snapshots a complete official geo_shape query.
func GeoShape(query types.GeoShapeQuery) Query { return FromQuery(&types.Query{GeoShape: &query}) }

// Percolate snapshots an official percolate query. Exactly one document source is required.
func Percolate(query types.PercolateQuery) Query {
	sources := 0
	if len(query.Document) > 0 {
		sources++
	}
	if len(query.Documents) > 0 {
		sources++
	}
	if query.Id != nil {
		sources++
	}
	if sources != 1 {
		return Query{err: fmt.Errorf("%w: percolate needs exactly one document source", ErrValidation)}
	}
	return checkedField(query.Field, &types.Query{Percolate: &query})
}

// ObjectField groups queries and field paths for an object or nested mapping.
type ObjectField struct{ Field[any] }

// Object creates an object path descriptor.
func Object(name string) ObjectField { return ObjectField{NewField[any](name)} }

// Nested wraps a query in this descriptor's nested path.
func (f ObjectField) Nested(query Query) Query { return Nested(f.name, query) }

// ChildField creates a typed child descriptor without generic methods (Go 1.26 compatible).
func ChildField[T any](parent ObjectField, name string) Field[T] {
	return NewField[T](parent.name + "." + name)
}
