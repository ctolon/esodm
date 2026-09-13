// Command docgen renders the public API from Go declarations and comments.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/doc"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	check := flag.Bool("check", false, "fail when generated documentation differs")
	flag.Parse()
	if err := run(*check); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(check bool) error {
	var out bytes.Buffer
	out.WriteString("# API reference\n\nGenerated from exported Go declarations. Run `go run ./internal/docgen` with Go 1.27 to update.\n\nStart with the [documentation guide](README.md) for complete usage examples and operational contracts.\n\n")
	packages := []string{".", "adapter/es8", "adapter/es9", "migrationstore", "observe/otel", "esodmtest"}
	for _, dir := range packages {
		fset := token.NewFileSet()
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		var files []*ast.File
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			file, err := parser.ParseFile(fset, filepath.Join(dir, entry.Name()), nil, parser.ParseComments)
			if err != nil {
				return err
			}
			if err := checkPublicFields(file); err != nil {
				return fmt.Errorf("%s: %w", filepath.Join(dir, entry.Name()), err)
			}
			files = append(files, file)
		}
		path := "github.com/ctolon/esodm"
		if dir != "." {
			path += "/" + dir
		}
		p, err := doc.NewFromFiles(fset, files, path, doc.PreserveAST)
		if err != nil {
			return err
		}
		{
			fmt.Fprintf(&out, "## %s\n\n%s\n", path, p.Doc)
			render := func(label, comment string, node any) error {
				if strings.TrimSpace(comment) == "" {
					return fmt.Errorf("missing public comment: %s.%s", path, label)
				}
				fmt.Fprintf(&out, "### %s\n\n%s\n```go\n", label, comment)
				if err := format.Node(&out, fset, node); err != nil {
					return err
				}
				out.WriteString("\n```\n\n")
				return nil
			}
			value := func(v *doc.Value) error {
				// Grouped declarations carry their comments on the individual specs.
				comment := v.Doc
				if strings.TrimSpace(comment) == "" {
					comment = "Values of " + strings.Join(v.Names, ", ") + ".\n"
				}
				return render(strings.Join(v.Names, ", "), comment, v.Decl)
			}
			fn := func(f *doc.Func) error {
				decl := *f.Decl
				decl.Body = nil
				decl.Doc = nil
				label := f.Name
				if f.Recv != "" {
					label = f.Recv + "." + f.Name
				}
				if strings.Contains(f.Decl.Name.Name, "Repository") && strings.Contains(f.Recv, "Client") {
					f.Doc += "\nAvailable with Go 1.27.\n"
				}
				if f.Name == "Load" && strings.Contains(f.Recv, "Client") {
					f.Doc += "\nAvailable with Go 1.27.\n"
				}
				return render(label, f.Doc, &decl)
			}
			for _, v := range p.Consts {
				if err := value(v); err != nil {
					return err
				}
			}
			for _, v := range p.Vars {
				if err := value(v); err != nil {
					return err
				}
			}
			for _, f := range p.Funcs {
				if err := fn(f); err != nil {
					return err
				}
			}
			for _, t := range p.Types {
				// Remove private implementation fields from the displayed declaration.
				ast.FilterDecl(t.Decl, ast.IsExported)
				if err := render(t.Name, t.Doc, t.Decl); err != nil {
					return err
				}
				for _, v := range t.Consts {
					if err := value(v); err != nil {
						return err
					}
				}
				for _, v := range t.Vars {
					if err := value(v); err != nil {
						return err
					}
				}
				for _, f := range t.Funcs {
					if err := fn(f); err != nil {
						return err
					}
				}
				for _, f := range t.Methods {
					if err := fn(f); err != nil {
						return err
					}
				}
			}
		}
	}
	output := append(bytes.TrimRight(out.Bytes(), "\n"), '\n')
	destination := filepath.Join("docs", "api.md")
	if check {
		existing, err := os.ReadFile(destination)
		if err != nil {
			return err
		}
		if !bytes.Equal(existing, output) {
			return fmt.Errorf("%s is stale; run go run ./internal/docgen", destination)
		}
		return nil
	}
	return os.WriteFile(destination, output, 0644)
}

// checkPublicFields checks named public struct fields, including anonymous response
// structs. Embedded fields inherit the contract of their documented public type.
func checkPublicFields(file *ast.File) error {
	var problems []error
	var fields func(string, *ast.StructType)
	fields = func(owner string, st *ast.StructType) {
		for _, field := range st.Fields.List {
			for _, name := range field.Names {
				if !ast.IsExported(name.Name) {
					continue
				}
				path := owner + "." + name.Name
				if field.Doc == nil || strings.TrimSpace(field.Doc.Text()) == "" {
					problems = append(problems, fmt.Errorf("missing public field comment: %s", path))
				}
				if nested, ok := field.Type.(*ast.StructType); ok {
					fields(path, nested)
				}
			}
		}
	}
	for _, decl := range file.Decls {
		general, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range general.Specs {
			typ, ok := spec.(*ast.TypeSpec)
			if !ok || !ast.IsExported(typ.Name.Name) {
				continue
			}
			if st, ok := typ.Type.(*ast.StructType); ok {
				fields(typ.Name.Name, st)
			}
		}
	}
	return errors.Join(problems...)
}
