package esodm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v9/typedapi/core/deletebyquery"
	"github.com/elastic/go-elasticsearch/v9/typedapi/core/update"
	"github.com/elastic/go-elasticsearch/v9/typedapi/core/updatebyquery"
	taskcancel "github.com/elastic/go-elasticsearch/v9/typedapi/tasks/cancel"
	taskget "github.com/elastic/go-elasticsearch/v9/typedapi/tasks/get"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types/enums/conflicts"
)

// UpdateOptions adds server-side optimistic conflict retries to ordinary write options.
// RetryOnConflict cannot be combined with explicit concurrency tokens.
type UpdateOptions struct {
	WriteOptions
	// RetryOnConflict is a nonnegative server-side retry count; zero omits it. Positive values
	// cannot be combined with explicit concurrency tokens.
	RetryOnConflict int
}

// UpdateResult includes optional updated source requested through update.Request.Source_.
type UpdateResult[T any] struct {
	WriteResult
	// Get is the optional updated source response; nil means it was not returned. It is decoded
	// without repository AfterRead hooks.
	Get *Hit[T] `json:"get,omitempty"`
}

// UpdateWith executes an official update body against this repository's index.
// Doc patches run BeforePatch and audit stamping; scripts execute on the server
// and must implement their own audit/validation policy. AfterWrite runs on success.
func (r *Repository[T]) UpdateWith(ctx context.Context, id string, request *update.Request, options UpdateOptions) (UpdateResult[T], error) {
	if err := validateIndexName(r.Index()); err != nil {
		return UpdateResult[T]{}, err
	}
	var out UpdateResult[T]
	if request == nil {
		return out, fmt.Errorf("%w: update request required", ErrValidation)
	}
	if err := validateID(id); err != nil {
		return out, err
	}
	if options.RetryOnConflict < 0 || options.RetryOnConflict > 0 && options.IfSeqNo != nil || options.Pipeline != "" {
		return out, fmt.Errorf("%w: invalid update options", ErrValidation)
	}
	q, err := options.values()
	if err != nil {
		return out, err
	}
	if err = r.schema.requireJoinRouting(options.Routing); err != nil {
		return out, err
	}
	req := *request
	if len(req.Doc) > 0 && req.Script != nil {
		return out, fmt.Errorf("%w: doc and script are mutually exclusive", ErrValidation)
	}
	if len(req.Doc) == 0 && req.Script == nil {
		return out, fmt.Errorf("%w: doc or script required", ErrValidation)
	}
	if len(req.Doc) > 0 {
		var patch Patch
		dec := json.NewDecoder(bytes.NewReader(req.Doc))
		dec.UseNumber()
		if err = dec.Decode(&patch); err != nil {
			return out, fmt.Errorf("%w: %w", ErrValidation, err)
		}
		if !json.Valid(req.Doc) || patch == nil {
			return out, fmt.Errorf("%w: doc must be an object", ErrValidation)
		}
		patch, err = r.preparePatch(ctx, id, patch)
		if err != nil {
			return out, err
		}
		req.Doc, err = json.Marshal(patch)
		if err != nil {
			return out, err
		}
	}
	// Upsert candidates are full documents and must satisfy repository validation.
	if req.DocAsUpsert != nil && *req.DocAsUpsert {
		if req.Script != nil || len(req.Upsert) > 0 {
			return out, fmt.Errorf("%w: ambiguous upsert", ErrValidation)
		}
		req.Upsert = req.Doc
		req.DocAsUpsert = nil
	}
	if len(req.Upsert) > 0 {
		var doc T
		if err = json.Unmarshal(req.Upsert, &doc); err != nil {
			return out, fmt.Errorf("%w: %w", ErrValidation, err)
		}
		r.stamp(&doc, "create")
		if r.hooks.Validate != nil {
			if err = r.hooks.Validate(ctx, &doc); err != nil {
				return out, fmt.Errorf("%w: %w", ErrValidation, err)
			}
		}
		req.Upsert, err = json.Marshal(doc)
		if err != nil {
			return out, err
		}
		if err = r.schema.validateJoin(req.Upsert, options.Routing); err != nil {
			return out, err
		}
	}
	if options.RetryOnConflict > 0 {
		q.Set("retry_on_conflict", strconv.Itoa(options.RetryOnConflict))
	}
	err = r.client.Do(ctx, "POST", "/"+segment(r.schema.index)+"/_update/"+segment(id), q, &req, &out)
	out.Routing = options.Routing
	return out, r.afterWrite(ctx, "update", out.WriteResult, err)
}

// ByQueryOptions controls batch-by-query execution. Async returns a server task ID.
// Conflicts defaults to abort. No per-document hooks or audit stamping run.
type ByQueryOptions struct {
	// Routing limits shard routes; nil or empty omits it.
	Routing []string
	// Refresh refreshes affected shards when true; false leaves normal refresh scheduling. By-query
	// APIs do not accept wait_for.
	Refresh bool
	// Async returns a task ID instead of waiting when true; false waits for completion.
	Async bool
	// Conflicts accepts abort or proceed; an empty Name selects the server default. Proceed
	// tolerates version conflicts but does not make other failures successful.
	Conflicts conflicts.Conflicts
}

// IncompleteOperationError indicates server-reported partial work, which is not rolled back.
type IncompleteOperationError struct {
	// Operation identifies the incomplete server operation, such as reindex or update_by_query.
	Operation string
}

// Error describes a non-atomic operation's incomplete outcome.
func (e *IncompleteOperationError) Error() string {
	return "esodm: " + e.Operation + " incomplete; inspect response"
}
func (o ByQueryOptions) validate() error {
	if o.Conflicts.Name != "" && o.Conflicts.Name != "abort" && o.Conflicts.Name != "proceed" {
		return fmt.Errorf("%w: invalid conflicts policy", ErrValidation)
	}
	return nil
}

// UpdateByQuery executes the official request, preserving its complete response.
// An explicit query is required; use MatchAll intentionally for all documents.
func (r *Repository[T]) UpdateByQuery(ctx context.Context, req *updatebyquery.Request, o ByQueryOptions) (updatebyquery.Response, error) {
	if err := validateIndexName(r.Index()); err != nil {
		return updatebyquery.Response{}, err
	}
	var out updatebyquery.Response
	if req == nil || req.Query == nil {
		return out, fmt.Errorf("%w: explicit query required", ErrValidation)
	}
	if err := o.validate(); err != nil {
		return out, err
	}
	b := updatebyquery.NewUpdateByQueryFunc(nil)(r.Index()).Request(req).Refresh(o.Refresh).WaitForCompletion(!o.Async).Header("Accept", "application/json").Header("Content-Type", "application/json")
	if len(o.Routing) > 0 {
		b.Routing(o.Routing...)
	}
	if o.Conflicts.Name != "" {
		b.Conflicts(o.Conflicts)
	}
	err := r.client.DoTyped(ctx, b, &out)
	if err == nil {
		if o.Async && (out.Task == nil || *out.Task == "") || !o.Async && (out.Total == nil || len(out.Failures) > 0 || out.TimedOut != nil && *out.TimedOut || o.Conflicts.Name != "proceed" && out.VersionConflicts != nil && *out.VersionConflicts > 0) {
			err = &IncompleteOperationError{"update_by_query"}
		}
	}
	return out, err
}

// DeleteByQuery executes an explicit query through the official builder.
// Deleted documents are not restored on partial failure; per-document hooks do not run.
func (r *Repository[T]) DeleteByQuery(ctx context.Context, req *deletebyquery.Request, o ByQueryOptions) (deletebyquery.Response, error) {
	if err := validateIndexName(r.Index()); err != nil {
		return deletebyquery.Response{}, err
	}
	var out deletebyquery.Response
	if req == nil || req.Query == nil {
		return out, fmt.Errorf("%w: explicit query required", ErrValidation)
	}
	if err := o.validate(); err != nil {
		return out, err
	}
	b := deletebyquery.NewDeleteByQueryFunc(nil)(r.Index()).Request(req).Refresh(o.Refresh).WaitForCompletion(!o.Async).Header("Accept", "application/json").Header("Content-Type", "application/json")
	if len(o.Routing) > 0 {
		b.Routing(o.Routing...)
	}
	if o.Conflicts.Name != "" {
		b.Conflicts(o.Conflicts)
	}
	err := r.client.DoTyped(ctx, b, &out)
	if err == nil {
		if o.Async && (out.Task == nil || *out.Task == "") || !o.Async && (out.Total == nil || len(out.Failures) > 0 || out.TimedOut != nil && *out.TimedOut || o.Conflicts.Name != "proceed" && out.VersionConflicts != nil && *out.VersionConflicts > 0) {
			err = &IncompleteOperationError{"delete_by_query"}
		}
	}
	return out, err
}

// Task fetches a server task using the official task response model.
func (a Admin) Task(ctx context.Context, id string) (taskget.Response, error) {
	var out taskget.Response
	if err := validateTaskID(id); err != nil {
		return out, err
	}
	b := taskget.NewGetFunc(nil)(id).Header("Accept", "application/json")
	err := a.client.DoTyped(ctx, b, &out)
	return out, err
}

// WaitTask polls until completion or cancellation. Cancellation does not cancel the server task.
func (a Admin) WaitTask(ctx context.Context, id string, interval time.Duration) (taskget.Response, error) {
	if interval <= 0 {
		return taskget.Response{}, fmt.Errorf("%w: positive poll interval required", ErrValidation)
	}
	for {
		out, err := a.Task(ctx, id)
		if err != nil {
			return out, err
		}
		if out.Completed {
			if out.Error != nil {
				return out, &Error{Status: 500, Cause: *out.Error, Type: out.Error.Type}
			}
			return out, nil
		}
		if err = waitContext(ctx, interval); err != nil {
			return out, err
		}
	}
}

// CancelTask requests cancellation; callers must still inspect task completion.
func (a Admin) CancelTask(ctx context.Context, id string) (taskcancel.Response, error) {
	var out taskcancel.Response
	if err := validateTaskID(id); err != nil {
		return out, err
	}
	b := taskcancel.New(nil).TaskId(id).Header("Accept", "application/json").Header("Content-Type", "application/json")
	err := a.client.DoTyped(ctx, b, &out)
	return out, err
}
func waitContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func validateTaskID(id string) error {
	node, number, ok := strings.Cut(id, ":")
	if !ok || node == "" {
		return fmt.Errorf("%w: invalid task ID", ErrValidation)
	}
	for _, r := range node {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return fmt.Errorf("%w: invalid task node", ErrValidation)
		}
	}
	if _, err := strconv.ParseUint(number, 10, 64); err != nil {
		return fmt.Errorf("%w: invalid task number", ErrValidation)
	}
	return nil
}
