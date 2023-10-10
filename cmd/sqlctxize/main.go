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
			switch node := n.(type) {
			case *ast.CallExpr:
				if selExpr, ok := node.Fun.(*ast.SelectorExpr); ok {
					tv, ok := info.Types[selExpr.X]
					if !ok || tv.Type == nil {
						return true
					}
					if ptr, ok := tv.Type.(*types.Pointer); ok {
						if named, ok := ptr.Elem().(*types.Named); ok {
							if named.Obj().Pkg().Path() == "database/sql" && named.Obj().Name() == "DB" {
								if newMethod, exists := contextMethods[selExpr.Sel.Name]; exists {
									selExpr.Sel.Name = newMethod

									if ident, isIdent := selExpr.X.(*ast.Ident); isIdent && ident.Obj != nil && ident.Obj.Decl != nil {
										if funDecl, isFuncDecl := ident.Obj.Decl.(*ast.FuncDecl); isFuncDecl {
											ctxExpr := ast.NewIdent("ctx")
											if isCtxAvailable(funDecl) {
												node.Args = append([]ast.Expr{ctxExpr}, node.Args...)
											} else {
												addContextParam(funDecl.Type)
												node.Args = append([]ast.Expr{&ast.CallExpr{
													Fun: &ast.SelectorExpr{
														X:   ast.NewIdent("context"),
														Sel: ast.NewIdent("Background"),
													},
												}}, node.Args...)
											}
										}
									}
								}
							}
						}
					}
				}
			case *ast.FuncDecl:
				if !isCtxAvailable(node) {
					addContextParam(node.Type)
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
		fmt.Println(buf.String())
	}
}

func isCtxAvailable(funDecl *ast.FuncDecl) bool {
	for _, param := range funDecl.Type.Params.List {
		for _, name := range param.Names {
			if name.Name == "ctx" {
				return true
			}
		}
	}
	return false
}

func addContextParam(fun *ast.FuncType) {
	if fun.Params == nil {
		fun.Params = &ast.FieldList{}
	}
	ctxField := &ast.Field{
		Names: []*ast.Ident{ast.NewIdent("ctx")},
		Type:  &ast.SelectorExpr{X: ast.NewIdent("context"), Sel: ast.NewIdent("Context")},
	}
	fun.Params.List = append([]*ast.Field{ctxField}, fun.Params.List...)
}
