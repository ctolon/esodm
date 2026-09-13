package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/importer"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"reflect"
	"sort"
	"strings"
	"unicode"
)

type resolvedField struct {
	field  *types.Var
	tag    reflect.StructTag
	name   string
	depth  int
	tagged bool
}

func visibleFields(t *types.Struct) ([]resolvedField, error) {
	candidates := map[string][]resolvedField{}
	var collect func(*types.Struct, int, map[*types.Struct]bool) error
	collect = func(st *types.Struct, depth int, seen map[*types.Struct]bool) error {
		if depth > 32 || seen[st] {
			return fmt.Errorf("recursive/deep embedded model")
		}
		seen[st] = true
		defer delete(seen, st)
		for i := 0; i < st.NumFields(); i++ {
			f := st.Field(i)
			tag := reflect.StructTag(st.Tag(i))
			ft := types.Unalias(f.Type())
			if p, ok := ft.(*types.Pointer); ok {
				ft = types.Unalias(p.Elem())
			}
			nested, isStruct := ft.Underlying().(*types.Struct)
			if !f.Exported() && (!f.Embedded() || !isStruct) {
				continue
			}
			name, _, _ := strings.Cut(tag.Get("json"), ",")
			if tag.Get("es") == "-" && name != "-" {
				return fmt.Errorf("es:- requires json:- on %s", f.Name())
			}
			if name == "-" {
				continue
			}
			if !validTagName(name) {
				name = ""
			}
			if f.Embedded() && name == "" && isStruct {
				if tag.Get("es") != "" {
					return fmt.Errorf("embedded mapping tag requires JSON name")
				}
				if hasJSONMarshaler(f.Type()) {
					return fmt.Errorf("embedded JSON marshaler requires explicit descriptors")
				}
				if err := collect(nested, depth+1, seen); err != nil {
					return err
				}
				continue
			}
			tagged := name != ""
			if name == "" {
				name = f.Name()
			}
			candidates[name] = append(candidates[name], resolvedField{f, tag, name, depth, tagged})
		}
		return nil
	}
	if err := collect(t, 0, map[*types.Struct]bool{}); err != nil {
		return nil, err
	}
	var out []resolvedField
	for name, list := range candidates {
		depth := list[0].depth
		for _, f := range list {
			depth = min(depth, f.depth)
		}
		var fields, tags []resolvedField
		for _, f := range list {
			if f.depth == depth {
				fields = append(fields, f)
				if f.tagged {
					tags = append(tags, f)
				}
			}
		}
		if len(tags) > 0 {
			fields = tags
		}
		if len(fields) != 1 {
			return nil, fmt.Errorf("ambiguous JSON field %s", name)
		}
		out = append(out, fields[0])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}
func validTagName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if !strings.ContainsRune("!#$%&()*+-./:;<=>?@[]^_{|}~ ", r) && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
func hasJSONMarshaler(t types.Type) bool {
	for _, typ := range []types.Type{t, types.NewPointer(t)} {
		if obj, _, _ := types.LookupFieldOrMethod(typ, true, nil, "MarshalJSON"); obj != nil {
			if method, ok := obj.(*types.Func); ok {
				signature := method.Type().(*types.Signature)
				if signature.Params().Len() == 0 && signature.Results().Len() == 2 && types.Identical(signature.Results().At(0).Type(), types.NewSlice(types.Typ[types.Byte])) && types.Identical(signature.Results().At(1).Type(), types.Universe.Lookup("error").Type()) {
					return true
				}
			}
		}
	}
	return false
}

func generateDescriptors(dir string, fset *token.FileSet, files []*ast.File, names, packageName string) (bytes.Buffer, map[string]string, error) {
	var declarations bytes.Buffer
	used := map[string]string{"esodm": "github.com/ctolon/esodm"}
	imports := map[string]bool{}
	for _, f := range files {
		for _, imp := range f.Imports {
			var path string
			_ = json.Unmarshal([]byte(imp.Path.Value), &path)
			if path != "" {
				imports[path] = true
			}
		}
	}
	exports := map[string]string{}
	if len(imports) > 0 {
		args := []string{"list", "-mod=mod", "-e", "-export", "-deps", "-json"}
		var paths []string
		for p := range imports {
			if p != "C" {
				paths = append(paths, p)
			}
		}
		sort.Strings(paths)
		args = append(args, paths...)
		cmd := exec.Command("go", args...)
		cmd.Dir = dir
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		output, err := cmd.Output()
		if err != nil {
			return declarations, nil, fmt.Errorf("load model imports: %w: %s", err, stderr.String())
		}
		dec := json.NewDecoder(bytes.NewReader(output))
		for {
			var p struct{ ImportPath, Export string }
			if err := dec.Decode(&p); err == io.EOF {
				break
			} else if err != nil {
				return declarations, nil, err
			}
			if p.Export != "" {
				exports[p.ImportPath] = p.Export
			}
		}
	}
	lookup := func(path string) (io.ReadCloser, error) {
		if export := exports[path]; export != "" {
			return os.Open(export)
		}
		return nil, fmt.Errorf("no export data for %s; resolve the model module dependencies first", path)
	}
	cfg := types.Config{Importer: importer.ForCompiler(fset, "gc", lookup), IgnoreFuncBodies: true}
	pkg, err := cfg.Check(packageName, fset, files, nil)
	if err != nil {
		return declarations, nil, err
	}
	qualifier := func(p *types.Package) string {
		if p == pkg {
			return ""
		}
		for alias, path := range used {
			if path == p.Path() {
				return alias
			}
		}
		alias := p.Name()
		for i := 2; used[alias] != ""; i++ {
			alias = fmt.Sprintf("%s%d", p.Name(), i)
		}
		used[alias] = p.Path()
		return alias
	}
	var emit func(types.Type, string, map[types.Type]bool) (string, string, error)
	emit = func(t types.Type, path string, seen map[types.Type]bool) (string, string, error) {
		if len(seen) > 32 || seen[t] {
			return "", "", fmt.Errorf("recursive/deep object %s", path)
		}
		seen[t] = true
		defer delete(seen, t)
		st, ok := types.Unalias(t).Underlying().(*types.Struct)
		if !ok {
			return "", "", fmt.Errorf("%s is not a struct", t)
		}
		fields, err := visibleFields(st)
		if err != nil {
			return "", "", err
		}
		var defs, values bytes.Buffer
		if path != "" {
			defs.WriteString("esodm.ObjectField\n")
			fmt.Fprintf(&values, "ObjectField:esodm.Object(%q),\n", path)
		}
		goNames := map[string]bool{}
		if path != "" {
			goNames["ObjectField"] = true
		}
		for _, f := range fields {
			name := f.field.Name()
			if goNames[name] {
				return "", "", fmt.Errorf("descriptor field collision %s", name)
			}
			goNames[name] = true
			child := f.name
			if path != "" {
				child = path + "." + child
			}
			ft := types.Unalias(f.field.Type())
			for {
				switch x := ft.(type) {
				case *types.Pointer:
					ft = types.Unalias(x.Elem())
				case *types.Slice:
					// A byte slice is one base64 value, not a field of numeric bytes.
					if element, ok := types.Unalias(x.Elem()).Underlying().(*types.Basic); ok && element.Kind() == types.Uint8 {
						goto unwrapped
					}
					ft = types.Unalias(x.Elem())
				case *types.Array:
					ft = types.Unalias(x.Elem())
				default:
					goto unwrapped
				}
			}
		unwrapped:
			fieldType, constructor := "", ""
			if named, ok := ft.(*types.Named); ok && named.Obj().Pkg() != nil {
				pkgPath, typeName := named.Obj().Pkg().Path(), named.Obj().Name()
				switch {
				case pkgPath == "time" && typeName == "Time":
					fieldType, constructor = "esodm.DateField", "esodm.Date"
				case pkgPath == "github.com/elastic/go-elasticsearch/v9/typedapi/types" && typeName == "LatLonGeoLocation":
					fieldType, constructor = "esodm.GeoField", "esodm.Geo"
				case pkgPath == "github.com/ctolon/esodm" && typeName == "Ref" && named.TypeArgs().Len() == 1:
					target := types.TypeString(named.TypeArgs().At(0), qualifier)
					fieldType, constructor = "esodm.RelationField["+target+"]", "esodm.Relation["+target+"]"
				}
			}
			var value string
			if fieldType == "" {
				if _, isStruct := ft.Underlying().(*types.Struct); isStruct && !hasJSONMarshaler(ft) {
					fieldType, value, err = emit(ft, child, seen)
					if err != nil {
						return "", "", err
					}
				} else {
					typeName := types.TypeString(ft, qualifier)
					fieldType, constructor = "esodm.Field["+typeName+"]", "esodm.NewField["+typeName+"]"
					if basic, ok := ft.Underlying().(*types.Basic); ok {
						if basic.Kind() == types.String && strings.Contains(","+f.tag.Get("es")+",", ",type=text,") {
							fieldType, constructor = "esodm.TextField", "esodm.Text"
						} else if basic.Info()&(types.IsInteger|types.IsFloat|types.IsString) != 0 {
							fieldType, constructor = "esodm.OrderedField["+typeName+"]", "esodm.OrderedValue["+typeName+"]"
						}
					}
				}
			}
			if value == "" {
				value = fmt.Sprintf("%s(%q)", constructor, child)
			}
			fmt.Fprintf(&defs, "%s %s\n", name, fieldType)
			fmt.Fprintf(&values, "%s:%s,\n", name, value)
		}
		typ := "struct {\n" + defs.String() + "}"
		return typ, typ + "{\n" + values.String() + "}", nil
	}
	seenNames := map[string]bool{}
	for _, name := range strings.Split(names, ",") {
		name = strings.TrimSpace(name)
		if seenNames[name] {
			return declarations, nil, fmt.Errorf("duplicate type %s", name)
		}
		seenNames[name] = true
		obj, ok := pkg.Scope().Lookup(name).(*types.TypeName)
		if !ok {
			return declarations, nil, fmt.Errorf("struct %s not found", name)
		}
		if named, ok := types.Unalias(obj.Type()).(*types.Named); ok && named.TypeParams().Len() > 0 {
			return declarations, nil, fmt.Errorf("generic root models require concrete types")
		}
		_, value, err := emit(obj.Type(), "", map[types.Type]bool{})
		if err != nil {
			return declarations, nil, err
		}
		fmt.Fprintf(&declarations, "// %sFields contains typed descriptors for %s's JSON fields.\nvar %sFields = %s\n", name, name, name, value)
	}
	return declarations, used, nil
}
