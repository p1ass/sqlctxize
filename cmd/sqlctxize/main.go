package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io/ioutil"
	"os"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Println("Usage: converter <directory-path>")
		return
	}
	dirpath := os.Args[1]

	files, err := ioutil.ReadDir(dirpath)
	if err != nil {
		fmt.Println("Failed to read directory:", err)
		return
	}

	fset := token.NewFileSet()
	parsedFiles := []*ast.File{}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		filename := file.Name()
		if ext := ".go"; len(filename) > len(ext) && filename[len(filename)-len(ext):] == ext {
			path := dirpath + "/" + filename
			node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if err != nil {
				fmt.Printf("Failed to parse the file %s: %v\n", path, err)
				return
			}
			parsedFiles = append(parsedFiles, node)
		}
	}

	conf := types.Config{Importer: importer.Default()}
	info := &types.Info{
		Types: map[ast.Expr]types.TypeAndValue{},
	}
	_, err = conf.Check(dirpath, fset, parsedFiles, info)
	if err != nil {
		fmt.Println("Failed to type check:", err)
		return
	}

	contextMethods := map[string]string{
		"Query":    "QueryContext",
		"Exec":     "ExecContext",
		"Prepare":  "PrepareContext",
		"QueryRow": "QueryRowContext",
	}

	for _, file := range parsedFiles {
		ast.Inspect(file, func(n ast.Node) bool {
			if callExpr, ok := n.(*ast.CallExpr); ok {
				if selExpr, ok := callExpr.Fun.(*ast.SelectorExpr); ok {
					tv, ok := info.Types[selExpr.X]
					if ok && tv.Type != nil {
						if ptr, ok := tv.Type.(*types.Pointer); ok {
							if named, ok := ptr.Elem().(*types.Named); ok {
								if named.Obj().Pkg().Path() == "database/sql" && named.Obj().Name() == "DB" {
									if newMethod, exists := contextMethods[selExpr.Sel.Name]; exists {
										selExpr.Sel.Name = newMethod
										args := []ast.Expr{
											&ast.CallExpr{
												Fun: &ast.SelectorExpr{
													X:   ast.NewIdent("context"),
													Sel: ast.NewIdent("Background"),
												},
											},
										}
										args = append(args, callExpr.Args...)
										callExpr.Args = args
									}
								}
							}
						}
					}
				}
			}
			return true
		})

		// ここで変更されたファイル内容を出力するか、ファイルに上書き保存することができます。
		// この例では、変更された内容を標準出力に表示します。

		var buf bytes.Buffer
		err = format.Node(&buf, fset, file)
		if err != nil { /* エラー処理 */
		}
		// v + 1
		fmt.Println(buf.String())
	}
}
