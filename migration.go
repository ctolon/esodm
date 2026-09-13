package esodm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/elastic/go-elasticsearch/v9/typedapi/core/reindex"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types/enums/optype"
)

// MigrationState is a serializable checkpoint for one explicitly authorized migration.
// Store it in trusted application storage; it is not an authenticated capability.
// Application writes and competing alias administration must remain paused until completion.
type MigrationState struct {
	// Version is the checkpoint format version; only 1 is supported.
	Version int `json:"version"`
	// Summary records the approved source, target, alias and planned changes.
	Summary MigrationSummary `json:"summary"`
	// Mapping is the desired target mapping, normalized from the server after creation. It must be
	// valid JSON.
	Mapping json.RawMessage `json:"mapping"`
	// Settings is the snapshotted target settings JSON; JSON null means unspecified settings.
	Settings json.RawMessage `json:"settings"`
	// SourceMapping is the original source mapping snapshot, checked for drift before alias
	// switching.
	SourceMapping json.RawMessage `json:"source_mapping"`
	// Phase is planned, creating, created, submitting, copying, switching or complete. Planned
	// states cannot be resumed; creating and submitting require manual reconciliation.
	Phase string `json:"phase"`
	// TaskID is the asynchronous reindex task identifier; required in copying phase.
	TaskID string `json:"task_id,omitempty"`
	// Documents is the verified nonnegative copied count; zero before task completion is not proof
	// of an empty source.
	Documents int64 `json:"documents"`
}

// MigrationPendingError means a side effect may have occurred before its acknowledgement
// was saved. Inspect the cluster and repair the trusted checkpoint; do not restart blindly.
type MigrationPendingError struct {
	// Phase is the ambiguous phase, normally creating or submitting.
	Phase string
}

// Error describes an ambiguous migration checkpoint.
func (e *MigrationPendingError) Error() string {
	return "esodm: migration outcome requires reconciliation in phase " + e.Phase
}
func (s MigrationState) clone() MigrationState {
	s.Mapping = append(json.RawMessage(nil), s.Mapping...)
	s.Settings = append(json.RawMessage(nil), s.Settings...)
	s.SourceMapping = append(json.RawMessage(nil), s.SourceMapping...)
	s.Summary.ChangedFields = append([]string(nil), s.Summary.ChangedFields...)
	s.Summary.ChangedMappingOptions = append([]string(nil), s.Summary.ChangedMappingOptions...)
	s.Summary.TargetSettings = append(json.RawMessage(nil), s.Summary.TargetSettings...)
	s.Summary.Steps = append([]string(nil), s.Summary.Steps...)
	return s
}
func checkpoint(s MigrationState, o MigrationOptions) error {
	if o.Checkpoint != nil {
		return o.Checkpoint(s.clone())
	}
	return nil
}
func (a Admin) checkMigrationMapping(ctx context.Context, index string, want json.RawMessage) error {
	raw, err := a.Mapping(ctx, index)
	if err != nil {
		return err
	}
	var response map[string]struct {
		Mappings json.RawMessage `json:"mappings"`
	}
	if err = json.Unmarshal(raw, &response); err != nil {
		return err
	}
	if !jsonEqual(response[index].Mappings, want) {
		return fmt.Errorf("%w: migration mapping changed for %s", ErrConflict, index)
	}
	return nil
}

// StartMigration creates the target and submits an official asynchronous reindex.
// Checkpoint is called before and after each side effect. A failed checkpoint stops
// further work; the returned state always carries the latest known task/phase.
func (a Admin) StartMigration(ctx context.Context, plan *MigrationPlan, o MigrationOptions) (MigrationState, error) {
	var state MigrationState
	if plan == nil || !o.WritesPaused || o.PollInterval < 0 {
		return state, fmt.Errorf("%w: plan, paused writes and nonnegative poll interval required", ErrValidation)
	}
	state = MigrationState{Version: 1, Summary: plan.Summary(), Mapping: append(json.RawMessage(nil), plan.mapping...), Settings: append(json.RawMessage(nil), plan.settings...), SourceMapping: append(json.RawMessage(nil), plan.sourceMapping...), Phase: "planned"}
	source, err := a.resolveAlias(ctx, state.Summary.Alias)
	if err != nil {
		return state, err
	}
	if source != state.Summary.Source {
		return state, fmt.Errorf("%w: alias changed", ErrConflict)
	}
	if err = a.checkMigrationMapping(ctx, source, state.SourceMapping); err != nil {
		return state, err
	}
	if err = a.Refresh(ctx, source); err != nil {
		return state, err
	}
	state.Phase = "creating"
	if err = checkpoint(state, o); err != nil {
		return state, err
	}
	var settings any
	if string(state.Settings) != "null" {
		settings = state.Settings
	}
	if err = a.CreateIndex(ctx, state.Summary.Target, state.Mapping, settings); err != nil {
		return state, err
	}
	rawMapping, err := a.Mapping(ctx, state.Summary.Target)
	if err != nil {
		return state, err
	}
	var mappings map[string]struct {
		Mappings json.RawMessage `json:"mappings"`
	}
	if err = json.Unmarshal(rawMapping, &mappings); err != nil {
		return state, err
	}
	normalized := mappings[state.Summary.Target].Mappings
	if len(normalized) == 0 {
		return state, fmt.Errorf("esodm: missing target mapping")
	}
	state.Mapping = append(json.RawMessage(nil), normalized...)
	state.Phase = "created"
	if err = checkpoint(state, o); err != nil {
		return state, err
	}
	return a.submitMigration(ctx, state, o)
}
func (a Admin) submitMigration(ctx context.Context, state MigrationState, o MigrationOptions) (MigrationState, error) {
	state.Phase = "submitting"
	if err := checkpoint(state, o); err != nil {
		return state, err
	}
	request := &reindex.Request{Source: types.ReindexSource{Index: []string{state.Summary.Source}}, Dest: types.ReindexDestination{Index: state.Summary.Target, OpType: &optype.Create}}
	builder := reindex.New(nil).Request(request).WaitForCompletion(false).Refresh(true).Header("Accept", "application/json").Header("Content-Type", "application/json")
	var response reindex.Response
	if err := a.client.DoTyped(ctx, builder, &response); err != nil {
		return state, err
	}
	if response.Task == nil || *response.Task == "" {
		return state, &MigrationPendingError{Phase: state.Phase}
	}
	state.TaskID = *response.Task
	state.Phase = "copying"
	return state, checkpoint(state, o)
}
func (s MigrationState) validate() error {
	if s.Version != 1 || s.Documents < 0 || !json.Valid(s.Mapping) || !json.Valid(s.SourceMapping) || !json.Valid(s.Settings) {
		return fmt.Errorf("%w: invalid migration checkpoint", ErrValidation)
	}
	for _, name := range []string{s.Summary.Alias, s.Summary.Source, s.Summary.Target} {
		if err := validateIndexName(name); err != nil {
			return err
		}
	}
	if s.Summary.Source == s.Summary.Target || s.Summary.Alias == s.Summary.Target {
		return fmt.Errorf("%w: invalid migration names", ErrValidation)
	}
	switch s.Phase {
	case "creating", "submitting", "created", "copying", "switching", "complete":
	default:
		return fmt.Errorf("%w: invalid migration phase", ErrValidation)
	}
	return nil
}

// ResumeMigration waits for a recorded reindex, verifies mappings/counts and moves
// the alias atomically. Cancellation leaves the server task running for later resume.
// Creating/submitting phases require explicit reconciliation because task submission
// and checkpoint persistence cannot form one transaction.
func (a Admin) ResumeMigration(ctx context.Context, input MigrationState, o MigrationOptions) (MigrationState, error) {
	state := input.clone()
	if !o.WritesPaused || o.PollInterval < 0 {
		return state, fmt.Errorf("%w: paused writes and nonnegative poll interval required", ErrValidation)
	}
	if err := state.validate(); err != nil {
		return state, err
	}
	if state.Phase == "creating" || state.Phase == "submitting" {
		return state, &MigrationPendingError{Phase: state.Phase}
	}
	if state.Phase == "complete" {
		return state, nil
	}
	if state.Phase == "created" {
		next, err := a.submitMigration(ctx, state, o)
		if err != nil {
			return next, err
		}
		state = next
	}
	if state.Phase == "copying" {
		if state.TaskID == "" {
			return state, fmt.Errorf("%w: task ID required", ErrValidation)
		}
		interval := o.PollInterval
		if interval == 0 {
			interval = time.Second
		}
		task, err := a.WaitTask(ctx, state.TaskID, interval)
		if err != nil {
			return state, err
		}
		var response struct {
			Total, Created   *int64
			VersionConflicts int64             `json:"version_conflicts"`
			TimedOut         bool              `json:"timed_out"`
			Failures         []json.RawMessage `json:"failures"`
		}
		if len(task.Response) == 0 {
			return state, &IncompleteOperationError{"reindex"}
		}
		if err = json.Unmarshal(task.Response, &response); err != nil {
			return state, err
		}
		if response.Total == nil || response.Created == nil || *response.Total < 0 || *response.Created < 0 || response.TimedOut || response.VersionConflicts > 0 || len(response.Failures) > 0 || *response.Created != *response.Total {
			return state, &IncompleteOperationError{"reindex"}
		}
		state.Documents = *response.Created
	}
	if err := a.checkMigrationMapping(ctx, state.Summary.Source, state.SourceMapping); err != nil {
		return state, err
	}
	if err := a.checkMigrationMapping(ctx, state.Summary.Target, state.Mapping); err != nil {
		return state, err
	}
	for _, index := range []string{state.Summary.Source, state.Summary.Target} {
		if err := a.Refresh(ctx, index); err != nil {
			return state, err
		}
	}
	for _, index := range []string{state.Summary.Source, state.Summary.Target} {
		var out struct {
			Count  int64 `json:"count"`
			Shards struct {
				Failed int `json:"failed"`
			} `json:"_shards"`
		}
		if err := a.client.Do(ctx, "GET", "/"+segment(index)+"/_count", nil, nil, &out); err != nil {
			return state, err
		}
		if out.Shards.Failed > 0 {
			return state, &PartialSearchError{FailedShards: out.Shards.Failed}
		}
		if out.Count != state.Documents {
			return state, fmt.Errorf("%w: migration count mismatch", ErrConflict)
		}
	}
	current, err := a.resolveAlias(ctx, state.Summary.Alias)
	if err != nil {
		return state, err
	}
	if current == state.Summary.Target && state.Phase == "switching" {
		state.Phase = "complete"
		return state, checkpoint(state, o)
	}
	if current != state.Summary.Source {
		return state, fmt.Errorf("%w: migration alias changed", ErrConflict)
	}
	state.Phase = "switching"
	if err = checkpoint(state, o); err != nil {
		return state, err
	}
	yes := true
	if err = a.Aliases(ctx, AliasAction{Index: state.Summary.Source, Alias: state.Summary.Alias}, AliasAction{Add: true, Index: state.Summary.Target, Alias: state.Summary.Alias, WriteIndex: &yes}); err != nil {
		return state, err
	}
	state.Phase = "complete"
	return state, checkpoint(state, o)
}
