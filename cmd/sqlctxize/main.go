package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"golang.org/x/tools/go/packages"
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

	cfg := &packages.Config{
		Mode: packages.LoadSyntax,
		Dir:  dirpath,
	}

	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		fmt.Println("Failed to load packages:", err)
		return
	}

	contextMethods := map[string]string{
		"Query":    "QueryContext",
		"Exec":     "ExecContext",
		"Prepare":  "PrepareContext",
		"QueryRow": "QueryRowContext",
	}

	for _, pkg := range pkgs {
		for _, file := range pkg.Syntax {

			ast.Inspect(file, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.CallExpr:
					// database/sqlのメソッド呼び出しかどうかを判定
					if selExpr, ok := node.Fun.(*ast.SelectorExpr); ok {
						tv, ok := pkg.TypesInfo.Types[selExpr.X]
						if !ok || tv.Type == nil {
							return true
						}
						if ptr, ok := tv.Type.(*types.Pointer); ok {
							if named, ok := ptr.Elem().(*types.Named); ok {
								if named.Obj().Pkg().Path() == "database/sql" && named.Obj().Name() == "DB" {
									if newMethod, exists := contextMethods[selExpr.Sel.Name]; exists {
										selExpr.Sel.Name = newMethod
										ctxExpr := ast.NewIdent("ctx")
										node.Args = append([]ast.Expr{ctxExpr}, node.Args...)
									}
								}
							}
						}
					}

				case *ast.FuncDecl:
					if !isCtxAvailable(node) && !hasHttpParams(node) {
						addContextParam(node.Type)
						modifyFuncCalls(node.Name.Name, file)
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

func hasHttpParams(funDecl *ast.FuncDecl) bool {
	if len(funDecl.Type.Params.List) != 2 {
		return false
	}
	firstParam, secondParam := funDecl.Type.Params.List[0], funDecl.Type.Params.List[1]

	return isSelectorExprOfType(firstParam.Type, "http", "ResponseWriter") && isStarExprOfType(secondParam.Type, "http", "Request")
}

func isSelectorExprOfType(expr ast.Expr, pkg string, name string) bool {
	if sel, ok := expr.(*ast.SelectorExpr); ok {
		if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == pkg && sel.Sel.Name == name {
			return true
		}
	}
	return false
}

func isStarExprOfType(expr ast.Expr, pkg, typeName string) bool {
	if star, ok := expr.(*ast.StarExpr); ok {
		if ident, ok := star.X.(*ast.SelectorExpr); ok {
			return isSelectorExprOfType(ident, pkg, typeName)
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

// 関数の呼び出しを修正するためのヘルパー関数
func modifyFuncCalls(name string, file *ast.File) {
	ast.Inspect(file, func(n ast.Node) bool {
		callExpr, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		ident, ok := callExpr.Fun.(*ast.Ident)
		if !ok || ident.Name != name {
			return true
		}

		// 既にctxが最初の引数として存在しているか確認
		if len(callExpr.Args) > 0 {
			firstArg, ok := callExpr.Args[0].(*ast.Ident)
			if ok && firstArg.Name == "ctx" {
				return true
			}
		}

		// ctxを最初の引数として追加
		ctxExpr := ast.NewIdent("ctx")
		callExpr.Args = append([]ast.Expr{ctxExpr}, callExpr.Args...)
		return true
	})
}
