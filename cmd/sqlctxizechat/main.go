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
	"os"
	"path"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Println("Usage: converter <filepath>")
		return
	}
	filepath := os.Args[1]

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filepath, nil, parser.ParseComments)
	if err != nil {
		fmt.Println("Failed to parse the file:", err)
		return
	}

	conf := types.Config{Importer: importer.Default()}
	info := &types.Info{
		Types: map[ast.Expr]types.TypeAndValue{},
	}
	_, err = conf.Check(path.Dir(filepath), fset, []*ast.File{node}, info)
	if err != nil {
		fmt.Println("Failed to type check:", err)
		return
	}

	ast.Inspect(node, func(n ast.Node) bool {
		if callExpr, ok := n.(*ast.CallExpr); ok {
			if selExpr, ok := callExpr.Fun.(*ast.SelectorExpr); ok {
				tv, ok := info.Types[selExpr.X]
				if ok && tv.Type != nil {
					underlying := tv.Type.Underlying()
					if ptr, ok := underlying.(*types.Pointer); ok {
						if named, ok := ptr.Elem().(*types.Named); ok {
							fmt.Println("named ok")
							if named.Obj().Pkg().Path() == "database/sql" && named.Obj().Name() == "DB" && selExpr.Sel.Name == "Query" {
								selExpr.Sel.Name = "QueryContext"
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
		return true
	})

	// print modified code
	// ast.Fprint(os.Stdout, fset, node, nil)

	var buf bytes.Buffer
	err = format.Node(&buf, fset, node)
	if err != nil { /* エラー処理 */
	}
	// v + 1
	fmt.Println(buf.String())
}
