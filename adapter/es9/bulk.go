package es9

import (
	"bytes"
	"context"
	"fmt"
	"github.com/ctolon/esodm"
	"github.com/elastic/go-elasticsearch/v9/esutil"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
)

// BulkIndexerItem prepares a document for the official indexer, preserving ODM
// validation and completion hooks. onResult is required and may run concurrently.
// Configure batch pipeline/refresh on the indexer; per-item values are rejected.
// If indexer.Add fails, invoke the returned item's OnFailure with that error.
func BulkIndexerItem[T any](ctx context.Context, repo *esodm.Repository[T], op esodm.BulkOperation[T], onResult func(context.Context, esodm.BulkItem)) (esutil.BulkIndexerItem, error) {
	var item esutil.BulkIndexerItem
	if repo == nil || onResult == nil {
		return item, fmt.Errorf("%w: repository and result callback required", esodm.ErrValidation)
	}
	prepared, err := repo.PrepareIndexerOperation(ctx, op)
	if err != nil {
		return item, err
	}
	item = esutil.BulkIndexerItem{Index: prepared.Index, Action: string(prepared.Action), DocumentID: prepared.ID, Routing: prepared.Routing, IfSeqNo: prepared.IfSeqNo, IfPrimaryTerm: prepared.IfPrimaryTerm}
	if len(prepared.Body) > 0 {
		item.Body = bytes.NewReader(prepared.Body)
	}
	complete := func(ctx context.Context, response esutil.BulkIndexerResponseItem, err error) {
		result := esodm.WriteResult{Metadata: esodm.Metadata{ID: response.DocumentID, Index: response.Index, Version: response.Version}, Result: response.Result}
		if response.PrimTerm > 0 {
			seq, term := response.SeqNo, response.PrimTerm
			result.SeqNo = &seq
			result.PrimaryTerm = &term
		}
		result.Shards = response.Shards
		if err == nil && response.Error.Type != "" {
			cause := types.ErrorCause{Type: response.Error.Type, Reason: &response.Error.Reason}
			if response.Error.Cause.Type != "" || response.Error.Cause.Reason != "" {
				cause.CausedBy = &types.ErrorCause{Type: response.Error.Cause.Type, Reason: &response.Error.Cause.Reason}
			}
			err = &esodm.Error{Status: response.Status, Type: response.Error.Type, Reason: response.Error.Reason, Cause: cause}
		}
		onResult(ctx, prepared.Complete(ctx, result, response.Status, err))
	}
	item.OnSuccess = func(ctx context.Context, _ esutil.BulkIndexerItem, response esutil.BulkIndexerResponseItem) {
		complete(ctx, response, nil)
	}
	item.OnFailure = func(ctx context.Context, _ esutil.BulkIndexerItem, response esutil.BulkIndexerResponseItem, err error) {
		complete(ctx, response, err)
	}
	return item, nil
}
