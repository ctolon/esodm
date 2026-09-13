package esodm

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"
)

// Indexed declares a model's index or alias. IndexName must work on a zero model.
type Indexed interface{ IndexName() string }

// Identified supplies an explicit document ID for Save and full-document bulk operations.
type Identified interface{ DocumentID() string }

// Routed supplies default routing when write options do not specify it.
type Routed interface{ DocumentRouting() string }

// DocumentMeta provides opt-in ID and routing metadata excluded from _source.
// Elasticsearch-generated IDs are returned in WriteResult, not written into caller values.
type DocumentMeta struct {
	// ID is application metadata excluded from _source. Save requires a nonempty ID; Insert requires
	// it to be empty.
	ID string `json:"-"`
	// Routing is the default route for full-document writes; explicit nonempty write routing
	// overrides it.
	Routing string `json:"-"`
}

// DocumentID returns the model's ID.
func (m DocumentMeta) DocumentID() string { return m.ID }

// DocumentRouting returns the model's routing value.
func (m DocumentMeta) DocumentRouting() string { return m.Routing }

// NewSchemaFor reads IndexName from a zero T or *T, then infers its mapping.
func NewSchemaFor[T any](config ...SchemaOption) (*Schema[T], error) {
	var model T
	if reflect.TypeFor[T]().Kind() != reflect.Struct {
		return nil, fmt.Errorf("%w: model must be a struct", ErrValidation)
	}
	initializeEmbeds(reflect.ValueOf(&model).Elem(), 0)
	indexed, ok := any(&model).(Indexed)
	if !ok {
		return nil, fmt.Errorf("%w: model must implement Indexed", ErrValidation)
	}
	return NewSchema[T](indexed.IndexName(), config...)
}

// Ensurer describes a repository's index initialization contract.
type Ensurer interface {
	Index() string
	EnsureIndex(context.Context) error
}

// Registry explicitly groups repositories for sequential, non-migrating initialization.
// Its zero value is ready to use. Add and snapshot operations are concurrency safe.
type Registry struct {
	mu      sync.RWMutex
	entries []Ensurer
}

// Add atomically registers repositories, rejecting nil entries and duplicate index names.
func (g *Registry) Add(entries ...Ensurer) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	names := map[string]bool{}
	for _, e := range g.entries {
		names[e.Index()] = true
	}
	for _, e := range entries {
		if nilValue(e) {
			return fmt.Errorf("%w: nil repository", ErrValidation)
		}
		if err := validateIndexName(e.Index()); err != nil {
			return err
		}
		if names[e.Index()] {
			return fmt.Errorf("%w: duplicate index %s", ErrValidation, e.Index())
		}
		names[e.Index()] = true
	}
	g.entries = append(g.entries, entries...)
	return nil
}
func (g *Registry) snapshot() []Ensurer {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return append([]Ensurer(nil), g.entries...)
}

// Indexes returns registration-order index names in an independent slice.
func (g *Registry) Indexes() []string {
	entries := g.snapshot()
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Index()
	}
	return names
}

// EnsureAll initializes each index, stopping at the first error. It never migrates existing mappings.
func (g *Registry) EnsureAll(ctx context.Context) error {
	for _, e := range g.snapshot() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := e.EnsureIndex(ctx); err != nil {
			return fmt.Errorf("esodm: initialize %s: %w", e.Index(), err)
		}
	}
	return nil
}

// Stamper opts a document into audit timestamps. StampFields returns top-level JSON names.
// Implementations must mutate only the receiver's own fields or anonymous embedded models.
type Stamper interface {
	Stamp(operation Operation, now time.Time)
	StampFields() (created, updated string)
}

// Timestamps provides opt-in UTC creation and modification dates. Embed by value or pointer.
type Timestamps struct {
	// CreatedAt is preserved when nonzero; otherwise Stamp sets it to the current UTC audit time.
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt is replaced by Stamp on full-document writes. Partial updates add an audit time only
	// when the patch does not already contain updated_at.
	UpdatedAt time.Time `json:"updated_at"`
}

// Stamp preserves an existing creation date and updates the modification date.
func (t *Timestamps) Stamp(_ Operation, now time.Time) {
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
}

// StampFields returns the JSON names used for partial updates.
func (*Timestamps) StampFields() (string, string) { return "created_at", "updated_at" }

// initializeEmbeds copies anonymous pointers before hooks/stamping to protect caller models.
// Recursive anonymous models are rejected by schema inference; the guard also bounds explicit schemas.
func initializeEmbeds(v reflect.Value, depth int) {
	if depth >= 32 || v.Kind() != reflect.Struct {
		return
	}
	for i := range v.NumField() {
		f := v.Field(i)
		if !v.Type().Field(i).Anonymous || !f.CanSet() {
			continue
		}
		if f.Kind() == reflect.Pointer && f.Type().Elem().Kind() == reflect.Struct {
			copy := reflect.New(f.Type().Elem())
			if !f.IsNil() {
				copy.Elem().Set(f.Elem())
			}
			f.Set(copy)
			initializeEmbeds(copy.Elem(), depth+1)
		} else if f.Kind() == reflect.Struct {
			initializeEmbeds(f, depth+1)
		}
	}
}
func (r *Repository[T]) now() time.Time {
	if r.client.config.Now != nil {
		return r.client.config.Now().UTC()
	}
	return time.Now().UTC()
}
func (r *Repository[T]) stamp(doc *T, operation Operation) {
	initializeEmbeds(reflect.ValueOf(doc).Elem(), 0)
	if s, ok := any(doc).(Stamper); ok {
		s.Stamp(operation, r.now())
	}
}
func (r *Repository[T]) stampPatch(patch Patch) error {
	var model T
	s, ok := any(&model).(Stamper)
	if !ok {
		return nil
	}
	initializeEmbeds(reflect.ValueOf(&model).Elem(), 0)
	_, updated := s.StampFields()
	if updated == "" || strings.Contains(updated, ".") {
		return fmt.Errorf("%w: empty timestamp field", ErrValidation)
	}
	if _, set := patch[updated]; !set {
		patch[updated] = r.now()
	}
	return nil
}
func modelOptions[T any](doc *T, options WriteOptions) WriteOptions {
	if options.Routing == "" {
		if routed, ok := any(doc).(Routed); ok {
			initializeEmbeds(reflect.ValueOf(doc).Elem(), 0)
			options.Routing = routed.DocumentRouting()
		}
	}
	return options
}

// Save indexes a model using DocumentID and optional DocumentRouting.
func (r *Repository[T]) Save(ctx context.Context, doc T, opts ...WriteOption) (WriteResult, error) {
	options, err := writeOptions(opts)
	if err != nil {
		return WriteResult{}, err
	}
	id, ok := any(&doc).(Identified)
	if !ok {
		return WriteResult{}, fmt.Errorf("%w: model must implement Identified", ErrValidation)
	}
	initializeEmbeds(reflect.ValueOf(&doc).Elem(), 0)
	return r.Replace(ctx, id.DocumentID(), doc, modelOptions(&doc, options))
}

// Insert creates a document with a server-generated ID. Concurrency tokens are invalid.
func (r *Repository[T]) Insert(ctx context.Context, doc T, opts ...WriteOption) (WriteResult, error) {
	options, err := writeOptions(opts)
	if err != nil {
		return WriteResult{}, err
	}
	initializeEmbeds(reflect.ValueOf(&doc).Elem(), 0)
	if id, ok := any(&doc).(Identified); ok && id.DocumentID() != "" {
		return WriteResult{}, fmt.Errorf("%w: use Save or Create for an identified model", ErrValidation)
	}
	return r.write(ctx, "insert", "", doc, modelOptions(&doc, options))
}

// MetadataReceiver opts source models into hydration of Elasticsearch metadata.
type MetadataReceiver interface{ SetDocumentMetadata(Metadata) }

// SetDocumentMetadata hydrates excluded ID and routing fields after repository reads.
func (m *DocumentMeta) SetDocumentMetadata(metadata Metadata) {
	m.ID = metadata.ID
	m.Routing = metadata.Routing
}
func (r *Repository[T]) afterRead(ctx context.Context, hit *Hit[T]) error {
	if hit.Routing == "" {
		if raw, ok := hit.Fields["_routing"]; ok {
			var routes []string
			if err := json.Unmarshal(raw, &routes); err != nil || len(routes) != 1 {
				return fmt.Errorf("esodm: invalid stored routing metadata")
			}
			hit.Routing = routes[0]
		}
	}

	if receiver, ok := any(&hit.Source).(MetadataReceiver); ok {
		initializeEmbeds(reflect.ValueOf(&hit.Source).Elem(), 0)
		receiver.SetDocumentMetadata(hit.Metadata)
	}
	if r.hooks.AfterRead != nil {
		return r.hooks.AfterRead(ctx, hit)
	}
	return nil
}
