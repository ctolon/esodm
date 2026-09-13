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
