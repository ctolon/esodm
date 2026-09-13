// Package esodmtest provides deterministic HTTP fixtures for applications using esodm.
package esodmtest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sync"

	"github.com/ctolon/esodm"
)

// TestingT is the test reporting interface used by NewClient and AssertAll.
type TestingT interface {
	Helper()
	Fatalf(string, ...any)
	Errorf(string, ...any)
}

// Request is an owned snapshot of a recorded HTTP request. Body and headers may
// contain application data; do not publish recordings without reviewing them.
type Request struct {
	// Method is the recorded HTTP method.
	Method string
	// Path is the escaped URL path without query parameters.
	Path string
	// Query is the raw URL query string, without the leading question mark.
	Query string
	// Header is an independent copy of request headers and may contain credentials.
	Header http.Header
	// Body is an independent copy of the request payload and may contain application data.
	Body []byte
}

// Recorder matches the first unused matching expectation in registration order.
// Different paths need not arrive in registration order. Expectations are single-use. Its zero
// value is usable. Configure expectations before issuing concurrent requests.
type Recorder struct {
	mu           sync.Mutex
	expectations []*Expectation
	requests     []Request
	unexpected   []string
}

// Expectation is a single response fixture. Configure it before sending requests.
type Expectation struct {
	method  string
	pattern *regexp.Regexp
	status  int
	body    []byte
	used    bool
}

// NewClient creates an ODM client with a declared version and a recording transport.
// No network or version-discovery request is made.
func NewClient(t TestingT, version esodm.Version) (*esodm.Client, *Recorder) {
	t.Helper()
	r := &Recorder{}
	c, err := esodm.ConnectVersion(context.Background(), r, version, esodm.Config{})
	if err != nil {
		t.Fatalf("create fixture client: %v", err)
	}
	return c, r
}

// On adds an expectation with an anchored regular expression for the escaped path.
// Invalid patterns panic, like regexp.MustCompile. Query parameters are available
// in Requests for separate assertions and are not part of the path match.
func (r *Recorder) On(method, pathPattern string) *Expectation {
	e := &Expectation{method: method, pattern: regexp.MustCompile("^(?:" + pathPattern + ")$"), status: 200, body: []byte("{}")}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expectations = append(r.expectations, e)
	return e
}

// Reply configures the HTTP status and JSON response body for this expectation.
func (e *Expectation) Reply(status int, body string) *Expectation {
	e.status = status
	e.body = []byte(body)
	return e
}

// Perform records a request and consumes the first matching unused expectation.
// Unexpected requests return an error and are also reported by AssertAll.
func (r *Recorder) Perform(req *http.Request) (*http.Response, error) {
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	var body []byte
	if req.Body != nil {
		var err error
		body, err = io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, err
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, Request{Method: req.Method, Path: req.URL.EscapedPath(), Query: req.URL.RawQuery, Header: req.Header.Clone(), Body: body})
	for _, e := range r.expectations {
		if !e.used && e.method == req.Method && e.pattern.MatchString(req.URL.EscapedPath()) {
			e.used = true
			return &http.Response{StatusCode: e.status, Header: http.Header{"Content-Type": {"application/json"}, "X-Elastic-Product": {"Elasticsearch"}}, Body: io.NopCloser(bytes.NewReader(e.body)), Request: req}, nil
		}
	}
	message := fmt.Sprintf("unexpected request: %s %s", req.Method, req.URL.EscapedPath())
	r.unexpected = append(r.unexpected, message)
	return nil, fmt.Errorf("%s", message)
}

// Requests returns independent snapshots in arrival order.
func (r *Recorder) Requests() []Request {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Request, len(r.requests))
	copy(out, r.requests)
	for i := range out {
		out[i].Header = out[i].Header.Clone()
		out[i].Body = bytes.Clone(out[i].Body)
	}
	return out
}

// AssertAll reports unused expectations and unexpected requests.
func (r *Recorder) AssertAll(t TestingT) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.expectations {
		if !e.used {
			t.Errorf("unused expectation: %s %s", e.method, e.pattern)
		}
	}
	for _, message := range r.unexpected {
		t.Errorf("%s", message)
	}
}
