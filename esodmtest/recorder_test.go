package esodmtest

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/ctolon/esodm"
)

type reporter struct{ errors int }

func (*reporter) Helper()                 {}
func (r *reporter) Errorf(string, ...any) { r.errors++ }
func (r *reporter) Fatalf(string, ...any) { r.errors++ }
func TestRecorder(t *testing.T) {
	c, r := NewClient(t, esodm.Version{Major: 9, Minor: 5, Patch: 2})
	r.On("POST", "/items/_doc/[0-9]+").Reply(201, `{"_id":"1"}`)
	if err := c.Do(context.Background(), "POST", "/items/_doc/1", nil, map[string]string{"name": "Ada"}, nil); err != nil {
		t.Fatal(err)
	}
	r.AssertAll(t)
	requests := r.Requests()
	if len(requests) != 1 || !strings.Contains(string(requests[0].Body), "Ada") {
		t.Fatal(requests)
	}
	requests[0].Body[0] = 'x'
	requests[0].Header.Set("private", "changed")
	if r.Requests()[0].Body[0] == 'x' || r.Requests()[0].Header.Get("private") != "" {
		t.Fatal("mutable snapshot")
	}
	if err := c.Do(context.Background(), "GET", "/unexpected", nil, nil, nil); err == nil {
		t.Fatal("unexpected accepted")
	}
	r.On("GET", "/unused")
	report := &reporter{}
	r.AssertAll(report)
	if report.errors != 2 {
		t.Fatal(report.errors)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "/", nil)
	if _, err := r.Perform(req); err == nil {
		t.Fatal("canceled accepted")
	}
}
