package esodm

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

func validateIndexName(name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	if len(name) > 255 || !utf8.ValidString(name) || strings.ToLower(name) != name || strings.ContainsAny(name, ":\"<>|\t\v\f") {
		return fmt.Errorf("%w: invalid index name %q", ErrValidation, name)
	}
	if strings.ContainsRune("-_+", rune(name[0])) {
		return fmt.Errorf("%w: invalid index prefix", ErrValidation)
	}
	return nil
}
func validateReadTarget(target string) error {
	if target == "" {
		return fmt.Errorf("%w: empty read target", ErrValidation)
	}
	for _, part := range strings.Split(target, ",") {
		if strings.Count(part, ":") > 1 {
			return fmt.Errorf("%w: invalid remote target", ErrValidation)
		}
		for _, name := range strings.Split(part, ":") {
			if err := validateIndexName(strings.ReplaceAll(name, "*", "x")); err != nil {
				return err
			}
		}
	}
	return nil
}

// WithIndex derives a repository sharing the client, hooks and mapping snapshot.
// A concrete name supports reads and writes. Wildcards, lists and remote targets
// support Search, MultiSearch, Count and PIT iteration; document writes reject them.
func (r *Repository[T]) WithIndex(name string) (*Repository[T], error) {
	if r == nil || r.schema == nil {
		return nil, fmt.Errorf("%w: unbound repository", ErrValidation)
	}
	if err := validateReadTarget(name); err != nil {
		return nil, err
	}
	schema := *r.schema
	schema.index = name
	out := *r
	out.schema = &schema
	return &out, nil
}

// MappingDrift compares the expected mapping with one existing index's mapping.
// Fields are reported at the top level, so nested differences identify their parent.
// ChangedOptions covers expected top-level mapping options other than properties.
type MappingDrift struct {
	// Index is the inspected concrete index or alias.
	Index string
	// ChangedFields lists differing properties present in both schema and server.
	ChangedFields []string
	// MissingFields lists schema properties absent from the server mapping.
	MissingFields []string
	// ExtraFields lists server properties absent from the schema.
	ExtraFields []string
	// ChangedOptions lists differing top-level mapping options outside properties.
	ChangedOptions []string
}

// Matches reports whether the server mapping matches the schema's expectations.
func (d MappingDrift) Matches() bool {
	return len(d.ChangedFields)+len(d.MissingFields)+len(d.ExtraFields)+len(d.ChangedOptions) == 0
}

// VerifyIndex reads and compares mappings without changing cluster state.
// The repository target must resolve to exactly one index; use concrete repositories
// to verify each index behind a multi-index alias or search pattern.
func (r *Repository[T]) VerifyIndex(ctx context.Context) (MappingDrift, error) {
	var out MappingDrift
	if err := validateIndexName(r.Index()); err != nil {
		return out, err
	}
	mappings, err := r.client.Admin().Mapping(ctx, r.Index())
	if err != nil {
		return out, err
	}
	var envelope map[string]struct {
		Mappings map[string]json.RawMessage `json:"mappings"`
	}
	if err := json.Unmarshal(mappings, &envelope); err != nil {
		return out, err
	}
	if len(envelope) != 1 {
		return out, fmt.Errorf("%w: mapping target must resolve to one index", ErrValidation)
	}
	var actual map[string]json.RawMessage
	for index, m := range envelope {
		out.Index = index
		actual = m.Mappings
	}
	if actual == nil {
		return out, fmt.Errorf("esodm: missing mapping response")
	}
	var expected map[string]json.RawMessage
	if err := json.Unmarshal(r.schema.mapping, &expected); err != nil {
		return out, err
	}
	var want, got map[string]json.RawMessage
	if raw := expected["properties"]; raw != nil {
		if err := json.Unmarshal(raw, &want); err != nil {
			return out, err
		}
	}
	if raw := actual["properties"]; raw != nil {
		if err := json.Unmarshal(raw, &got); err != nil {
			return out, err
		}
	}
	for field, value := range want {
		v, ok := got[field]
		if !ok {
			out.MissingFields = append(out.MissingFields, field)
		} else if !jsonEqual(value, v) {
			out.ChangedFields = append(out.ChangedFields, field)
		}
	}
	for field := range got {
		if _, ok := want[field]; !ok {
			out.ExtraFields = append(out.ExtraFields, field)
		}
	}
	for key, value := range expected {
		if key != "properties" && !jsonEqual(value, actual[key]) {
			out.ChangedOptions = append(out.ChangedOptions, key)
		}
	}
	slices.Sort(out.ChangedFields)
	slices.Sort(out.MissingFields)
	slices.Sort(out.ExtraFields)
	slices.Sort(out.ChangedOptions)
	return out, nil
}

// VerifyAll checks registered repositories in registration order without changing
// mappings. A registered Ensurer lacking VerifyIndex is rejected before requests.
func (g *Registry) VerifyAll(ctx context.Context) ([]MappingDrift, error) {
	type verifier interface {
		VerifyIndex(context.Context) (MappingDrift, error)
	}
	entries := g.snapshot()
	for _, entry := range entries {
		if _, ok := entry.(verifier); !ok {
			return nil, fmt.Errorf("%w: %s does not support mapping verification", ErrUnsupported, entry.Index())
		}
	}
	out := make([]MappingDrift, 0, len(entries))
	for _, entry := range entries {
		drift, err := entry.(verifier).VerifyIndex(ctx)
		if err != nil {
			return out, err
		}
		out = append(out, drift)
	}
	return out, nil
}
