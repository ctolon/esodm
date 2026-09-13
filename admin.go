package esodm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"time"
)

// Admin exposes explicit resource management; repository construction never
// creates or alters cluster resources implicitly.
type Admin struct {
	client *Client
}

// Admin returns explicit cluster-resource management operations.
func (c *Client) Admin() Admin {
	return Admin{c}
}

// ResourceKind selects one of the resource endpoints supported by Admin.
type ResourceKind string

const (
	// IndexTemplate selects composable index templates.
	IndexTemplate ResourceKind = "_index_template"
	// ComponentTemplate selects reusable template components.
	ComponentTemplate ResourceKind = "_component_template"
	// IngestPipeline selects document ingest pipelines.
	IngestPipeline ResourceKind = "_ingest/pipeline"
	// LifecyclePolicy selects index lifecycle policies.
	LifecyclePolicy ResourceKind = "_ilm/policy"
	// DataStream selects data stream resources.
	DataStream ResourceKind = "_data_stream"
)

func resourcePath(kind ResourceKind, name string) (string, error) {
	if err := validateName(name); err != nil {
		return "", err
	}
	switch kind {
	case IndexTemplate, ComponentTemplate, IngestPipeline, LifecyclePolicy, DataStream:
		return "/" + string(kind) + "/" + segment(name), nil
	default:
		return "", fmt.Errorf("%w: unknown resource kind", ErrValidation)
	}
}

// Put creates or replaces a named resource and requires server acknowledgement.
// DataStream requires a nil spec; other kinds require a specification.
func (a Admin) Put(ctx context.Context, kind ResourceKind, name string, spec any) error {
	path, err := resourcePath(kind, name)
	if err != nil {
		return err
	}
	if kind == DataStream && spec != nil {
		return fmt.Errorf("%w: data stream creation has no body; configure an index template first", ErrValidation)
	}
	if kind != DataStream && spec == nil {
		return fmt.Errorf("%w: resource specification required", ErrValidation)
	}
	return a.ack(ctx, "PUT", path, spec)
}

// Get returns the raw JSON representation of a named cluster resource.
func (a Admin) Get(ctx context.Context, kind ResourceKind, name string) (json.RawMessage, error) {
	var out json.RawMessage
	path, err := resourcePath(kind, name)
	if err != nil {
		return nil, err
	}
	err = a.client.Do(ctx, "GET", path, nil, nil, &out)
	return out, err
}

// Delete removes a named cluster resource and requires server acknowledgement.
func (a Admin) Delete(ctx context.Context, kind ResourceKind, name string) error {
	path, err := resourcePath(kind, name)
	if err != nil {
		return err
	}
	return a.ack(ctx, "DELETE", path, nil)
}

// CreateIndex creates an index using explicit mapping and settings values.
// Both arguments accept official typed models; nil values omit those sections.
func (a Admin) CreateIndex(ctx context.Context, name string, mapping, settings any) error {
	if err := validateName(name); err != nil {
		return err
	}
	body := map[string]any{}
	if mapping != nil {
		body["mappings"] = mapping
	}
	if settings != nil {
		body["settings"] = settings
	}
	return a.ack(ctx, "PUT", "/"+segment(name), body)
}

// DeleteIndex permanently deletes the named index after validating its name.
func (a Admin) DeleteIndex(ctx context.Context, name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	return a.ack(ctx, "DELETE", "/"+segment(name), nil)
}

// Mapping returns the raw mapping response for an index.
func (a Admin) Mapping(ctx context.Context, index string) (json.RawMessage, error) {
	var out json.RawMessage
	if err := validateName(index); err != nil {
		return nil, err
	}
	err := a.client.Do(ctx, "GET", "/"+segment(index)+"/_mapping", nil, nil, &out)
	return out, err
}

// PutMapping applies an additive mapping update and requires acknowledgement.
func (a Admin) PutMapping(ctx context.Context, index string, mapping any) error {
	if err := validateName(index); err != nil {
		return err
	}
	return a.ack(ctx, "PUT", "/"+segment(index)+"/_mapping", mapping)
}

// PutSettings updates index settings and requires acknowledgement.
func (a Admin) PutSettings(ctx context.Context, index string, settings any) error {
	if err := validateName(index); err != nil {
		return err
	}
	return a.ack(ctx, "PUT", "/"+segment(index)+"/_settings", settings)
}

// Refresh makes completed writes searchable and rejects failed shards.
func (a Admin) Refresh(ctx context.Context, index string) error {
	if err := validateName(index); err != nil {
		return err
	}
	var out struct {
		Shards struct {
			Failed int `json:"failed"`
		} `json:"_shards"`
	}
	if err := a.client.Do(ctx, "POST", "/"+segment(index)+"/_refresh", nil, nil, &out); err != nil {
		return err
	}
	if out.Shards.Failed > 0 {
		return &PartialSearchError{FailedShards: out.Shards.Failed}
	}
	return nil
}

// AliasAction adds or removes an index association in an atomic alias request.
type AliasAction struct {
	// Add selects adding an alias association; false removes it.
	Add bool
	// Index is the required index associated with the alias.
	Index string
	// Alias is the required alias name.
	Alias string
	// WriteIndex sets is_write_index on add: nil omits it, true selects this index, false excludes
	// it as a write index. It is ignored on remove.
	WriteIndex *bool
}

// Aliases applies all alias actions atomically and requires acknowledgement.
func (a Admin) Aliases(ctx context.Context, actions ...AliasAction) error {
	if len(actions) == 0 {
		return fmt.Errorf("%w: alias actions required", ErrValidation)
	}
	body := make([]any, len(actions))
	for i, action := range actions {
		if err := validateName(action.Index); err != nil {
			return err
		}
		if err := validateName(action.Alias); err != nil {
			return err
		}
		kind := "remove"
		spec := map[string]any{"index": action.Index, "alias": action.Alias}
		if action.Add {
			kind = "add"
			if action.WriteIndex != nil {
				spec["is_write_index"] = *action.WriteIndex
			}
		} else {
			spec["must_exist"] = true
		}
		body[i] = map[string]any{kind: spec}
	}
	return a.ack(ctx, "POST", "/_aliases", map[string]any{"actions": body})
}

// RolloverResult reports condition checks and the indices involved in rollover.
type RolloverResult struct {
	// OldIndex is the previous write index.
	OldIndex string `json:"old_index"`
	// NewIndex is the proposed or created next write index.
	NewIndex string `json:"new_index"`
	// RolledOver reports whether the server performed the rollover.
	RolledOver bool `json:"rolled_over"`
	// DryRun reports that only conditions were evaluated.
	DryRun bool `json:"dry_run"`
	// Conditions maps server condition descriptions to whether each condition matched.
	Conditions map[string]bool `json:"conditions"`
}

// Rollover evaluates conditions and optionally creates the next write index.
// When dryRun is true, the server only evaluates conditions.
func (a Admin) Rollover(ctx context.Context, target string, conditions map[string]any, dryRun bool) (RolloverResult, error) {
	var out RolloverResult
	if err := validateName(target); err != nil {
		return out, err
	}
	q := url.Values{}
	if dryRun {
		q.Set("dry_run", "true")
	}
	err := a.client.Do(ctx, "POST", "/"+segment(target)+"/_rollover", q, map[string]any{"conditions": conditions}, &out)
	return out, err
}

// ExplainLifecycle returns the index lifecycle status as raw JSON.
func (a Admin) ExplainLifecycle(ctx context.Context, index string) (json.RawMessage, error) {
	var out json.RawMessage
	if err := validateName(index); err != nil {
		return nil, err
	}
	err := a.client.Do(ctx, "GET", "/"+segment(index)+"/_ilm/explain", nil, nil, &out)
	return out, err
}

// SimulatePipeline evaluates documents through a pipeline without indexing them.
func (a Admin) SimulatePipeline(ctx context.Context, name string, documents []any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := validateName(name); err != nil {
		return nil, err
	}
	docs := make([]any, len(documents))
	for i, doc := range documents {
		docs[i] = map[string]any{"_source": doc}
	}
	err := a.client.Do(ctx, "POST", "/_ingest/pipeline/"+segment(name)+"/_simulate", nil, map[string]any{"docs": docs}, &out)
	return out, err
}

// MigrationSummary lists the source, target, mapping differences and planned steps.
type MigrationSummary struct {
	// Alias is the application alias to switch.
	Alias string `json:"alias"`
	// Source is the concrete index currently selected by Alias.
	Source string `json:"source"`
	// Target is the new concrete index to create.
	Target string `json:"target"`
	// ChangedFields lists top-level mapping properties that differ, including additions and
	// removals.
	ChangedFields []string `json:"changed_fields"`
	// Steps lists planned operations in execution order; this is descriptive output, not executable
	// instructions.
	Steps []string `json:"steps"`
	// ChangedMappingOptions lists differences outside the properties map.
	ChangedMappingOptions []string `json:"changed_mapping_options"`
	// TargetSettings contains the exact desired settings; server defaults are not compared.
	TargetSettings json.RawMessage `json:"target_settings"`
}

// MigrationPlan owns snapshots; public Summary returns a copy. Plans must be
// prepared again after any source mapping or alias change.
type MigrationPlan struct {
	summary                          MigrationSummary
	mapping, settings, sourceMapping json.RawMessage
}

// Summary returns an independent copy of the migration plan description.
func (p *MigrationPlan) Summary() MigrationSummary {
	s := p.summary
	s.ChangedFields = append([]string(nil), s.ChangedFields...)
	s.Steps = append([]string(nil), s.Steps...)
	s.ChangedMappingOptions = append([]string(nil), s.ChangedMappingOptions...)
	s.TargetSettings = append(json.RawMessage(nil), s.TargetSettings...)
	return s
}

// MarshalJSON serializes the snapshot, returning any deferred construction error.
func (p *MigrationPlan) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.Summary())
}
func (a Admin) resolveAlias(ctx context.Context, alias string) (string, error) {
	var result map[string]struct {
		Aliases map[string]json.RawMessage `json:"aliases"`
	}
	if err := a.client.Do(ctx, "GET", "/_alias/"+segment(alias), nil, nil, &result); err != nil {
		return "", err
	}
	if len(result) != 1 {
		return "", fmt.Errorf("%w: migration requires a single-index alias", ErrValidation)
	}
	for index, entry := range result {
		raw, ok := entry.Aliases[alias]
		if !ok {
			return "", fmt.Errorf("%w: alias not found", ErrValidation)
		}
		var options map[string]json.RawMessage
		_ = json.Unmarshal(raw, &options)
		for k := range options {
			if k != "is_write_index" {
				return "", fmt.Errorf("%w: migration does not support filtered or routed aliases", ErrUnsupported)
			}
		}
		return index, nil
	}
	return "", ErrNotFound
}

// PlanMigration snapshots a single-index alias and desired schema without writing.
// Filtered or routed aliases are rejected; target must name a new index.
func PlanMigration[T any](ctx context.Context, client *Client, alias, target string, schema *Schema[T]) (*MigrationPlan, error) {
	if client == nil || schema == nil {
		return nil, fmt.Errorf("%w: client/schema required", ErrValidation)
	}
	if err := validateName(alias); err != nil {
		return nil, err
	}
	if err := validateName(target); err != nil {
		return nil, err
	}
	if alias == target {
		return nil, fmt.Errorf("%w: target must differ from alias", ErrValidation)
	}
	a := client.Admin()
	source, err := a.resolveAlias(ctx, alias)
	if err != nil {
		return nil, err
	}
	if source == target {
		return nil, fmt.Errorf("%w: target must be a new index", ErrValidation)
	}
	raw, err := a.Mapping(ctx, source)
	if err != nil {
		return nil, err
	}
	var response map[string]struct {
		Mappings json.RawMessage `json:"mappings"`
	}
	if err = json.Unmarshal(raw, &response); err != nil {
		return nil, err
	}
	old := response[source].Mappings
	if len(old) == 0 {
		return nil, fmt.Errorf("esodm: missing source mapping")
	}
	var prior, next struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	_ = json.Unmarshal(old, &prior)
	_ = json.Unmarshal(schema.mapping, &next)
	changed := []string{}
	for field, v := range next.Properties {
		if !jsonEqual(v, prior.Properties[field]) {
			changed = append(changed, field)
		}
	}
	for field := range prior.Properties {
		if _, ok := next.Properties[field]; !ok {
			changed = append(changed, field)
		}
	}
	slices.Sort(changed)
	var oldOptions, newOptions map[string]json.RawMessage
	_ = json.Unmarshal(old, &oldOptions)
	_ = json.Unmarshal(schema.mapping, &newOptions)
	delete(oldOptions, "properties")
	delete(newOptions, "properties")
	var changedOptions []string
	for k, v := range newOptions {
		if !jsonEqual(v, oldOptions[k]) {
			changedOptions = append(changedOptions, k)
		}
	}
	for k := range oldOptions {
		if _, ok := newOptions[k]; !ok {
			changedOptions = append(changedOptions, k)
		}
	}
	slices.Sort(changedOptions)
	return &MigrationPlan{
		summary: MigrationSummary{
			Alias: alias, Source: source, Target: target, ChangedFields: changed, ChangedMappingOptions: changedOptions, TargetSettings: schema.Settings(),
			Steps: []string{
				"pause application writes and refresh source",
				"create target with desired mapping/settings",
				"reindex without overwriting documents",
				"verify no failures and equal document counts",
				"atomically move alias; retain source index",
			},
		},
		mapping:       schema.Mapping(),
		settings:      schema.Settings(),
		sourceMapping: append(json.RawMessage(nil), old...),
	}, nil
}
func jsonEqual(a, b []byte) bool {
	var x, y any
	dx, dy := json.NewDecoder(bytes.NewReader(a)), json.NewDecoder(bytes.NewReader(b))
	dx.UseNumber()
	dy.UseNumber()
	if dx.Decode(&x) != nil || dy.Decode(&y) != nil {
		return false
	}
	ax, _ := json.Marshal(x)
	by, _ := json.Marshal(y)
	return string(ax) == string(by)
}

// MigrationOptions records the caller's assurance that application writes are paused.
type MigrationOptions struct {
	// WritesPaused must be true. It asserts that the caller paused writers and serialized migrations
	// and alias administration; the library does not acquire a lock.
	WritesPaused bool
	// PollInterval is a nonnegative asynchronous polling duration; zero selects one second.
	// ApplyMigration does not poll, but still rejects a negative value.
	PollInterval time.Duration
	// Checkpoint persists asynchronous progress. It must complete durably before returning nil.
	Checkpoint func(MigrationState) error
}

// MigrationResult reports the retained indices, copied count and alias switch outcome.
type MigrationResult struct {
	// Source is the retained original index.
	Source string
	// Target is the destination index; it is retained even when migration fails.
	Target string
	// Documents is the server-reported number of created documents.
	Documents int64
	// AliasMoved reports successful acknowledgement of the atomic alias switch.
	AliasMoved bool
}

// ApplyMigration requires the caller to pause application writes and serialize
// migration/alias administration until completion. A failure retains both indices
// for inspection and never deletes data. Re-plan with a new target before retrying.
func (a Admin) ApplyMigration(ctx context.Context, plan *MigrationPlan, options MigrationOptions) (MigrationResult, error) {
	var out MigrationResult
	if plan == nil || !options.WritesPaused || options.PollInterval < 0 {
		return out, fmt.Errorf("%w: plan, externally paused writes and nonnegative poll interval required", ErrValidation)
	}
	s := plan.summary
	out.Source = s.Source
	out.Target = s.Target
	source, err := a.resolveAlias(ctx, s.Alias)
	if err != nil {
		return out, err
	}
	if source != s.Source {
		return out, fmt.Errorf("%w: alias changed since planning", ErrConflict)
	}
	raw, err := a.Mapping(ctx, source)
	if err != nil {
		return out, err
	}
	var response map[string]struct {
		Mappings json.RawMessage `json:"mappings"`
	}
	if err = json.Unmarshal(raw, &response); err != nil {
		return out, err
	}
	if !jsonEqual(response[source].Mappings, plan.sourceMapping) {
		return out, fmt.Errorf("%w: mapping changed since planning", ErrConflict)
	}
	if err = a.Refresh(ctx, source); err != nil {
		return out, err
	}
	var settings any
	if string(plan.settings) != "null" {
		settings = plan.settings
	}
	if err = a.CreateIndex(ctx, s.Target, plan.mapping, settings); err != nil {
		return out, err
	}
	var reindex struct {
		Total            int64             `json:"total"`
		Created          int64             `json:"created"`
		VersionConflicts int64             `json:"version_conflicts"`
		TimedOut         bool              `json:"timed_out"`
		Failures         []json.RawMessage `json:"failures"`
	}
	err = a.client.Do(ctx, "POST", "/_reindex",
		url.Values{"wait_for_completion": {"true"}, "refresh": {"true"}},
		map[string]any{
			"source": map[string]string{"index": source},
			"dest":   map[string]string{"index": s.Target, "op_type": "create"},
		}, &reindex)
	if err != nil {
		return out, err
	}
	if reindex.TimedOut || reindex.VersionConflicts > 0 || len(reindex.Failures) > 0 || reindex.Created != reindex.Total {
		return out, fmt.Errorf("esodm: reindex incomplete; alias unchanged")
	}
	counts := make([]int64, 2)
	for i, index := range []string{source, s.Target} {
		var count struct {
			Count  int64 `json:"count"`
			Shards struct {
				Failed int `json:"failed"`
			} `json:"_shards"`
		}
		if err = a.client.Do(ctx, "GET", "/"+segment(index)+"/_count", nil, nil, &count); err != nil {
			return out, err
		}
		if count.Shards.Failed > 0 {
			return out, &PartialSearchError{FailedShards: count.Shards.Failed}
		}
		counts[i] = count.Count
	}
	if counts[0] != counts[1] || counts[1] != reindex.Created {
		return out, fmt.Errorf("esodm: migration count verification failed")
	}
	out.Documents = counts[1]
	current, err := a.resolveAlias(ctx, s.Alias)
	if err != nil {
		return out, err
	}
	if current != source {
		return out, fmt.Errorf("%w: alias changed during migration", ErrConflict)
	}
	w := true
	err = a.Aliases(ctx, AliasAction{Index: source, Alias: s.Alias}, AliasAction{Add: true, Index: s.Target, Alias: s.Alias, WriteIndex: &w})
	if err == nil {
		out.AliasMoved = true
	}
	return out, err
}

func (a Admin) ack(ctx context.Context, method, path string, body any) error {
	var out struct {
		Acknowledged *bool `json:"acknowledged"`
		Errors       bool  `json:"errors"`
	}
	if err := a.client.Do(ctx, method, path, nil, body, &out); err != nil {
		return err
	}
	if out.Acknowledged == nil || !*out.Acknowledged || out.Errors {
		return ErrUnacknowledged
	}
	return nil
}
