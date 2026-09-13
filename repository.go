package esodm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"strconv"
	"strings"
)

// Metadata identifies a document and carries optimistic concurrency tokens.
type Metadata struct {
	// ID is the server document identifier. Explicit write IDs must be valid UTF-8 and 1..512 bytes.
	ID string `json:"_id"`
	// Index is the concrete index reported by Elasticsearch, which can differ from a repository
	// alias.
	Index string `json:"_index"`
	// Routing is the resolved route, or empty when unavailable or unused.
	Routing string `json:"_routing,omitempty"`
	// SeqNo is the nonnegative sequence number; nil means it was not returned. Use with PrimaryTerm
	// for optimistic concurrency.
	SeqNo *int64 `json:"_seq_no,omitempty"`
	// PrimaryTerm is the positive primary term; nil means it was not returned.
	PrimaryTerm *int64 `json:"_primary_term,omitempty"`
	// Version is the server document version; zero means unavailable.
	Version int64 `json:"_version,omitempty"`
}

// Hit pairs a decoded source document with search or get metadata.
type Hit[T any] struct {
	Metadata
	// Source is the decoded document. When _source is omitted or filtered it may be zero-valued or
	// incomplete; do not blindly replace the stored document with it.
	Source T `json:"_source"`
	// Score is the relevance score; nil means unscored or unavailable.
	Score *float64 `json:"_score,omitempty"`
	// Sort holds ordered raw JSON sort values for search_after. Nil means no sort values were
	// returned.
	Sort []json.RawMessage `json:"sort,omitempty"`
	// Highlight maps field names to highlighted fragments; nil means none were returned. Treat
	// fragments as untrusted text when rendering HTML.
	Highlight map[string][]string `json:"highlight,omitempty"`
	// InnerHits holds named raw inner-hit groups; use InnerHits to decode a group into its document
	// type.
	InnerHits map[string]json.RawMessage `json:"inner_hits,omitempty"`
	// Fields holds requested stored, runtime or doc-value fields as raw JSON; nil means none were
	// returned.
	Fields map[string]json.RawMessage `json:"fields,omitempty"`
}

// WriteResult describes an acknowledged document operation and shard outcomes.
type WriteResult struct {
	Metadata
	// Result is the server outcome, normally created, updated, deleted, not_found or noop. Unknown
	// future values are preserved.
	Result string `json:"result"`
	// Shards reports replication outcomes. A successful HTTP status does not imply every replica
	// succeeded.
	Shards struct {
		// Total is the number of shard copies targeted.
		Total int `json:"total"`
		// Successful is the number of successful shard copies.
		Successful int `json:"successful"`
		// Failed is the number of failed shard copies.
		Failed int `json:"failed"`
	} `json:"_shards"`
}

// WriteOptions configures routing, refresh, concurrency control and ingest.
type WriteOptions struct {
	// Routing must match the value used when the document was indexed.
	Routing string
	// Refresh accepts empty, false, true or wait_for.
	Refresh Refresh
	// IfSeqNo and IfPrimaryTerm must be supplied together for concurrency control.
	IfSeqNo *int64
	// IfPrimaryTerm must accompany IfSeqNo and be at least one; nil omits concurrency control only
	// when both tokens are nil.
	IfPrimaryTerm *int64
	// Pipeline applies only to index/create operations.
	Pipeline string
}

func (o WriteOptions) values() (url.Values, error) {
	v := url.Values{}
	if o.Refresh != "" && o.Refresh != "false" && o.Refresh != "true" && o.Refresh != "wait_for" {
		return nil, fmt.Errorf("%w: invalid refresh", ErrValidation)
	}
	if (o.IfSeqNo == nil) != (o.IfPrimaryTerm == nil) {
		return nil, fmt.Errorf("%w: both concurrency tokens required", ErrValidation)
	}
	if o.IfSeqNo != nil {
		if *o.IfSeqNo < 0 || *o.IfPrimaryTerm < 1 {
			return nil, fmt.Errorf("%w: invalid concurrency tokens", ErrValidation)
		}
		v.Set("if_seq_no", strconv.FormatInt(*o.IfSeqNo, 10))
		v.Set("if_primary_term", strconv.FormatInt(*o.IfPrimaryTerm, 10))
	}
	if o.Routing != "" {
		v.Set("routing", o.Routing)
	}
	if o.Refresh != "" {
		v.Set("refresh", string(o.Refresh))
	}
	if o.Pipeline != "" {
		v.Set("pipeline", o.Pipeline)
	}
	return v, nil
}

// Hooks execute synchronously. AfterWrite errors are marked as committed, so a
// caller can avoid retrying an already successful write. Hooks must be thread safe.
type Hooks[T any] struct {
	// Validate checks a prepared full document or upsert create candidate before sending; nil
	// disables it. Failures wrap ErrValidation.
	Validate func(context.Context, *T) error
	// BeforeWrite runs after stamping for full-document writes; nil disables it. An error prevents
	// sending the operation.
	BeforeWrite func(context.Context, Operation, *T) error
	// AfterWrite runs after a successful single or bulk item write; nil disables it. An error
	// becomes CommittedError, not permission to replay.
	AfterWrite func(context.Context, Operation, WriteResult) error
	// AfterRead runs after source decoding and metadata hydration on repository reads; nil disables
	// it. An error is returned with the affected result.
	AfterRead func(context.Context, *Hit[T]) error
	// BeforePatch receives a shallow copy of the patch before update or bulk update.
	// Nested caller-owned values must not be mutated by the hook.
	BeforePatch func(context.Context, string, Patch) error
	// BeforeDelete runs before single or bulk deletion.
	BeforeDelete func(context.Context, string) error
}

// CommittedError indicates that the write succeeded but its AfterWrite hook failed.
// Retrying the write can repeat an already committed operation.
type CommittedError struct {
	// Result describes the successful write whose completion hook failed.
	Result WriteResult
	// Cause is the non-nil completion hook error, exposed through Unwrap.
	Cause error
}

// Error returns a human-readable description of the failure.
func (e *CommittedError) Error() string {
	return "esodm: write committed, hook failed: " + e.Cause.Error()
}

// Unwrap returns the hook failure that followed the committed write.
func (e *CommittedError) Unwrap() error {
	return e.Cause
}

// Repository maps documents of T using an immutable schema and optional hooks.
type Repository[T any] struct {
	client *Client
	schema *Schema[T]
	hooks  Hooks[T]
}

// NewRepository binds a client and schema without creating cluster resources.
// Options are applied in order; a later hook configuration replaces earlier hooks.
func NewRepository[T any](client *Client, schema *Schema[T], options ...RepositoryOption[T]) (*Repository[T], error) {
	if client == nil || schema == nil {
		return nil, fmt.Errorf("%w: client/schema required", ErrValidation)
	}
	r := &Repository[T]{client: client, schema: schema}
	for _, option := range options {
		if option == nil || nilValue(option) {
			return nil, fmt.Errorf("%w: nil repository option", ErrValidation)
		}
		option.applyRepository(r)
	}
	return r, nil
}

// Schema returns the repository's immutable schema.
func (r *Repository[T]) Schema() *Schema[T] {
	return r.schema
}

// EnsureIndex creates the index if absent; it does not migrate existing mappings.
func (r *Repository[T]) EnsureIndex(ctx context.Context) error {
	if err := validateIndexName(r.Index()); err != nil {
		return err
	}
	err := r.client.Do(ctx, "HEAD", "/"+segment(r.schema.index), nil, nil, nil)
	if err == nil {
		return nil
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	body := map[string]any{"mappings": r.schema.mapping}
	if string(r.schema.settings) != "null" {
		body["settings"] = r.schema.settings
	}
	err = r.client.Admin().ack(ctx, "PUT", "/"+segment(r.schema.index), body)
	var ee *Error
	if errors.As(err, &ee) && ee.Type == "resource_already_exists_exception" {
		return nil
	}
	return err
}

// Get loads one document and invokes AfterRead. Missing documents return ErrNotFound.
func (r *Repository[T]) Get(ctx context.Context, id, routing string) (Hit[T], error) {
	return r.GetWith(ctx, id, ReadOptions{Routing: routing})
}

// GetWith loads a document with explicit routing, source and consistency options.
func (r *Repository[T]) GetWith(ctx context.Context, id string, options ReadOptions) (Hit[T], error) {
	if err := validateIndexName(r.Index()); err != nil {
		return Hit[T]{}, err
	}
	routing := options.Routing
	var out struct {
		Hit[T]
		Found bool `json:"found"`
	}
	if err := validateID(id); err != nil {
		return out.Hit, err
	}
	if err := r.schema.requireJoinRouting(routing); err != nil {
		return out.Hit, err
	}
	q := options.values()
	if routing != "" {
		q.Set("routing", routing)
	}
	if r.client.version.Major == 9 && r.schema.hasVectors {
		q.Set("_source_exclude_vectors", "false")
	}
	err := r.client.Do(ctx, "GET", "/"+segment(r.schema.index)+"/_doc/"+segment(id), q, nil, &out)
	if err == nil && !out.Found {
		err = ErrNotFound
	}
	if err == nil {
		out.Routing = routing
		err = r.afterRead(ctx, &out.Hit)
	}
	return out.Hit, err
}

// Exists reports whether a document exists using an HTTP HEAD request.
func (r *Repository[T]) Exists(ctx context.Context, id, routing string) (bool, error) {
	return r.ExistsWith(ctx, id, ReadOptions{Routing: routing})
}

// ExistsWith checks document existence with routing and consistency options.
// Source filtering has no effect on a HEAD request.
func (r *Repository[T]) ExistsWith(ctx context.Context, id string, options ReadOptions) (bool, error) {
	if err := validateIndexName(r.Index()); err != nil {
		return false, err
	}
	routing := options.Routing
	if err := r.schema.requireJoinRouting(routing); err != nil {
		return false, err
	}
	if err := validateID(id); err != nil {
		return false, err
	}
	q := options.values()
	if routing != "" {
		q.Set("routing", routing)
	}
	err := r.client.Do(ctx, "HEAD", "/"+segment(r.schema.index)+"/_doc/"+segment(id), q, nil, nil)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

// Create inserts a document only if its ID is absent, otherwise returning ErrConflict.
func (r *Repository[T]) Create(ctx context.Context, id string, doc T, opts ...WriteOption) (WriteResult, error) {
	options, err := writeOptions(opts)
	if err != nil {
		return WriteResult{}, err
	}
	return r.write(ctx, "create", id, doc, options)
}

// Replace indexes the complete source, creating the document if absent.
// Supply both concurrency tokens to protect a read-modify-write operation.
func (r *Repository[T]) Replace(ctx context.Context, id string, doc T, opts ...WriteOption) (WriteResult, error) {
	options, err := writeOptions(opts)
	if err != nil {
		return WriteResult{}, err
	}
	return r.write(ctx, "index", id, doc, options)
}
func (r *Repository[T]) write(ctx context.Context, operation Operation, id string, doc T, options WriteOptions) (WriteResult, error) {
	if err := validateIndexName(r.Index()); err != nil {
		return WriteResult{}, err
	}
	var result WriteResult
	if operation != "insert" {
		if err := validateID(id); err != nil {
			return result, err
		}
	}
	options = modelOptions(&doc, options)
	q, err := options.values()
	if err != nil {
		return result, err
	}
	if (operation == "create" || operation == "insert") && options.IfSeqNo != nil {
		return result, fmt.Errorf("%w: create cannot use concurrency tokens", ErrValidation)
	}
	r.stamp(&doc, operation)
	if r.hooks.BeforeWrite != nil {
		if err = r.hooks.BeforeWrite(ctx, operation, &doc); err != nil {
			return result, err
		}
	}
	if r.hooks.Validate != nil {
		if err = r.hooks.Validate(ctx, &doc); err != nil {
			return result, fmt.Errorf("%w: %w", ErrValidation, err)
		}
	}
	data, err := json.Marshal(doc)
	if err != nil {
		return result, fmt.Errorf("%w: %w", ErrValidation, err)
	}
	if err = r.schema.validateJoin(data, options.Routing); err != nil {
		return result, err
	}
	endpoint := "_doc"
	if operation == "create" {
		endpoint = "_create"
	}
	method, path := "PUT", "/"+segment(r.schema.index)+"/"+endpoint+"/"+segment(id)
	if operation == "insert" {
		method = "POST"
		path = "/" + segment(r.schema.index) + "/_doc"
		q.Set("op_type", "create")
	}
	err = r.client.request(ctx, method, path, q, data, "application/json", &result)
	result.Routing = options.Routing
	return result, r.afterWrite(ctx, operation, result, err)
}
func (r *Repository[T]) afterWrite(ctx context.Context, op Operation, result WriteResult, err error) error {
	if err == nil && r.hooks.AfterWrite != nil {
		if e := r.hooks.AfterWrite(ctx, op, result); e != nil {
			return &CommittedError{result, e}
		}
	}
	return err
}

// Patch contains only fields to change. Missing keys are untouched; nil writes
// JSON null. This deliberately does not use a zero-valued document as a patch.
type Patch map[string]any

// Update merges explicitly supplied fields into an existing document.
// Full-document validation hooks are not run for a partial patch.
func (r *Repository[T]) Update(ctx context.Context, id string, patch Patch, opts ...WriteOption) (WriteResult, error) {
	if err := validateIndexName(r.Index()); err != nil {
		return WriteResult{}, err
	}

	options, err := writeOptions(opts)
	if err != nil {
		return WriteResult{}, err
	}
	prepared, err := r.preparePatch(ctx, id, patch)
	if err != nil {
		return WriteResult{}, err
	}
	return r.update(ctx, id, map[string]any{"doc": prepared}, options)
}

// Upsert applies patch when the document exists or inserts create when absent.
// The create document is validated before sending the request.
func (r *Repository[T]) Upsert(ctx context.Context, id string, patch Patch, create T, opts ...WriteOption) (WriteResult, error) {
	if err := validateIndexName(r.Index()); err != nil {
		return WriteResult{}, err
	}

	options, err := writeOptions(opts)
	if err != nil {
		return WriteResult{}, err
	}
	options = modelOptions(&create, options)
	r.stamp(&create, "create")
	if r.hooks.Validate != nil {
		if err := r.hooks.Validate(ctx, &create); err != nil {
			return WriteResult{}, fmt.Errorf("%w: %w", ErrValidation, err)
		}
	}
	data, err := json.Marshal(create)
	if err != nil {
		return WriteResult{}, fmt.Errorf("%w: %w", ErrValidation, err)
	}
	if err := r.schema.validateJoin(data, options.Routing); err != nil {
		return WriteResult{}, err
	}
	prepared, err := r.preparePatch(ctx, id, patch)
	if err != nil {
		return WriteResult{}, err
	}
	return r.update(ctx, id, map[string]any{"doc": prepared, "upsert": json.RawMessage(data)}, options)
}
func (r *Repository[T]) update(ctx context.Context, id string, body any, options WriteOptions) (WriteResult, error) {
	if err := validateIndexName(r.Index()); err != nil {
		return WriteResult{}, err
	}
	var out WriteResult
	if err := validateID(id); err != nil {
		return out, err
	}
	q, err := options.values()
	if err != nil {
		return out, err
	}
	if options.Pipeline != "" {
		return out, fmt.Errorf("%w: update does not accept a pipeline", ErrValidation)
	}
	if err = r.schema.requireJoinRouting(options.Routing); err != nil {
		return out, err
	}
	err = r.client.Do(ctx, "POST", "/"+segment(r.schema.index)+"/_update/"+segment(id), q, body, &out)
	out.Routing = options.Routing
	return out, r.afterWrite(ctx, "update", out, err)
}

// Delete removes a document, optionally checking optimistic concurrency tokens.
func (r *Repository[T]) Delete(ctx context.Context, id string, opts ...WriteOption) (WriteResult, error) {
	if err := validateIndexName(r.Index()); err != nil {
		return WriteResult{}, err
	}
	options, err := writeOptions(opts)
	if err != nil {
		return WriteResult{}, err
	}
	var out WriteResult
	if err := validateID(id); err != nil {
		return out, err
	}
	q, err := options.values()
	if err != nil {
		return out, err
	}
	if options.Pipeline != "" {
		return out, fmt.Errorf("%w: delete does not accept a pipeline", ErrValidation)
	}
	if err = r.schema.requireJoinRouting(options.Routing); err != nil {
		return out, err
	}
	if r.hooks.BeforeDelete != nil {
		if err = r.hooks.BeforeDelete(ctx, id); err != nil {
			return out, err
		}
	}
	err = r.client.Do(ctx, "DELETE", "/"+segment(r.schema.index)+"/_doc/"+segment(id), q, nil, &out)
	out.Routing = options.Routing
	return out, r.afterWrite(ctx, "delete", out, err)
}

// CountOptions selects routing and shard preference for a count request.
type CountOptions struct {
	// Routing selects shard routes; nil or empty omits routing. Multiple values are comma-joined.
	Routing []string
	// Preference selects a server shard preference or custom session string; empty omits it.
	Preference string
}

// Count returns the matching document count and rejects partial shard results.
func (r *Repository[T]) Count(ctx context.Context, q Query) (int64, error) {
	return r.CountWith(ctx, q, CountOptions{})
}

// CountWith counts matching documents with explicit request options.
func (r *Repository[T]) CountWith(ctx context.Context, q Query, options CountOptions) (int64, error) {
	var out struct {
		Count  int64 `json:"count"`
		Shards struct {
			Failed int `json:"failed"`
		} `json:"_shards"`
	}
	params := url.Values{}
	if len(options.Routing) > 0 {
		params.Set("routing", strings.Join(options.Routing, ","))
	}
	if options.Preference != "" {
		params.Set("preference", options.Preference)
	}
	err := r.client.Do(ctx, "POST", "/"+segment(r.schema.index)+"/_count", params, map[string]any{"query": q}, &out)
	if err == nil && out.Shards.Failed > 0 {
		err = &PartialSearchError{FailedShards: out.Shards.Failed}
	}
	return out.Count, err
}

// Conditional returns independent concurrency tokens and routing from this metadata.
func (m Metadata) Conditional() WriteOptions {
	out := WriteOptions{Routing: m.Routing}
	if m.SeqNo != nil {
		value := *m.SeqNo
		out.IfSeqNo = &value
	}
	if m.PrimaryTerm != nil {
		value := *m.PrimaryTerm
		out.IfPrimaryTerm = &value
	}
	return out
}

// Index returns the repository index or alias name.
func (r *Repository[T]) Index() string { return r.schema.index }

// GetByID loads an unrouted document. Use Get when explicit routing is required.
func (r *Repository[T]) GetByID(ctx context.Context, id string) (Hit[T], error) {
	return r.Get(ctx, id, "")
}

func (r *Repository[T]) preparePatch(ctx context.Context, id string, patch Patch) (Patch, error) {
	if err := validateID(id); err != nil {
		return nil, err
	}
	copy := maps.Clone(patch)
	if copy == nil {
		copy = Patch{}
	}
	if err := r.stampPatch(copy); err != nil {
		return nil, err
	}
	if r.hooks.BeforePatch != nil {
		if err := r.hooks.BeforePatch(ctx, id, copy); err != nil {
			return nil, err
		}
	}
	return copy, nil
}
