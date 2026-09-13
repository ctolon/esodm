package esodm

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"unicode"
)

type jsonField struct {
	reflect.StructField
	name   string
	depth  int
	tagged bool
}

// jsonFields resolves promotion before mapping inference. Like encoding/json,
// shallower fields dominate; at equal depth a sole explicitly named field wins.
// Ambiguous names are rejected rather than silently omitted.
func jsonFields(t reflect.Type) ([]jsonField, error) {
	candidates := map[string][]jsonField{}
	var collect func(reflect.Type, []int, map[reflect.Type]bool) error
	collect = func(t reflect.Type, path []int, seen map[reflect.Type]bool) error {
		if len(path) > 32 || seen[t] {
			return fmt.Errorf("%w: recursive or deep embedded type %s", ErrValidation, t)
		}
		seen[t] = true
		defer delete(seen, t)
		for i := range t.NumField() {
			f := t.Field(i)
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if !f.IsExported() && (!f.Anonymous || ft.Kind() != reflect.Struct) {
				continue
			}
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if f.Tag.Get("es") == "-" && name != "-" {
				return fmt.Errorf("%w: es:- requires json:- on %s", ErrValidation, f.Name)
			}
			if name == "-" {
				continue
			}
			if !validJSONTag(name) {
				name = ""
			}
			index := append(slices.Clone(path), i)
			if f.Anonymous && name == "" && ft.Kind() == reflect.Struct {
				if f.Tag.Get("es") != "" {
					return fmt.Errorf("%w: embedded mapping tags require an explicit json name", ErrValidation)
				}
				if ft.Implements(reflect.TypeFor[json.Marshaler]()) || reflect.PointerTo(ft).Implements(reflect.TypeFor[json.Marshaler]()) {
					return fmt.Errorf("%w: embedded JSON marshaler needs an explicit schema", ErrValidation)
				}
				if err := collect(ft, index, seen); err != nil {
					return err
				}
				continue
			}
			tagged := name != ""
			if name == "" {
				name = f.Name
			}
			f.Index = index
			candidates[name] = append(candidates[name], jsonField{StructField: f, name: name, depth: len(path), tagged: tagged})
		}
		return nil
	}
	if err := collect(t, nil, map[reflect.Type]bool{}); err != nil {
		return nil, err
	}
	var fields []jsonField
	for name, group := range candidates {
		depth := group[0].depth
		for _, f := range group {
			depth = min(depth, f.depth)
		}
		var visible, tagged []jsonField
		for _, f := range group {
			if f.depth == depth {
				visible = append(visible, f)
				if f.tagged {
					tagged = append(tagged, f)
				}
			}
		}
		if len(tagged) > 0 {
			visible = tagged
		}
		if len(visible) != 1 {
			return nil, fmt.Errorf("%w: ambiguous JSON field %s", ErrValidation, name)
		}
		fields = append(fields, visible[0])
	}
	slices.SortFunc(fields, func(a, b jsonField) int { return strings.Compare(a.name, b.name) })
	return fields, nil
}

func validJSONTag(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if strings.ContainsRune("!#$%&()*+-./:;<=>?@[]^_{|}~ ", r) || unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		return false
	}
	return true
}
