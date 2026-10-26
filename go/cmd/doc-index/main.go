// SPDX-License-Identifier: LGPL-3.0-only
// Command doc-index extracts handwritten Go declarations for documentation and comment coverage checks.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// declaration preserves a source signature and its adjacent purpose/usage documentation.
type declaration struct {
	Name      string `json:"name"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	Public    bool   `json:"public"`
	Kind      string `json:"kind"`
	Signature string `json:"signature"`
	Doc       string `json:"doc"`
}

// signature prints syntax without a function body so implementation statements cannot masquerade as API.
func signature(set *token.FileSet, node any) string {
	var b bytes.Buffer
	_ = printer.Fprint(&b, set, node)
	return b.String()
}

// main walks selected source trees and emits declarations without generated or test helpers.
func main() {
	root := flag.String("root", ".", "repository root")
	flag.Parse()
	set := token.NewFileSet()
	records := []declaration{}
	for _, dir := range []string{"go", "examples/minimal-server"} {
		err := filepath.WalkDir(filepath.Join(*root, dir), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if entry.Name() == "gen" || entry.Name() == "build" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			file, err := parser.ParseFile(set, path, data, parser.ParseComments)
			if err != nil {
				return err
			}
			// Use Go's header marker rather than matching examples or string literals inside handwritten tools.
			if ast.IsGenerated(file) {
				return nil
			}
			rel, _ := filepath.Rel(*root, path)
			for _, decl := range file.Decls {
				switch node := decl.(type) {
				case *ast.FuncDecl:
					copy := *node
					copy.Body = nil
					name := node.Name.Name
					if node.Recv != nil {
						name = signature(set, node.Recv.List[0].Type) + "." + name
					}
					records = append(records, declaration{name, filepath.ToSlash(rel), set.Position(node.Pos()).Line, ast.IsExported(node.Name.Name), "method", signature(set, &copy), node.Doc.Text()})
				case *ast.GenDecl:
					// Standard AST filtering keeps constants/errors and public fields while hiding implementation state.
					ast.FileExports(&ast.File{Decls: []ast.Decl{node}})
					for _, spec := range node.Specs {
						if value, ok := spec.(*ast.ValueSpec); ok {
							doc := node.Doc.Text()
							if value.Doc != nil {
								doc = value.Doc.Text()
							}
							for _, name := range value.Names {
								records = append(records, declaration{name.Name, filepath.ToSlash(rel), set.Position(value.Pos()).Line, true, "value", node.Tok.String() + " " + signature(set, value), doc})
							}
							continue
						}
						typeSpec, ok := spec.(*ast.TypeSpec)
						if !ok || !ast.IsExported(typeSpec.Name.Name) {
							continue
						}
						doc := node.Doc.Text()
						if typeSpec.Doc != nil {
							doc = typeSpec.Doc.Text()
						}
						records = append(records, declaration{typeSpec.Name.Name, filepath.ToSlash(rel), set.Position(spec.Pos()).Line, true, "type", "type " + signature(set, typeSpec), doc})
						if contract, ok := typeSpec.Type.(*ast.InterfaceType); ok {
							for _, method := range contract.Methods.List {
								for _, name := range method.Names {
									records = append(records, declaration{typeSpec.Name.Name + "." + name.Name, filepath.ToSlash(rel), set.Position(method.Pos()).Line, true, "method", name.Name + strings.TrimPrefix(signature(set, method.Type), "func"), method.Doc.Text()})
								}
							}
						}
					}
				}
			}
			return nil
		})
		if err != nil {
			panic(err)
		}
	}
	if err := json.NewEncoder(os.Stdout).Encode(records); err != nil {
		panic(err)
	}
}
