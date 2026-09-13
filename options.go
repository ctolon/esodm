package esodm

import "fmt"

// SchemaOption configures an inferred schema. Options are applied in order.
// SchemaConfig replaces the complete configuration; later options override it.
type SchemaOption interface{ applySchema(*SchemaConfig) }
type schemaOptionFunc func(*SchemaConfig)

func (f schemaOptionFunc) applySchema(c *SchemaConfig) { f(c) }
func (c SchemaConfig) applySchema(dst *SchemaConfig)   { *dst = c }

// WithDynamic selects how Elasticsearch handles unmapped fields.
func WithDynamic(dynamic Dynamic) SchemaOption {
	return schemaOptionFunc(func(c *SchemaConfig) { c.Dynamic = dynamic })
}

// WithProperties replaces explicit mapping overrides. NewSchema snapshots the map.
func WithProperties(properties map[string]FieldMapping) SchemaOption {
	return schemaOptionFunc(func(c *SchemaConfig) { c.Properties = properties })
}

// WithSettings replaces index settings. NewSchema snapshots the map.
func WithSettings(settings map[string]any) SchemaOption {
	return schemaOptionFunc(func(c *SchemaConfig) { c.Settings = settings })
}

// RepositoryOption configures repository lifecycle behavior.
type RepositoryOption[T any] interface{ applyRepository(*Repository[T]) }
type repositoryOptionFunc[T any] func(*Repository[T])

func (f repositoryOptionFunc[T]) applyRepository(r *Repository[T]) { f(r) }
func (h Hooks[T]) applyRepository(r *Repository[T])                { r.hooks = h }

// WithHooks replaces lifecycle hooks. A later WithHooks replaces the entire set.
func WithHooks[T any](hooks Hooks[T]) RepositoryOption[T] {
	return repositoryOptionFunc[T](func(r *Repository[T]) { r.hooks = hooks })
}

// Refresh controls visibility of completed document and bulk writes.
type Refresh string

const (
	// RefreshFalse leaves visibility to the normal refresh schedule.
	RefreshFalse Refresh = "false"
	// RefreshTrue refreshes affected shards immediately.
	RefreshTrue Refresh = "true"
	// RefreshWaitFor waits for a scheduled refresh.
	RefreshWaitFor Refresh = "wait_for"
)

// Dynamic controls the mapping of previously unknown fields.
type Dynamic string

const (
	// DynamicStrict rejects unmapped fields.
	DynamicStrict Dynamic = "strict"
	// DynamicTrue infers mappings for new fields.
	DynamicTrue Dynamic = "true"
	// DynamicFalse retains unknown source fields without indexing them.
	DynamicFalse Dynamic = "false"
	// DynamicRuntime creates runtime fields for new fields.
	DynamicRuntime Dynamic = "runtime"
)

// Operation identifies a document lifecycle operation, including server-assigned IDs.
type Operation = BulkAction

const (
	// OpInsert creates a document with a server-generated ID.
	OpInsert Operation = "insert"
	// OpCreate creates a document with an explicit ID.
	OpCreate Operation = "create"
	// OpIndex replaces a document with an explicit ID.
	OpIndex Operation = "index"
	// OpUpdate updates a document.
	OpUpdate Operation = "update"
	// OpDelete deletes a document.
	OpDelete Operation = "delete"
)

// WriteOption configures a document write. Options are applied in order.
// WriteOptions replaces all options; subsequent functional options override fields.
type WriteOption interface{ applyWrite(*WriteOptions) }
type writeOptionFunc func(*WriteOptions)

func (f writeOptionFunc) applyWrite(o *WriteOptions) { f(o) }
func (o WriteOptions) applyWrite(dst *WriteOptions)  { *dst = o }

// WithRefresh controls write visibility.
func WithRefresh(refresh Refresh) WriteOption {
	return writeOptionFunc(func(o *WriteOptions) { o.Refresh = refresh })
}

// WithRouting selects document routing.
func WithRouting(routing string) WriteOption {
	return writeOptionFunc(func(o *WriteOptions) { o.Routing = routing })
}

// WithPipeline selects an ingest pipeline.
func WithPipeline(pipeline string) WriteOption {
	return writeOptionFunc(func(o *WriteOptions) { o.Pipeline = pipeline })
}

// IfMatches applies an independent copy of metadata's concurrency tokens and routing.
func IfMatches(metadata Metadata) WriteOption {
	snapshot := metadata.Conditional()
	return writeOptionFunc(func(o *WriteOptions) {
		c := Metadata{SeqNo: snapshot.IfSeqNo, PrimaryTerm: snapshot.IfPrimaryTerm, Routing: snapshot.Routing}.Conditional()
		o.IfSeqNo = c.IfSeqNo
		o.IfPrimaryTerm = c.IfPrimaryTerm
		o.Routing = c.Routing
	})
}
func writeOptions(options []WriteOption) (WriteOptions, error) {
	var out WriteOptions
	for _, option := range options {
		if option == nil || nilValue(option) {
			return out, fmt.Errorf("%w: nil write option", ErrValidation)
		}
		option.applyWrite(&out)
	}
	return out, nil
}
