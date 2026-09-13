package esodm

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types/enums/dynamicmapping"
)

// FieldMapping describes a mapping property, including nested objects and vectors.
type FieldMapping struct {
	// DocValues enables on-disk columnar values; nil uses the field type default. Some types,
	// including text, do not support this option.
	DocValues *bool `json:"doc_values,omitempty"`
	// IgnoreAbove is a nonnegative string length threshold; nil omits it. Elasticsearch validates
	// support for the selected type.
	IgnoreAbove *int `json:"ignore_above,omitempty"`
	// NullValue is a JSON scalar replacement for explicit null; nil omits it. It does not replace
	// missing fields or change _source.
	NullValue json.RawMessage `json:"null_value,omitempty"`
	// CopyTo lists destination field names; nil or empty omits it. Values are copied for indexing,
	// not into _source.
	CopyTo []string `json:"copy_to,omitempty"`
	// Dynamic controls unmapped child fields: strict, true, false or runtime. Nil inherits the
	// enclosing mapping behavior.
	Dynamic *dynamicmapping.DynamicMapping `json:"dynamic,omitempty"`
	// InferenceID names an existing inference endpoint for semantic_text; empty omits it. Endpoint
	// availability and permissions are server concerns.
	InferenceID string `json:"inference_id,omitempty"`
	// Type is the Elasticsearch mapping type, such as keyword, text, long, object, nested, join or
	// dense_vector. Inferred schemas require a nonempty type; the server validates type names.
	Type string `json:"type,omitempty"`
	// Analyzer selects a built-in or index-configured indexing analyzer; empty uses the server
	// default for the type.
	Analyzer string `json:"analyzer,omitempty"`
	// SearchAnalyzer selects the search-time analyzer; empty leaves the server default unchanged.
	SearchAnalyzer string `json:"search_analyzer,omitempty"`
	// Format is an Elasticsearch date format or ||-separated alternatives; empty uses the field type
	// default. It is not a Go time layout.
	Format string `json:"format,omitempty"`
	// Properties defines object or nested child mappings. Nil or empty omits children; inferred
	// schemas reject mapping depth greater than 32.
	Properties map[string]FieldMapping `json:"properties,omitempty"`
	// Fields defines named multi-fields, such as a keyword subfield of text. Nil or empty omits
	// multi-fields.
	Fields map[string]FieldMapping `json:"fields,omitempty"`
	// Dims is required for inferred dense_vector mappings and must be 1..4096. Zero omits the
	// property for other types.
	Dims int `json:"dims,omitempty"`
	// Similarity selects a server-supported similarity: dense vectors commonly use cosine,
	// dot_product, l2_norm or max_inner_product; text can name a configured similarity. Empty uses
	// the server default.
	Similarity string `json:"similarity,omitempty"`
	// Index controls indexing of the field; nil uses the field type default. False does not remove
	// values from _source.
	Index *bool `json:"index,omitempty"`
	// Relations maps parent names to a child string, []string or []any of strings. Required for a
	// root join field; names must be nonempty and a child must differ from its parent.
	Relations map[string]any `json:"relations,omitempty"`
}

// SchemaConfig controls tag-based inference and explicit mapping overrides.
// Use SchemaFromMapping for the complete official mapping API.
type SchemaConfig struct {
	// Properties overrides inferred mappings. An explicit conflicting es tag is an error.
	Properties map[string]FieldMapping
	// Settings contains JSON-serializable Elasticsearch index settings. Nil serializes as null and
	// omits settings during index creation; NewSchema snapshots the map.
	Settings map[string]any
	// Dynamic accepts strict, true, false or runtime; empty defaults to DynamicStrict.
	Dynamic Dynamic
}

// Schema is an immutable document mapping and index configuration for T.
type Schema[T any] struct {
	index      string
	mapping    json.RawMessage
	settings   json.RawMessage
	hasVectors bool
	join       *joinInfo
}

// Index returns the index or alias used by repositories built from the schema.
func (s *Schema[T]) Index() string {
	return s.index
}

// Mapping returns a copy of the serialized Elasticsearch type mapping.
func (s *Schema[T]) Mapping() json.RawMessage {
	return append(json.RawMessage(nil), s.mapping...)
}

// Settings returns a copy of the serialized index settings, or JSON null.
func (s *Schema[T]) Settings() json.RawMessage {
	return append(json.RawMessage(nil), s.settings...)
}

// NewSchema infers a mapping from a struct and its json/es tags.
// It defaults to strict dynamic mapping and rejects recursive embedded objects.
func NewSchema[T any](index string, config ...SchemaOption) (*Schema[T], error) {
	if err := validateIndexName(index); err != nil {
		return nil, err
	}
	cfg := SchemaConfig{Dynamic: DynamicStrict}
	for _, option := range config {
		if option == nil || nilValue(option) {
			return nil, fmt.Errorf("%w: nil schema option", ErrValidation)
		}
		option.applySchema(&cfg)
	}
	if cfg.Dynamic == "" {
		cfg.Dynamic = DynamicStrict
	}
	if cfg.Dynamic != "strict" && cfg.Dynamic != "true" && cfg.Dynamic != "false" && cfg.Dynamic != "runtime" {
		return nil, fmt.Errorf("%w: invalid dynamic mapping", ErrValidation)
	}
	t := reflect.TypeFor[T]()
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%w: document T must be a struct", ErrValidation)
	}
	props, explicit, err := inferProperties(t, map[reflect.Type]bool{}, 0)
	if err != nil {
		return nil, err
	}
	for name, field := range cfg.Properties {
		if _, ok := props[name]; !ok {
			return nil, fmt.Errorf("%w: mapping field %s not in document", ErrValidation, name)
		}
		if prior, ok := explicit[name]; ok {
			priorJSON, _ := json.Marshal(prior)
			fieldJSON, err := json.Marshal(field)
			if err != nil {
				return nil, fmt.Errorf("%w: %w", ErrValidation, err)
			}
			var tagged, override map[string]json.RawMessage
			_ = json.Unmarshal(priorJSON, &tagged)
			_ = json.Unmarshal(fieldJSON, &override)
			for key, value := range tagged {
				if !jsonEqual(value, override[key]) {
					return nil, fmt.Errorf("%w: conflicting tag %s for %s", ErrValidation, key, name)
				}
			}
		}
		props[name] = field
	}
	if err := validateStampFields[T](props); err != nil {
		return nil, err
	}
	if err := validateProperties(props, 0); err != nil {
		return nil, err
	}
	var dynamic any = cfg.Dynamic
	if cfg.Dynamic == "true" {
		dynamic = true
	}
	if cfg.Dynamic == "false" {
		dynamic = false
	}
	mapping, err := json.Marshal(map[string]any{"properties": props, "dynamic": dynamic})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrValidation, err)
	}
	settings, err := json.Marshal(cfg.Settings)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrValidation, err)
	}
	join, err := compileJoin(props)
	if err != nil {
		return nil, err
	}
	return &Schema[T]{index: index, mapping: mapping, settings: settings, hasVectors: containsVectors(props), join: join}, nil
}

func containsVectors(props map[string]FieldMapping) bool {
	for _, f := range props {
		if f.Type == "dense_vector" || f.Type == "sparse_vector" || f.Type == "rank_vectors" || containsVectors(f.Properties) {
			return true
		}
	}
	return false
}

func parseTag(s string) (FieldMapping, bool, error) {
	f := FieldMapping{}
	if s == "-" {
		return f, true, nil
	}
	seen := map[string]bool{}
	for _, item := range strings.Split(s, ",") {
		if item == "" {
			continue
		}
		key, value, ok := strings.Cut(item, "=")
		if !ok || value == "" || seen[key] {
			return f, false, fmt.Errorf("%w: invalid es tag %q", ErrValidation, item)
		}
		seen[key] = true
		switch key {
		case "type":
			f.Type = value
		case "analyzer":
			f.Analyzer = value
		case "search_analyzer":
			f.SearchAnalyzer = value
		case "format":
			f.Format = value
		case "index", "doc_values":
			if value != "true" && value != "false" {
				return f, false, fmt.Errorf("%w: %s must be true or false", ErrValidation, key)
			}
			enabled := value == "true"
			if key == "index" {
				f.Index = &enabled
			} else {
				f.DocValues = &enabled
			}
		case "ignore_above", "dims":
			n, err := strconv.Atoi(value)
			if err != nil || n < 0 {
				return f, false, fmt.Errorf("%w: invalid %s", ErrValidation, key)
			}
			if key == "dims" {
				f.Dims = n
			} else {
				f.IgnoreAbove = &n
			}
		case "similarity":
			f.Similarity = value
		case "inference_id":
			f.InferenceID = value
		case "copy_to":
			f.CopyTo = strings.Split(value, "|")
			for _, field := range f.CopyTo {
				if field == "" {
					return f, false, fmt.Errorf("%w: empty copy_to field", ErrValidation)
				}
			}
		case "fields":
			if value != "keyword" {
				return f, false, fmt.Errorf("%w: fields shortcut accepts keyword", ErrValidation)
			}
			f.Fields = map[string]FieldMapping{"keyword": {Type: "keyword"}}
		case "dynamic":
			if value != "strict" && value != "true" && value != "false" && value != "runtime" {
				return f, false, fmt.Errorf("%w: invalid dynamic mapping", ErrValidation)
			}
			f.Dynamic = &dynamicmapping.DynamicMapping{Name: value}
		case "null_value":
			var scalar any
			if err := json.Unmarshal([]byte(value), &scalar); err != nil {
				return f, false, fmt.Errorf("%w: null_value must be a JSON scalar", ErrValidation)
			}
			switch scalar.(type) {
			case map[string]any, []any:
				return f, false, fmt.Errorf("%w: null_value must be a JSON scalar", ErrValidation)
			}
			f.NullValue = json.RawMessage(value)
		default:
			return f, false, fmt.Errorf("%w: unknown es tag %s", ErrValidation, key)
		}
	}
	return f, false, nil
}
func inferProperties(t reflect.Type, seen map[reflect.Type]bool, depth int) (map[string]FieldMapping, map[string]FieldMapping, error) {
	if depth > 32 || seen[t] {
		return nil, nil, fmt.Errorf("%w: recursive or overly deep document %s", ErrValidation, t)
	}
	seen[t] = true
	defer delete(seen, t)
	props, explicit := map[string]FieldMapping{}, map[string]FieldMapping{}
	fields, err := jsonFields(t)
	if err != nil {
		return nil, nil, err
	}
	for _, selected := range fields {
		f, name := selected.StructField, selected.name
		tag, _, err := parseTag(f.Tag.Get("es"))
		if err != nil {
			return nil, nil, err
		}
		ft := f.Type
		binary := false
		for ft.Kind() == reflect.Pointer || ft.Kind() == reflect.Slice || ft.Kind() == reflect.Array {
			// encoding/json represents byte slices as base64 strings, including
			// through pointers and outer containers. Byte arrays remain numeric arrays.
			if ft.Kind() == reflect.Slice && ft.Elem().Kind() == reflect.Uint8 {
				binary = true
				break
			}
			ft = ft.Elem()
		}
		inferred := tag
		if binary {
			if inferred.Type == "" {
				inferred.Type = "binary"
			}
		} else if ft == reflect.TypeFor[types.LatLonGeoLocation]() {
			if inferred.Type == "" {
				inferred.Type = "geo_point"
			}
		} else if ft == reflect.TypeFor[time.Time]() {
			if inferred.Type == "" {
				inferred.Type = "date"
			}
		} else {
			switch ft.Kind() {
			case reflect.String:
				if inferred.Type == "" {
					inferred.Type = "keyword"
				}
			case reflect.Bool:
				if inferred.Type == "" {
					inferred.Type = "boolean"
				}
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				if inferred.Type == "" {
					inferred.Type = "long"
				}
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				if inferred.Type == "" {
					inferred.Type = "unsigned_long"
				}
			case reflect.Float32, reflect.Float64:
				if inferred.Type == "" {
					inferred.Type = "double"
				}
			case reflect.Struct:
				if inferred.Type == "" {
					inferred.Type = "object"
				}
				if inferred.Type == "object" || inferred.Type == "nested" {
					inferred.Properties, _, err = inferProperties(ft, seen, depth+1)
					if err != nil {
						return nil, nil, err
					}
				}
			case reflect.Map, reflect.Interface:
				if inferred.Type == "" {
					return nil, nil, fmt.Errorf("%w: %s needs an explicit es type", ErrValidation, name)
				}
			default:
				return nil, nil, fmt.Errorf("%w: unsupported field %s", ErrValidation, name)
			}
		}
		props[name] = inferred
		if f.Tag.Get("es") != "" {
			explicit[name] = tag
		}
	}
	return props, explicit, nil
}
func validateProperties(props map[string]FieldMapping, depth int) error {
	if depth > 32 {
		return fmt.Errorf("%w: mapping depth exceeds 32", ErrValidation)
	}
	joins := 0
	for name, f := range props {
		if name == "" || f.Type == "" {
			return fmt.Errorf("%w: mapping field/type required", ErrValidation)
		}
		if f.Type == "join" {
			joins++
			if depth != 0 || len(f.Relations) == 0 {
				return fmt.Errorf("%w: join requires root relations", ErrValidation)
			}
		}
		if f.Type == "dense_vector" && (f.Dims < 1 || f.Dims > 4096) {
			return fmt.Errorf("%w: vector dims must be 1..4096", ErrValidation)
		}
		if err := validateProperties(f.Properties, depth+1); err != nil {
			return err
		}
		if err := validateProperties(f.Fields, depth+1); err != nil {
			return err
		}
	}
	if joins > 1 {
		return fmt.Errorf("%w: only one join field per index", ErrValidation)
	}
	return nil
}

// SchemaFromMapping creates a schema from the official mapping and settings
// types without reflection-based field inference. It snapshots the inputs and
// lets Elasticsearch validate mapping options. T must be a struct. Use this
// constructor for mappings beyond the tag-based convenience API.
func SchemaFromMapping[T any](index string, mapping *types.TypeMapping, settings *types.IndexSettings) (*Schema[T], error) {
	if err := validateIndexName(index); err != nil {
		return nil, err
	}
	if reflect.TypeFor[T]().Kind() != reflect.Struct || mapping == nil {
		return nil, fmt.Errorf("%w: struct document and mapping required", ErrValidation)
	}
	data, err := json.Marshal(mapping)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrValidation, err)
	}
	config, err := json.Marshal(settings)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrValidation, err)
	}
	var properties struct {
		Properties map[string]FieldMapping `json:"properties"`
	}
	if err := json.Unmarshal(data, &properties); err != nil {
		return nil, err
	}
	if err := validateStampFields[T](properties.Properties); err != nil {
		return nil, err
	}
	join, err := compileJoin(properties.Properties)
	if err != nil {
		return nil, err
	}
	return &Schema[T]{index: index, mapping: data, settings: config, hasVectors: containsVectors(properties.Properties), join: join}, nil
}

func validateStampFields[T any](props map[string]FieldMapping) error {
	var sample T
	initializeEmbeds(reflect.ValueOf(&sample).Elem(), 0)
	if stamper, ok := any(&sample).(Stamper); ok {
		created, updated := stamper.StampFields()
		for _, name := range []string{created, updated} {
			if name == "" {
				continue
			}
			field, exists := props[name]
			if !exists || (field.Type != "date" && field.Type != "date_nanos") {
				return fmt.Errorf("%w: stamp field %q must be a top-level date", ErrValidation, name)
			}
		}
	}

	return nil
}
