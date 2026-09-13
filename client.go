package esodm

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/elastic/elastic-transport-go/v8/elastictransport"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
)

// Transport is implemented by the official Elasticsearch client. Authentication,
// connection pooling, TLS, compression and transport retries belong to that client.
type Transport interface {
	Perform(*http.Request) (*http.Response, error)
}

var (
	// ErrUnavailable identifies transient gateway/service failures.
	ErrUnavailable = errors.New("esodm: unavailable")
	// ErrTooManyRequests identifies HTTP 429 rate limiting.
	ErrTooManyRequests = errors.New("esodm: too many requests")
	// ErrUnacknowledged reports an uncertain management outcome; inspect server state before retrying.
	ErrUnacknowledged = errors.New("esodm: management action not acknowledged; inspect server state before retrying")
	// ErrNotFound identifies a missing document or resource.
	ErrNotFound = errors.New("esodm: not found")
	// ErrConflict identifies an optimistic concurrency or create conflict.
	ErrConflict = errors.New("esodm: conflict")
	// ErrValidation identifies invalid input or an HTTP 400 response.
	ErrValidation = errors.New("esodm: validation")
	// ErrUnsupported identifies a server version or feature outside the supported contract.
	ErrUnsupported = errors.New("esodm: unsupported capability")
	// ErrResponseTooLarge indicates that the configured response byte limit was exceeded.
	ErrResponseTooLarge = errors.New("esodm: response exceeds configured limit")
)

// Error preserves the HTTP status and Elasticsearch error details.
// Body may contain document data; avoid including it in unredacted logs.
type Error struct {
	// Cause preserves the official structured error, including root and nested causes.
	Cause types.ErrorCause
	// Status is the HTTP response status, including per-item bulk or multi-get statuses.
	Status int
	// Type is the Elasticsearch error type, or empty when unavailable.
	Type string
	// Reason is the server error description and can contain sensitive document data.
	Reason string
	// Body is the raw error response when available and can contain sensitive data; nil means it was
	// not retained.
	Body json.RawMessage
}

// Error returns a human-readable description of the failure.
func (e *Error) Error() string {
	return fmt.Sprintf("esodm: HTTP %d %s: %s", e.Status, e.Type, e.Reason)
}

// Is classifies HTTP 400, 404 and 409 as validation, missing and conflict errors.
func (e *Error) Is(target error) bool {
	switch target {
	case ErrNotFound:
		return e.Status == http.StatusNotFound
	case ErrConflict:
		return e.Status == http.StatusConflict
	case ErrValidation:
		return e.Status == http.StatusBadRequest
	case ErrTooManyRequests:
		return e.Status == http.StatusTooManyRequests
	case ErrUnavailable:
		return e.Status == 502 || e.Status == 503 || e.Status == 504
	}
	return false
}

// Retryable identifies transient HTTP statuses, not permission to replay a write.
// A transport failure or timed-out write may already have committed.
func (e *Error) Retryable() bool { return e.Is(ErrTooManyRequests) || e.Is(ErrUnavailable) }

// Version is an Elasticsearch server semantic version.
type Version struct {
	// Major, Minor and Patch are nonnegative semantic version components.
	Major, Minor, Patch int
}

// ParseVersion parses three nonnegative version components, ignoring a hyphen suffix.
func ParseVersion(s string) (Version, error) {
	var v Version
	base, _, _ := strings.Cut(s, "-")
	parts := strings.Split(base, ".")
	if len(parts) != 3 {
		return v, fmt.Errorf("%w: invalid version %q", ErrValidation, s)
	}
	n := []*int{&v.Major, &v.Minor, &v.Patch}
	for i, p := range parts {
		x, err := strconv.Atoi(p)
		if err != nil || x < 0 {
			return Version{}, fmt.Errorf("%w: invalid version", ErrValidation)
		}
		*n[i] = x
	}
	return v, nil
}

// String formats the version as major.minor.patch.
func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Event omits request bodies and credentials from its metadata. Err may contain
// sensitive server response details. Observers must be concurrency-safe and prompt.
type Event struct {
	// Operation identifies the endpoint without document IDs or query values.
	Operation string
	// Index identifies the target index when present in the request path.
	Index string
	// Method is the request HTTP method.
	Method string
	// Status is the response HTTP status; zero means no response status was available.
	Status int
	// Duration measures the transport and response processing time, including transport retries.
	Duration time.Duration
	// Err is the request error and may contain sensitive server details. Use Error.Redacted or safe
	// structured logging.
	Err error
}

// Observer receives request completion events synchronously and must be concurrency-safe.
type Observer func(context.Context, Event)

// Config controls ODM response limits and optional request observation.
type Config struct {
	// Now supplies the audit clock; nil selects time.Now. Results are normalized to UTC.
	Now func() time.Time
	// MaxResponseBytes limits the raw response body to 1..1<<40 bytes; zero selects 32 MiB.
	// Decoded values and concurrent requests consume additional memory.
	MaxResponseBytes int64
	// Observer receives completion events; nil disables observation.
	Observer Observer
}

// Client adds document mapping and bounded response handling to an official transport.
// A connected Client may be shared across goroutines.
type Client struct {
	transport Transport
	version   Version
	config    Config
}

// Connect discovers the server version and verifies the supported baseline.
func Connect(ctx context.Context, transport Transport, config Config) (*Client, error) {
	c, err := newClient(ctx, transport, config)
	if err != nil {
		return nil, err
	}
	var info struct {
		Version struct {
			Number string `json:"number"`
		} `json:"version"`
	}
	if err := c.Do(ctx, http.MethodGet, "/", nil, nil, &info); err != nil {
		return nil, err
	}
	v, err := ParseVersion(info.Version.Number)
	if err != nil {
		return nil, err
	}
	if err := validateVersion(v); err != nil {
		return nil, err
	}
	c.version = v
	return c, nil
}

// Version returns the server version discovered during Connect.
func (c *Client) Version() Version {
	return c.version
}

// Transport returns the configured official client transport.
func (c *Client) Transport() Transport {
	return c.transport
}

// Do is the JSON escape hatch. path must be an absolute API path, never a URL.
// No ODM retry is performed: configure retries only in the official transport.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrValidation, err)
		}
	}
	return c.request(ctx, method, path, query, data, "application/json", out)
}
func (c *Client) request(ctx context.Context, method, path string, query url.Values, data []byte, contentType string, out any) (err error) {
	if ctx == nil || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "?#\r\n") {
		return fmt.Errorf("%w: invalid context or API path", ErrValidation)
	}
	u := path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	return c.perform(req, out)
}

// RequestBuilder is implemented by official v8 and v9 typed API builders.
// Use a builder from the same client major as the connected server.
type RequestBuilder interface {
	HttpRequest(context.Context) (*http.Request, error)
}

// DoTyped executes an official typed API builder through this client's transport,
// response-size limit, error classification and observer. It decodes into out;
// pass nil to discard the response. Builders are mutable and must not be shared
// across concurrent calls. The builder's own transport is not used.
func (c *Client) DoTyped(ctx context.Context, builder RequestBuilder, out any) error {
	if ctx == nil || builder == nil || nilValue(builder) {
		return fmt.Errorf("%w: context and typed request required", ErrValidation)
	}
	req, err := builder.HttpRequest(ctx)
	if err != nil {
		return err
	}
	if req == nil || req.URL == nil || req.URL.Host != "" || req.URL.User != nil {
		if req != nil && req.Body != nil {
			_ = req.Body.Close()
		}
		return fmt.Errorf("%w: typed request must use the configured cluster", ErrValidation)
	}
	for _, name := range []string{"Accept", "Content-Type"} {
		for _, value := range strings.Split(req.Header.Get(name), ",") {
			_, params, err := mime.ParseMediaType(strings.TrimSpace(value))
			if err != nil || params["compatible-with"] == "" {
				continue
			}
			major, err := strconv.Atoi(params["compatible-with"])
			if err != nil || major != c.version.Major {
				if req.Body != nil {
					_ = req.Body.Close()
				}
				return fmt.Errorf("%w: builder major %s, server major %d", ErrUnsupported, params["compatible-with"], c.version.Major)
			}
		}
	}
	return c.perform(req, out)
}

func (c *Client) perform(req *http.Request, out any) (err error) {
	ctx, method := req.Context(), req.Method
	operation, index := requestOperation(req)
	var instrumentation elastictransport.Instrumentation
	if transport, ok := c.transport.(elastictransport.Instrumented); ok {
		instrumentation = transport.InstrumentationEnabled()
	}
	if instrumentation != nil {
		ctx = instrumentation.Start(ctx, operation)
		req = req.WithContext(ctx)
		defer instrumentation.Close(ctx)
		if index != "" {
			instrumentation.RecordPathPart(ctx, "index", index)
		}
		instrumentation.BeforeRequest(telemetryRequest(req, operation), operation)
		defer func() {
			instrumentation.AfterRequest(telemetryRequest(req, operation), "elasticsearch", operation)
			if err != nil {
				instrumentation.RecordError(ctx, errors.New("elasticsearch request failed"))
			}
		}()
	}
	start := time.Now()
	status := 0
	defer func() {
		if c.config.Observer != nil {
			c.config.Observer(ctx, Event{Method: method, Status: status, Duration: time.Since(start), Err: err, Operation: operation, Index: index})
		}
	}()
	resp, err := c.transport.Perform(req)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return err
	}
	if resp == nil || resp.Body == nil {
		return errors.New("esodm: transport returned an empty response")
	}
	defer func() { _ = resp.Body.Close() }()
	status = resp.StatusCode
	b, err := io.ReadAll(io.LimitReader(resp.Body, c.config.MaxResponseBytes+1))
	if err != nil {
		return err
	}
	if int64(len(b)) > c.config.MaxResponseBytes {
		return ErrResponseTooLarge
	}
	if status < 200 || status >= 300 {
		e := &Error{Status: status, Body: append(json.RawMessage(nil), b...)}
		var envelope struct {
			Error json.RawMessage `json:"error"`
		}
		if json.Unmarshal(b, &envelope) == nil {
			if len(envelope.Error) > 0 && envelope.Error[0] == '{' && json.Unmarshal(envelope.Error, &e.Cause) == nil {
				e.Type = e.Cause.Type
				if e.Cause.Reason != nil {
					e.Reason = *e.Cause.Reason
				}
			} else {
				_ = json.Unmarshal(envelope.Error, &e.Reason)
			}
		}
		return e
	}
	if out == nil || method == http.MethodHead {
		return nil
	}
	if len(b) == 0 {
		return errors.New("esodm: missing JSON response")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err = dec.Decode(out); err != nil {
		return fmt.Errorf("esodm: decode response: %w", err)
	}
	if dec.Decode(new(any)) != io.EOF {
		return errors.New("esodm: trailing response data")
	}
	return nil
}

func segment(s string) string {
	if s == "." {
		return "%2E"
	}
	if s == ".." {
		return "%2E%2E"
	}
	return url.PathEscape(s)
}
func nilValue(t any) bool {
	if t == nil {
		return true
	}
	v := reflect.ValueOf(t)
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Func, reflect.Interface, reflect.Slice, reflect.Chan:
		return v.IsNil()
	}
	return false
}
func validateID(id string) error {
	if id == "" || len(id) > 512 || !utf8.ValidString(id) {
		return fmt.Errorf("%w: document ID must be valid UTF-8 and contain 1..512 bytes", ErrValidation)
	}
	return nil
}
func validateName(s string) error {
	if s == "" || s == "." || s == ".." || strings.ContainsAny(s, "/\\?#\r\n\x00,* ") {
		return fmt.Errorf("%w: invalid resource name %q", ErrValidation, s)
	}
	return nil
}

// Compare orders semantic versions and returns -1, 0 or 1.
func (v Version) Compare(other Version) int {
	if n := cmp.Compare(v.Major, other.Major); n != 0 {
		return n
	}
	if n := cmp.Compare(v.Minor, other.Minor); n != 0 {
		return n
	}
	return cmp.Compare(v.Patch, other.Patch)
}

// TransportFunc adapts a function to Transport, useful for deterministic tests.
type TransportFunc func(*http.Request) (*http.Response, error)

// Perform invokes f with the request.
func (f TransportFunc) Perform(req *http.Request) (*http.Response, error) { return f(req) }

func requestOperation(req *http.Request) (string, string) {
	parts := strings.Split(strings.Trim(req.URL.EscapedPath(), "/"), "/")
	if len(parts) == 1 {
		switch parts[0] {
		case "":
			return "info", ""
		case "_bulk":
			return "bulk", ""
		case "_msearch":
			return "msearch", ""
		case "_mget":
			return "mget", ""
		case "_search":
			return "search", ""
		case "_pit":
			return "pit.close", ""
		case "_reindex":
			return "reindex", ""
		}
	}
	if len(parts) > 0 && strings.HasPrefix(parts[0], "_") {
		switch parts[0] {
		case "_tasks":
			if len(parts) > 2 && parts[len(parts)-1] == "_cancel" {
				return "tasks.cancel", ""
			}
			return "tasks.get", ""
		case "_aliases":
			return "aliases", ""
		case "_index_template":
			return "template.index", ""
		case "_component_template":
			return "template.component", ""
		case "_data_stream":
			return "data_stream", ""
		case "_ilm":
			return "ilm.policy", ""
		case "_ingest":
			if parts[len(parts)-1] == "_simulate" {
				return "pipeline.simulate", ""
			}
			return "pipeline", ""
		}
	}
	if len(parts) >= 2 && !strings.HasPrefix(parts[0], "_") {
		index, _ := url.PathUnescape(parts[0])
		switch parts[1] {
		case "_update_by_query":
			return "update_by_query", index
		case "_delete_by_query":
			return "delete_by_query", index
		case "_mapping":
			return "mapping", index
		case "_settings":
			return "settings", index
		case "_refresh":
			return "refresh", index
		case "_alias":
			return "aliases", index
		case "_rollover":
			return "rollover", index
		case "_ilm":
			return "ilm.explain", index
		case "_mget":
			return "mget", index
		case "_msearch":
			return "msearch", index
		case "_bulk":
			return "bulk", index
		case "_doc":
			switch req.Method {
			case "GET":
				return "get", index
			case "HEAD":
				return "exists", index
			case "DELETE":
				return "delete", index
			default:
				return "index", index
			}
		case "_create":
			return "create", index
		case "_update":
			return "update", index
		case "_search":
			return "search", index
		case "_count":
			return "count", index
		case "_pit":
			return "pit.open", index
		}
	}
	return "admin", ""
}

// telemetryRequest removes source data, credentials, IDs and query values. The
// official OTel AfterRequest records url.full, so omitting RecordPathPart(id)
// alone would not prevent ID/routing disclosure.
func telemetryRequest(req *http.Request, operation string) *http.Request {
	safe := req.Clone(req.Context())
	safe.URL = &url.URL{Scheme: req.URL.Scheme, Host: req.URL.Host, Path: "/" + operation}
	safe.Header = nil
	safe.Body = nil
	safe.GetBody = nil
	safe.RequestURI = ""
	return safe
}

// LogValue returns safe structured fields. Reason, Body and Cause are omitted
// because Elasticsearch may include document values in all three.
func (e *Error) LogValue() slog.Value {
	if e == nil {
		return slog.AnyValue(nil)
	}
	return slog.GroupValue(slog.Int("status", e.Status), slog.String("type", e.Type))
}

// Redacted returns a copy containing only HTTP status and error type.
func (e *Error) Redacted() *Error {
	if e == nil {
		return nil
	}
	return &Error{Status: e.Status, Type: e.Type}
}

func errorReason(cause *types.ErrorCause) string {
	if cause == nil || cause.Reason == nil {
		return ""
	}
	return *cause.Reason
}

// ConnectVersion uses a caller-verified server version without a discovery request.
// Use this when GET / is forbidden. Authentication and TLS remain transport concerns;
// the declared version must match the cluster and official client major.
func ConnectVersion(ctx context.Context, transport Transport, version Version, config Config) (*Client, error) {
	c, err := newClient(ctx, transport, config)
	if err != nil {
		return nil, err
	}
	if err := validateVersion(version); err != nil {
		return nil, err
	}
	c.version = version
	return c, nil
}

// newClient applies connection-independent validation and defaults once.
func newClient(ctx context.Context, transport Transport, config Config) (*Client, error) {
	if ctx == nil || nilValue(transport) {
		return nil, fmt.Errorf("%w: context and transport required", ErrValidation)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if config.MaxResponseBytes == 0 {
		config.MaxResponseBytes = 32 << 20
	}
	if config.MaxResponseBytes < 1 || config.MaxResponseBytes > 1<<40 {
		return nil, fmt.Errorf("%w: response limit must be 1..1099511627776 bytes", ErrValidation)
	}
	return &Client{transport: transport, config: config}, nil
}

func validateVersion(version Version) error {
	var minimum Version
	switch version.Major {
	case 8:
		minimum = Version{8, 18, 1}
	case 9:
		minimum = Version{9, 4, 5}
	default:
		return fmt.Errorf("%w: Elasticsearch %s; require major 8 or 9", ErrUnsupported, version)
	}
	if version.Minor < 0 || version.Patch < 0 || version.Compare(minimum) < 0 {
		return fmt.Errorf("%w: Elasticsearch %s; require %s or later within major", ErrUnsupported, version, minimum)
	}
	return nil
}
