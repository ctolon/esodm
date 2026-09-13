package esodm

import (
	"context"
	"fmt"
)

// IndexerOperation is a prepared, owned payload for official esutil adapters.
// Complete must be called exactly once for the final item outcome.
type IndexerOperation struct {
	// Action is the normalized bulk operation kind.
	Action BulkAction
	// Index, ID and Routing are the prepared target metadata. Empty ID requests generation for create.
	Index, ID, Routing string
	// IfSeqNo and IfPrimaryTerm are paired optional concurrency tokens.
	IfSeqNo, IfPrimaryTerm *int64
	// Body is the owned JSON payload without a trailing newline; delete operations have no body.
	Body []byte
	// Complete must be called exactly once with the final result, HTTP status and error. It applies
	// routing and invokes AfterWrite on success.
	Complete func(context.Context, WriteResult, int, error) BulkItem
}

// PrepareIndexerOperation runs preparation once. Per-item pipelines and refresh
// are rejected because esutil.BulkIndexerItem cannot represent them.
func (r *Repository[T]) PrepareIndexerOperation(ctx context.Context, op BulkOperation[T]) (IndexerOperation, error) {
	var out IndexerOperation
	if op.Options.Pipeline != "" || op.Options.Refresh != "" {
		return out, fmt.Errorf("%w: configure pipeline/refresh on the indexer", ErrUnsupported)
	}
	var err error
	op, err = normalizeBulk(op)
	if err != nil {
		return out, err
	}
	_, body, err := r.prepareBulk(ctx, op)
	if err != nil {
		return out, err
	}
	out = IndexerOperation{Action: op.Action, ID: op.ID, Index: r.Index(), Routing: op.Options.Routing, Body: body}
	if op.Options.IfSeqNo != nil {
		// The prepared payload owns its concurrency tokens as well as its body.
		seq, term := *op.Options.IfSeqNo, *op.Options.IfPrimaryTerm
		out.IfSeqNo, out.IfPrimaryTerm = &seq, &term
	}
	action, routing := op.Action, op.Options.Routing
	out.Complete = func(ctx context.Context, result WriteResult, status int, err error) BulkItem {
		result.Routing = routing
		if err == nil && (status < 200 || status >= 300) {
			err = &Error{Status: status}
		}
		err = r.afterWrite(ctx, action, result, err)
		return BulkItem{Action: action, WriteResult: result, Status: status, Err: err}
	}
	return out, nil
}
