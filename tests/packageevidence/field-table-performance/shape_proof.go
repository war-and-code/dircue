//go:build ignore

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strconv"
)

type shapes struct{ types, variables map[string]*ast.StructType }

func load(paths ...string) shapes {
	s := shapes{map[string]*ast.StructType{}, map[string]*ast.StructType{}}
	for _, path := range paths {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			panic(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.TypeSpec:
				if shape, ok := n.Type.(*ast.StructType); ok {
					s.types[n.Name.Name] = shape
				}
			case *ast.ValueSpec:
				if shape, ok := n.Type.(*ast.StructType); ok {
					for _, name := range n.Names {
						s.variables[name.Name] = shape
					}
				}
			}
			return true
		})
	}
	return s
}
func (s shapes) describe(expression ast.Expr) any {
	switch e := expression.(type) {
	case *ast.Ident:
		if shape, exists := s.types[e.Name]; exists {
			return s.describe(shape)
		}
		return e.Name
	case *ast.StructType:
		fields := []any{}
		for _, field := range e.Fields.List {
			names := []string{}
			for _, name := range field.Names {
				names = append(names, name.Name)
			}
			tag := ""
			if field.Tag != nil {
				var err error
				tag, err = strconv.Unquote(field.Tag.Value)
				if err != nil {
					panic(err)
				}
			}
			fields = append(fields, map[string]any{"names": names, "type": s.describe(field.Type), "tag": tag})
		}
		return map[string]any{"struct": fields}
	case *ast.ArrayType:
		kind := "slice"
		if e.Len != nil {
			var b bytes.Buffer
			_ = format.Node(&b, token.NewFileSet(), e.Len)
			kind = "array " + b.String()
		}
		return map[string]any{kind: s.describe(e.Elt)}
	case *ast.MapType:
		return map[string]any{"map_key": s.describe(e.Key), "map_value": s.describe(e.Value)}
	case *ast.StarExpr:
		return map[string]any{"pointer": s.describe(e.X)}
	default:
		var b bytes.Buffer
		_ = format.Node(&b, token.NewFileSet(), expression)
		return b.String()
	}
}
func main() {
	oldPath := flag.String("before", "", "Baseline import.go source")
	newPath := flag.String("after", "pkg/packageevidence/import.go", "Candidate import.go")
	fieldsPath := flag.String("fields", "pkg/packageevidence/fields.go", "Candidate fixed tables")
	flag.Parse()
	old, next := load(*oldPath), load(*newPath, *fieldsPath)
	schema := old.types["nativeDocument"].Fields.List[5].Type
	pairs := []struct {
		name string
		old  ast.Expr
	}{{"nativeDocument", old.types["nativeDocument"]}, {"nativeSchema", schema}, {"nativeDescriptor", old.variables["descriptor"]}, {"nativeSource", old.variables["source"]}, {"nativePackage", old.types["nativePackage"]}, {"nativeLocation", old.types["nativeLocation"]}, {"nativeFile", old.variables["file"]}, {"nativeRelationship", old.variables["row"]}}
	proofs := map[string]any{}
	for _, pair := range pairs {
		before, after := old.describe(pair.old), next.describe(next.types[pair.name])
		if !reflect.DeepEqual(before, after) {
			fmt.Fprintln(os.Stderr, "shape changed:", pair.name)
			os.Exit(1)
		}
		proofs[pair.name] = before
	}
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"equal": true, "method": "Go AST field order, names, recursively expanded native field types and exact struct tags; only private type names differ", "shapes": proofs}); err != nil {
		panic(err)
	}
}
