package esodm

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
)

// Ref stores a document reference, not a copy of the target. T denotes its Go
// source type. Routing is required when the referenced document is routed.
type Ref[T any] struct {
	// Index is the target local index or alias, required independently of T.
	Index string `json:"index"`
	// ID is the target document ID, valid UTF-8 and 1..512 bytes.
	ID string `json:"id"`
	// Routing must match the target indexing route; empty omits it.
	Routing string `json:"routing,omitempty"`
}

// ReferenceResult associates a reference with its loaded hit or individual error.
type ReferenceResult[T any] struct {
	// Ref is the original input reference.
	Ref Ref[T]
	// Hit contains the decoded target when Found is true. Duplicate references may share nested maps
	// and pointers.
	Hit Hit[T]
	// Found distinguishes an existing document from a missing or failed item.
	Found bool
	// Err is an individual server or repository read-hook error. A missing document has Found=false
	// and Err=nil.
	Err error
}

// RelationField is an optional typed descriptor for explicit reference loading.
type RelationField[T any] struct {
	Field[Ref[T]]
}

// Relation creates a typed field descriptor for explicit reference loading.
func Relation[T any](name string) RelationField[T] {
	return RelationField[T]{NewField[Ref[T]](name)}
}

// Load resolves references using the client and preserves input order.
func (f RelationField[T]) Load(ctx context.Context, client *Client, refs ...Ref[T]) ([]ReferenceResult[T], error) {
	return Load(ctx, client, refs)
}

type refKey struct {
	index, id, routing string
}

func key[T any](r Ref[T]) refKey {
	return refKey{r.Index, r.ID, r.Routing}
}

// Load batches and deduplicates references. Results retain input order, including
// duplicate and missing references. Per-document errors are retained in results.
func Load[T any](ctx context.Context, client *Client, refs []Ref[T]) ([]ReferenceResult[T], error) {
	return loadWith(ctx, client, refs, ReadOptions{})
}
func loadWith[T any](ctx context.Context, client *Client, refs []Ref[T], options ReadOptions) ([]ReferenceResult[T], error) {
	if client == nil {
		return nil, fmt.Errorf("%w: nil client", ErrValidation)
	}
	out := make([]ReferenceResult[T], len(refs))
	unique := []Ref[T]{}
	positions := map[refKey][]int{}
	for i, ref := range refs {
		if err := validateName(ref.Index); err != nil {
			return nil, err
		}
		if err := validateID(ref.ID); err != nil {
			return nil, err
		}
		k := key(ref)
		if len(positions[k]) == 0 {
			unique = append(unique, ref)
		}
		positions[k] = append(positions[k], i)
	}
	for start := 0; start < len(unique); start += 500 {
		end := min(start+500, len(unique))
		batch := unique[start:end]
		docs := make([]map[string]any, len(batch))
		for i, ref := range batch {
			docs[i] = map[string]any{"_index": ref.Index, "_id": ref.ID}
			if ref.Routing != "" {
				docs[i]["routing"] = ref.Routing
			}
		}
		var response struct {
			Docs []struct {
				Hit[T]
				Found  bool              `json:"found"`
				Error  *types.ErrorCause `json:"error"`
				Status int               `json:"status"`
			} `json:"docs"`
		}
		q := options.values()
		q.Del("routing")
		if client.version.Major == 9 {
			q.Set("_source_exclude_vectors", "false")
		}
		if err := client.Do(ctx, "POST", "/_mget", q, map[string]any{"docs": docs}, &response); err != nil {
			return nil, err
		}
		if len(response.Docs) != len(batch) {
			return nil, fmt.Errorf("esodm: mget response count mismatch")
		}
		for i, doc := range response.Docs {
			ref := batch[i]
			result := ReferenceResult[T]{Ref: ref, Hit: doc.Hit, Found: doc.Found}
			result.Hit.Routing = ref.Routing
			if doc.Error != nil {
				result.Err = &Error{Status: doc.Status, Type: doc.Error.Type, Reason: errorReason(doc.Error), Cause: *doc.Error}
				result.Found = false
			}
			for _, pos := range positions[key(ref)] {
				out[pos] = result
			}
		}
	}
	return out, nil
}

// MGet loads document IDs in input order, preserving missing and failed items.
func (r *Repository[T]) MGet(ctx context.Context, ids []string, routing string) ([]ReferenceResult[T], error) {
	return r.MGetWith(ctx, ids, ReadOptions{Routing: routing})
}

// MGetWith loads IDs in input order with explicit routing and read options.
func (r *Repository[T]) MGetWith(ctx context.Context, ids []string, options ReadOptions) ([]ReferenceResult[T], error) {
	if err := validateIndexName(r.Index()); err != nil {
		return nil, err
	}
	routing := options.Routing
	if err := r.schema.requireJoinRouting(routing); err != nil {
		return nil, err
	}
	refs := make([]Ref[T], len(ids))
	for i, id := range ids {
		refs[i] = Ref[T]{r.schema.index, id, routing}
	}
	out, err := loadWith(ctx, r.client, refs, options)
	if err == nil {
		for i := range out {
			if out[i].Found && out[i].Err == nil {
				out[i].Err = r.afterRead(ctx, &out[i].Hit)
			}
		}
	}
	return out, err
}

// PreloadOptions bounds graph traversal depth and number of distinct documents.
type PreloadOptions struct {
	// MaxDepth bounds breadth-first levels including roots, 1..32; zero selects three. Unvisited
	// references at the limit set Graph.Truncated.
	MaxDepth int
	// MaxDocuments limits distinct references including missing documents; zero selects 10000 and
	// negative values are invalid. Exceeding it returns the partial graph and ErrValidation.
	MaxDocuments int
}

// Graph contains loaded documents and missing references from explicit preloading.
type Graph[T any] struct {
	// Nodes holds distinct loaded references in breadth-first order, including missing or failed
	// items.
	Nodes []ReferenceResult[T]
	// Truncated reports unvisited references beyond MaxDepth; it does not mean MaxDocuments was
	// exceeded.
	Truncated bool
}

// Preload traverses explicitly supplied references breadth-first. It never mutates
// source documents. Repeated references/cycles are loaded once. Use separate calls
// for heterogeneous target types. Truncated signals the depth limit was reached.
func Preload[T any](ctx context.Context, client *Client, roots []Ref[T], children func(T) []Ref[T], options PreloadOptions) (Graph[T], error) {
	var graph Graph[T]
	if options.MaxDepth == 0 {
		options.MaxDepth = 3
	}
	if options.MaxDocuments == 0 {
		options.MaxDocuments = 10000
	}
	if options.MaxDepth < 1 || options.MaxDepth > 32 || options.MaxDocuments < 1 || children == nil {
		return graph, fmt.Errorf("%w: invalid preload options", ErrValidation)
	}
	seen := map[refKey]bool{}
	pending := append([]Ref[T](nil), roots...)
	for depth := 0; len(pending) > 0; depth++ {
		if err := ctx.Err(); err != nil {
			return graph, err
		}
		batch := []Ref[T]{}
		for _, ref := range pending {
			k := key(ref)
			if !seen[k] {
				seen[k] = true
				batch = append(batch, ref)
			}
		}
		if len(batch) == 0 {
			break
		}
		if depth >= options.MaxDepth {
			graph.Truncated = true
			break
		}
		if len(graph.Nodes)+len(batch) > options.MaxDocuments {
			return graph, fmt.Errorf("%w: preload document limit exceeded", ErrValidation)
		}
		results, err := Load(ctx, client, batch)
		if err != nil {
			return graph, err
		}
		graph.Nodes = append(graph.Nodes, results...)
		pending = nil
		for _, result := range results {
			if result.Found && result.Err == nil {
				pending = append(pending, children(result.Hit.Source)...)
			}
		}
	}
	return graph, nil
}

// Join identifies a parent or child relation; children must include a parent ID
// and use consistent routing for every document operation.
type Join struct {
	// Name is a declared parent or child relation name.
	Name string `json:"name"`
	// Parent is required for child relations and must be a valid document ID; root parent documents
	// leave it empty.
	Parent string `json:"parent,omitempty"`
}

// joinInfo is compiled once from the immutable schema.
type joinInfo struct {
	field    string
	parents  map[string]bool
	children map[string]bool
}

func compileJoin(props map[string]FieldMapping) (*joinInfo, error) {
	var out *joinInfo
	for name, f := range props {
		if f.Type != "join" {
			continue
		}
		if out != nil {
			return nil, fmt.Errorf("%w: multiple join fields", ErrValidation)
		}
		out = &joinInfo{field: name, parents: map[string]bool{}, children: map[string]bool{}}
		for parent, raw := range f.Relations {
			if parent == "" {
				return nil, fmt.Errorf("%w: empty join parent", ErrValidation)
			}
			out.parents[parent] = true
			var children []string
			switch value := raw.(type) {
			case string:
				children = []string{value}
			case []string:
				children = value
			case []any:
				for _, child := range value {
					name, ok := child.(string)
					if !ok {
						return nil, fmt.Errorf("%w: invalid join child", ErrValidation)
					}
					children = append(children, name)
				}
			default:
				return nil, fmt.Errorf("%w: invalid join relations", ErrValidation)
			}
			if len(children) == 0 {
				return nil, fmt.Errorf("%w: empty join children", ErrValidation)
			}
			for _, child := range children {
				if child == "" || child == parent {
					return nil, fmt.Errorf("%w: invalid join child", ErrValidation)
				}
				out.children[child] = true
			}
		}
		if len(out.parents) == 0 {
			return nil, fmt.Errorf("%w: join relations required", ErrValidation)
		}
	}
	return out, nil
}

func (s *Schema[T]) requireJoinRouting(routing string) error {
	if s.join != nil && routing == "" {
		return fmt.Errorf("%w: explicit routing required for join index operations", ErrValidation)
	}
	return nil
}

func (s *Schema[T]) validateJoin(data []byte, routing string) error {
	if s.join == nil {
		return nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return fmt.Errorf("%w: %w", ErrValidation, err)
	}
	var relation Join
	if err := json.Unmarshal(obj[s.join.field], &relation); err != nil {
		if err := json.Unmarshal(obj[s.join.field], &relation.Name); err != nil {
			return fmt.Errorf("%w: invalid join value: %w", ErrValidation, err)
		}
	}
	if relation.Name == "" {
		return fmt.Errorf("%w: join name required", ErrValidation)
	}
	// A relation that is itself a child still requires its parent, even if it
	// also has children in a multi-level join mapping.
	if s.join.parents[relation.Name] && !s.join.children[relation.Name] && relation.Parent == "" {
		return nil
	}
	if !s.join.children[relation.Name] || relation.Parent == "" || routing == "" {
		return fmt.Errorf("%w: child requires declared relation, parent ID and routing", ErrValidation)
	}
	return validateID(relation.Parent)
}
