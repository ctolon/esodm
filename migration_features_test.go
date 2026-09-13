package esodm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func migrationFixture(t *testing.T) (*Client, *MigrationPlan, *string, *int) {
	t.Helper()
	schema, err := NewSchema[testDoc]("target")
	if err != nil {
		t.Fatal(err)
	}
	plan := &MigrationPlan{summary: MigrationSummary{Alias: "live", Source: "source", Target: "target", ChangedFields: []string{"age"}}, mapping: schema.Mapping(), sourceMapping: schema.Mapping(), settings: schema.Settings()}
	current := "source"
	submissions := 0
	c := testClient(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.URL.Path == "/_alias/live":
			return response(200, `{"`+current+`":{"aliases":{"live":{}}}}`), nil
		case strings.HasSuffix(req.URL.Path, "/_mapping"):
			index := strings.Split(req.URL.Path, "/")[1]
			return response(200, `{"`+index+`":{"mappings":`+string(schema.Mapping())+`}}`), nil
		case req.URL.Path == "/target" && req.Method == "PUT":
			return response(200, `{"acknowledged":true}`), nil
		case strings.HasSuffix(req.URL.Path, "/_refresh"):
			return response(200, `{"_shards":{"failed":0}}`), nil
		case req.URL.Path == "/_reindex":
			submissions++
			if req.URL.Query().Get("wait_for_completion") != "false" {
				t.Fatal(req.URL)
			}
			return response(200, `{"task":"node:1"}`), nil
		case req.URL.Path == "/_tasks/node:1":
			return response(200, `{"completed":true,"task":{},"response":{"total":3,"created":3}}`), nil
		case strings.HasSuffix(req.URL.Path, "/_count"):
			return response(200, `{"count":3,"_shards":{"failed":0}}`), nil
		case req.URL.Path == "/_aliases":
			current = "target"
			return response(200, `{"acknowledged":true}`), nil
		default:
			t.Fatal(req.Method, req.URL)
			return nil, nil
		}
	})
	return c, plan, &current, &submissions
}
func TestMigrationCheckpointResume(t *testing.T) {
	c, plan, current, submissions := migrationFixture(t)
	var checkpoints []MigrationState
	options := MigrationOptions{WritesPaused: true, PollInterval: time.Nanosecond, Checkpoint: func(state MigrationState) error {
		checkpoints = append(checkpoints, state)
		state.Summary.ChangedFields[0] = "mutated"
		return nil
	}}
	state, err := c.Admin().StartMigration(context.Background(), plan, options)
	if err != nil || state.Phase != "copying" || state.Summary.ChangedFields[0] != "age" {
		t.Fatal(state, err)
	}
	serialized, _ := json.Marshal(state)
	var restored MigrationState
	_ = json.Unmarshal(serialized, &restored)
	restored, err = c.Admin().ResumeMigration(context.Background(), restored, options)
	if err != nil || restored.Phase != "complete" || *current != "target" || *submissions != 1 || len(checkpoints) != 6 {
		t.Fatal(restored, err, *current, *submissions, len(checkpoints))
	}
	if _, err = c.Admin().ResumeMigration(context.Background(), restored, options); err != nil {
		t.Fatal(err)
	}
	// A lost alias acknowledgement can resume by observing the already-moved alias.
	restored.Phase = "switching"
	if restored, err = c.Admin().ResumeMigration(context.Background(), restored, options); err != nil || restored.Phase != "complete" {
		t.Fatal(restored, err)
	}
}
func TestMigrationStopsOnCheckpointFailure(t *testing.T) {
	for _, phase := range []string{"creating", "created", "submitting", "copying", "switching", "complete"} {
		t.Run(phase, func(t *testing.T) {
			c, plan, _, submissions := migrationFixture(t)
			options := MigrationOptions{WritesPaused: true, PollInterval: time.Nanosecond, Checkpoint: func(state MigrationState) error {
				if state.Phase == phase {
					return io.ErrClosedPipe
				}
				return nil
			}}
			state, err := c.Admin().StartMigration(context.Background(), plan, options)
			if err == nil {
				state, err = c.Admin().ResumeMigration(context.Background(), state, options)
			}
			if !errors.Is(err, io.ErrClosedPipe) || state.Phase != phase {
				t.Fatal(state, err)
			}
			if (phase == "creating" || phase == "created" || phase == "submitting") && *submissions != 0 {
				t.Fatal(*submissions)
			}
			if phase == "creating" || phase == "submitting" {
				_, err = c.Admin().ResumeMigration(context.Background(), state, MigrationOptions{WritesPaused: true})
				var pending *MigrationPendingError
				if !errors.As(err, &pending) {
					t.Fatal(err)
				}
				_ = pending.Error()
			}
			if phase == "created" {
				state, err = c.Admin().ResumeMigration(context.Background(), state, MigrationOptions{WritesPaused: true, PollInterval: time.Nanosecond})
				if err != nil || state.Phase != "complete" {
					t.Fatal(state, err)
				}
			}
		})
	}
}
func TestTaskErrorsAndCancellation(t *testing.T) {
	for _, body := range []string{`{"completed":true,"error":{"type":"failure","reason":"failed"}}`, `{"completed":false,"task":{}}`} {
		c := testClient(func(*http.Request) (*http.Response, error) { return response(200, body), nil })
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := c.Admin().WaitTask(ctx, "node:1", time.Nanosecond)
		if err == nil {
			t.Fatal(body)
		}
	}
	c := testClient(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/_tasks/node:1/_cancel" {
			t.Fatal(req.URL)
		}
		return response(200, `{"nodes":{}}`), nil
	})
	if _, err := c.Admin().CancelTask(context.Background(), "node:1"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"", "node", ":1", "node:bad", "node/../x:1"} {
		if _, err := c.Admin().Task(context.Background(), id); !errors.Is(err, ErrValidation) {
			t.Fatal(id, err)
		}
		if _, err := c.Admin().CancelTask(context.Background(), id); !errors.Is(err, ErrValidation) {
			t.Fatal(err)
		}
	}
	if _, err := c.Admin().WaitTask(context.Background(), "node:1", 0); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
}
func TestMigrationInvalidAndPartial(t *testing.T) {
	c, plan, _, _ := migrationFixture(t)
	if _, err := c.Admin().StartMigration(context.Background(), nil, MigrationOptions{}); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	state, err := c.Admin().StartMigration(context.Background(), plan, MigrationOptions{WritesPaused: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*MigrationState){func(s *MigrationState) { s.Version = 0 }, func(s *MigrationState) { s.Phase = "unknown" }, func(s *MigrationState) { s.Mapping = nil }, func(s *MigrationState) { s.Summary.Target = s.Summary.Source }, func(s *MigrationState) { s.Summary.Target = "../bad" }, func(s *MigrationState) { s.TaskID = "" }} {
		bad := state.clone()
		change(&bad)
		if _, err := c.Admin().ResumeMigration(context.Background(), bad, MigrationOptions{WritesPaused: true}); err == nil {
			t.Fatal(bad)
		}
	}
	original := c.transport
	for _, body := range []string{`{"completed":true,"task":{}}`, `{"completed":true,"response":{"total":3,"created":2}}`, `{"completed":true,"response":{"total":3,"created":3,"failures":[{}]}}`, `{"completed":true,"response":[]}`} {
		c.transport = TransportFunc(func(req *http.Request) (*http.Response, error) {
			if strings.HasPrefix(req.URL.Path, "/_tasks/") {
				return response(200, body), nil
			}
			return original.Perform(req)
		})
		if _, err := c.Admin().ResumeMigration(context.Background(), state, MigrationOptions{WritesPaused: true}); err == nil {
			t.Fatal(body)
		}
	}
}

func TestMigrationResumeFailuresPreserveCheckpoint(t *testing.T) {
	for _, tc := range []struct {
		name, path, body string
		status           int
	}{
		{"source drift", "/source/_mapping", `{"source":{"mappings":{}}}`, 200},
		{"target drift", "/target/_mapping", `{"target":{"mappings":{}}}`, 200},
		{"source read", "/source/_mapping", `{"error":{"type":"unavailable"}}`, 503},
		{"target read", "/target/_mapping", `{"error":{"type":"unavailable"}}`, 503},
		{"source refresh", "/source/_refresh", `{"_shards":{"failed":1}}`, 200},
		{"target refresh", "/target/_refresh", `{"_shards":{"failed":1}}`, 200},
		{"count error", "/source/_count", `{}`, 503},
		{"count partial", "/source/_count", `{"count":3,"_shards":{"failed":1}}`, 200},
		{"count mismatch", "/target/_count", `{"count":2}`, 200},
		{"alias moved", "/_alias/live", `{"other":{"aliases":{"live":{}}}}`, 200},
		{"alias read error", "/_alias/live", `{}`, 503},
		{"alias write error", "/_aliases", `{}`, 503},
		{"task error", "/_tasks/node:1", `{"completed":true,"error":{"type":"task_failure","reason":"failed"}}`, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, plan, _, _ := migrationFixture(t)
			options := MigrationOptions{WritesPaused: true, PollInterval: time.Nanosecond}
			state, err := c.Admin().StartMigration(context.Background(), plan, options)
			if err != nil {
				t.Fatal(err)
			}
			original := c.transport
			c.transport = TransportFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == tc.path {
					return response(tc.status, tc.body), nil
				}
				return original.Perform(req)
			})
			got, err := c.Admin().ResumeMigration(context.Background(), state, options)
			if err == nil || got.Phase == "complete" || state.Phase != "copying" {
				t.Fatal(got, err)
			}
		})
	}
}
