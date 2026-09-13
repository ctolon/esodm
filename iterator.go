package esodm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"net/url"
	"time"
)

// Iterator owns one PIT. It is not safe for concurrent use. Always defer Close,
// even when stopping before exhaustion. Cancellation and exhaustion also close it.
type Iterator[T any] struct {
	repo      *Repository[T]
	ctx       context.Context
	search    Search
	pit       string
	keepAlive string
	page      []Hit[T]
	position  int
	current   Hit[T]
	after     []json.RawMessage
	err       error
	closed    bool
	started   bool
}

// Iterate opens a PIT and returns an iterator that owns pagination and cleanup.
// An empty keep-alive selects one minute; pageSize must be between 1 and 10,000.
func (r *Repository[T]) Iterate(ctx context.Context, search Search, pageSize int, keepAlive string) (*Iterator[T], error) {
	if pageSize < 1 || pageSize > 10000 {
		return nil, fmt.Errorf("%w: page size must be 1..10000", ErrValidation)
	}
	if keepAlive == "" {
		keepAlive = "1m"
	}
	duration, err := time.ParseDuration(keepAlive)
	if err != nil || duration <= 0 {
		return nil, fmt.Errorf("%w: invalid PIT keep-alive", ErrValidation)
	}
	for _, k := range []string{"from", "search_after", "pit", "collapse", "retriever"} {
		if _, ok := search.fields[k]; ok {
			return nil, fmt.Errorf("%w: iterator owns pagination; %s is not supported", ErrValidation, k)
		}
	}
	if _, err = json.Marshal(search); err != nil {
		return nil, err
	}
	var out struct {
		ID string `json:"id"`
	}
	pitParams := url.Values{"keep_alive": {keepAlive}}
	for _, k := range []string{"routing", "preference"} {
		if v := search.params.Get(k); v != "" {
			pitParams.Set(k, v)
		}
	}
	params := search.Params()
	params.Del("routing")
	params.Del("preference")
	search.params = params
	if err = r.client.Do(ctx, "POST", "/"+segment(r.schema.index)+"/_pit", pitParams, nil, &out); err != nil {
		return nil, err
	}
	if out.ID == "" {
		return nil, errors.New("esodm: empty PIT ID")
	}
	// Elasticsearch implicitly adds _shard_doc to an explicit PIT sort. Make it
	// explicit for the default sort so every hit yields a stable search_after key.
	if _, ok := search.fields["sort"]; !ok {
		search = search.Sort(Sort{Field: "_shard_doc"})
	}
	return &Iterator[T]{repo: r, ctx: ctx, search: search.Size(pageSize), pit: out.ID, keepAlive: keepAlive}, nil
}

// Next advances to the next hit, closing the PIT on exhaustion or failure.
// Call Err after it returns false.
func (it *Iterator[T]) Next() bool {
	if it.closed || it.err != nil {
		return false
	}
	if err := it.ctx.Err(); err != nil {
		it.err = err
		_ = it.Close()
		return false
	}
	if it.position >= len(it.page) {
		s := it.search.PIT(it.pit, it.keepAlive)
		if it.started {
			s = s.SearchAfter(it.after...)
		}
		result, err := it.repo.Search(it.ctx, s)
		if result.PITID != "" {
			it.pit = result.PITID
		}
		if err != nil {
			it.err = err
			_ = it.Close()
			return false
		}
		it.page = result.Hits.Hits
		it.position = 0
		it.started = true
		if len(it.page) == 0 {
			_ = it.Close()
			return false
		}
		last := it.page[len(it.page)-1].Sort
		if len(last) == 0 {
			it.err = errors.New("esodm: PIT hit has no sort values")
			_ = it.Close()
			return false
		}
		before, _ := json.Marshal(it.after)
		after, _ := json.Marshal(last)
		if string(before) == string(after) {
			it.err = errors.New("esodm: pagination did not advance")
			_ = it.Close()
			return false
		}
		it.after = last
	}
	it.current = it.page[it.position]
	it.position++
	return true
}

// Hit returns the current hit; it is valid after Next returns true.
func (it *Iterator[T]) Hit() Hit[T] {
	return it.current
}

// Err returns the iteration or automatic PIT cleanup error, if any.
func (it *Iterator[T]) Err() error {
	return it.err
}

// Close releases the PIT using an independent timeout and is safe to call repeatedly.
func (it *Iterator[T]) Close() error {
	if it.closed {
		return it.err
	}
	it.closed = true
	ctx, cancel := context.WithTimeout(context.WithoutCancel(it.ctx), 5*time.Second)
	defer cancel()
	err := it.repo.client.Do(ctx, "DELETE", "/_pit", nil, map[string]string{"id": it.pit}, nil)
	if errors.Is(err, ErrNotFound) {
		err = nil
	}
	it.err = errors.Join(it.err, err)
	return it.err
}

// Cursor is an opaque, bounded serialization helper, not an authorization token.
// Callers exposing cursors to untrusted users must authenticate them separately.
type Cursor struct {
	// PIT is the required live server PIT identifier. A cursor does not keep it alive automatically.
	PIT string `json:"pit"`
	// Sort contains the last delivered hit sort values, required and in sort order. Values remain
	// raw JSON to preserve numeric precision.
	Sort []json.RawMessage `json:"sort"`
}

// EncodeCursor encodes a bounded cursor as URL-safe base64 JSON.
// It is not signed or encrypted; add application authentication if needed.
func EncodeCursor(c Cursor) (string, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	if len(b) > 16384 || c.PIT == "" || len(c.Sort) == 0 {
		return "", fmt.Errorf("%w: invalid cursor", ErrValidation)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// DecodeCursor decodes a bounded cursor and preserves numeric sort values.
func DecodeCursor(s string) (Cursor, error) {
	var c Cursor
	if len(s) > 22000 {
		return c, fmt.Errorf("%w: cursor too large", ErrValidation)
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return c, fmt.Errorf("%w: invalid cursor encoding", ErrValidation)
	}
	if err = json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("%w: invalid cursor JSON", ErrValidation)
	}
	if _, err = EncodeCursor(c); err != nil {
		return Cursor{}, err
	}
	return c, nil
}

// Each yields remaining hits and closes the PIT, including on early termination.
// Cleanup errors are available through Err when the consumer stops early.
func (it *Iterator[T]) Each() iter.Seq2[Hit[T], error] {
	return func(yield func(Hit[T], error) bool) {
		defer func() { _ = it.Close() }()
		for it.Next() {
			if !yield(it.Hit(), nil) {
				return
			}
		}
		if err := it.Close(); err != nil {
			yield(Hit[T]{}, err)
		}
	}
}

// Cursor snapshots the latest PIT and the last emitted hit, never unread page hits.
// The iterator retains ownership until Detach succeeds.
func (it *Iterator[T]) Cursor() (Cursor, error) {
	if it.closed || len(it.current.Sort) == 0 {
		return Cursor{}, fmt.Errorf("%w: no active cursor", ErrValidation)
	}
	c := Cursor{PIT: it.pit, Sort: make([]json.RawMessage, len(it.current.Sort))}
	for i, v := range it.current.Sort {
		c.Sort[i] = append(json.RawMessage(nil), v...)
	}
	return c, nil
}

// Detach transfers PIT cleanup responsibility to the caller and stops iteration.
// ResumeIterator takes ownership again; otherwise close the PIT before expiry.
func (it *Iterator[T]) Detach() (Cursor, error) {
	c, err := it.Cursor()
	if err != nil {
		return c, err
	}
	it.closed = true
	return c, nil
}

// ResumeIterator owns an existing PIT. Supply the same query and sort used to
// produce the cursor; changing either invalidates pagination semantics.
func (r *Repository[T]) ResumeIterator(ctx context.Context, search Search, cursor Cursor, pageSize int, keepAlive string) (*Iterator[T], error) {
	if _, err := EncodeCursor(cursor); err != nil {
		return nil, err
	}
	if pageSize < 1 || pageSize > 10000 {
		return nil, fmt.Errorf("%w: page size must be 1..10000", ErrValidation)
	}
	if keepAlive == "" {
		keepAlive = "1m"
	}
	if d, err := time.ParseDuration(keepAlive); err != nil || d <= 0 {
		return nil, fmt.Errorf("%w: invalid PIT keep-alive", ErrValidation)
	}
	for _, k := range []string{"from", "search_after", "pit", "collapse", "retriever"} {
		if _, ok := search.fields[k]; ok {
			return nil, fmt.Errorf("%w: iterator owns %s", ErrValidation, k)
		}
	}
	if err := search.PIT(cursor.PIT, keepAlive).validatePITParams(); err != nil {
		return nil, err
	}
	if _, err := json.Marshal(search); err != nil {
		return nil, err
	}
	if _, ok := search.fields["sort"]; !ok {
		search = search.Sort(Sort{Field: "_shard_doc"})
	}
	after := make([]json.RawMessage, len(cursor.Sort))
	for i, v := range cursor.Sort {
		after[i] = append(json.RawMessage(nil), v...)
	}
	return &Iterator[T]{repo: r, ctx: ctx, search: search.Size(pageSize), pit: cursor.PIT, keepAlive: keepAlive, after: after, started: true}, nil
}
