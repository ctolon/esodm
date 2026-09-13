# Administration and migrations

Repositories never create or alter cluster resources implicitly. `client.Admin()` exposes explicit management operations; anything outside this surface uses `DoTyped` with an official builder.

## Index resources

| Operation | Purpose |
| --- | --- |
| `CreateIndex`, `DeleteIndex` | Create or permanently delete an index |
| `Mapping`, `PutMapping`, `PutSettings` | Read or update mapping and settings |
| `Refresh` | Make recent writes searchable |
| `Aliases(actions...)` | Apply alias additions and removals atomically |
| `Rollover(target, conditions, dryRun)` | Roll over a write alias or data stream |
| `Put`, `Get`, `Delete` with a `ResourceKind` | Manage index templates, component templates, ingest pipelines, ILM policies and data streams |
| `ExplainLifecycle` | Read the ILM state of an index |
| `SimulatePipeline` | Run documents through a pipeline without indexing |

A data stream is created with a nil specification after its index template exists. Management calls return `ErrUnacknowledged` when the server does not acknowledge them; check the cluster state before repeating one.

## Reindex migrations

Changing an existing field mapping requires a new index. esodm implements the alias-switch pattern:

1. `PlanMigration(ctx, client, alias, target, schema)` resolves the alias to its single source index, snapshots the source mapping and computes the difference to the schema. Review `plan.Summary()`.
2. The application pauses its writers and any other alias administration.
3. `ApplyMigration` (synchronous) or `StartMigration` followed by `ResumeMigration` (asynchronous, checkpointed) creates the target, reindexes with `op_type: create`, verifies that the reindex reported no failures and that both indexes hold the same number of documents, and moves the alias in one atomic request.
4. The source index is kept for inspection or rollback.

<!-- source: examples/guide/migration.go -->
```go
package guide

import (
	"context"
	"time"

	"github.com/ctolon/esodm"
	"github.com/ctolon/esodm/migrationstore"
)

// MigrateProducts requires the application to have paused writers and serialized
// alias administration. checkpointPath must reside in an existing trusted directory.
func MigrateProducts(ctx context.Context, client *esodm.Client, checkpointPath string) (esodm.MigrationState, error) {
	schema, err := esodm.NewSchema[Product]("products-v2")
	if err != nil {
		return esodm.MigrationState{}, err
	}
	plan, err := esodm.PlanMigration(ctx, client, "products", "products-v2", schema)
	if err != nil {
		return esodm.MigrationState{}, err
	}
	store := &migrationstore.File{Path: checkpointPath}
	options := esodm.MigrationOptions{WritesPaused: true, PollInterval: time.Second, Checkpoint: store.Save}
	state, err := client.Admin().StartMigration(ctx, plan, options)
	if err != nil {
		return state, err
	}
	return client.Admin().ResumeMigration(ctx, state, options)
}

// ResumeProducts continues an existing migration while the same write pause holds.
func ResumeProducts(ctx context.Context, client *esodm.Client, checkpointPath string) (esodm.MigrationState, error) {
	store := &migrationstore.File{Path: checkpointPath}
	state, err := store.Load()
	if err != nil {
		return state, err
	}
	return client.Admin().ResumeMigration(ctx, state, esodm.MigrationOptions{
		WritesPaused: true, PollInterval: time.Second, Checkpoint: store.Save,
	})
}
```

`MigrationOptions.WritesPaused` must be set to `true`; it records that the application has stopped writing. The library cannot pause external writers itself. The alias must be unfiltered and unrouted and point to exactly one index, and the target must not exist yet.

## Checkpoints

The asynchronous form calls `MigrationOptions.Checkpoint` before and after each side effect with a `MigrationState`. The callback must persist the state durably before returning nil; a persistence error stops the migration. `migrationstore.File` writes the state to a temporary file, syncs it, renames it over the target path and syncs the directory (directory sync is skipped on Windows). Reads and writes are limited to 4 MiB. One process at a time may use a checkpoint path; coordinate multiple hosts with a lease in durable storage.

`ResumeMigration` continues from a stored state:

| Phase | Action |
| --- | --- |
| `created` | Submits the reindex task |
| `copying` | Waits for the recorded task, then verifies mappings and counts |
| `switching` | Moves the alias, or confirms a switch that already happened |
| `complete` | Returns immediately |
| `creating`, `submitting` | Returns `MigrationPendingError`: a side effect may have happened without a saved acknowledgement. Inspect the target index or the task list, correct the stored state and resume |

The write pause must last until `ResumeMigration` returns the `complete` phase. Cancelling the context stops polling; the reindex task keeps running and can be resumed later.

Online dual-write migrations and distributed locking are outside the library.

## Recovery decisions

Keep the write pause and migration lock while reconciling an ambiguous checkpoint. Preserve the returned state even when the operation returns an error: it can contain a task ID or phase that failed to reach durable storage.

- In `creating`, inspect whether the target exists and whether its mapping/settings match the approved plan. A target created by another actor is a conflict. If adopting the intended created target, persist its normalized server mapping and the `created` phase before resuming.
- In `submitting`, identify the exact reindex task before recording `TaskID` and `copying`. Do not submit another task merely because the client timed out: the first task may already be running or completed. If its identity/outcome cannot be established, stop and reconcile explicitly; keep both indexes and use a newly planned target for a fresh attempt after resolving outstanding work.
- In `switching`, resume checks mappings, counts and the current alias. An alias already on the expected target completes the checkpoint; an unexpected alias target is a conflict.
- A `complete` checkpoint is terminal and returns without repeating cluster verification. Use normal readiness/mapping checks to detect later changes.

Reindex verification compares source/target mappings and document counts, not a cryptographic comparison of every source document. Writers and competing administrators must remain paused for those checks to be meaningful. Source documents are retained after a successful switch; deleting an old index is a separate administrative decision. A missing checkpoint file is an os.ErrNotExist error; corrupt or oversized files fail loading. A file save error after rename can mean the new checkpoint is installed but its directory entry was not durably synced.
