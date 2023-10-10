package sqlctxize

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/types"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"os"
)

const doc = "sqlctxize is converter for database/sql"

// Analyzer is ...
var Analyzer = &analysis.Analyzer{
	Name: "sqlctxize",
	Doc:  doc,
	Run:  run,
	Requires: []*analysis.Analyzer{
		inspect.Analyzer,
	},
}

func run(pass *analysis.Pass) (any, error) {
	inspect := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	nodeFilter := []ast.Node{
		(*ast.CallExpr)(nil),
	}

	inspect.Preorder(nodeFilter, func(n ast.Node) {
		info := pass.TypesInfo
		if callExpr, ok := n.(*ast.CallExpr); ok {
			if selExpr, ok := callExpr.Fun.(*ast.SelectorExpr); ok {
				tv, ok := info.Types[selExpr.X]
				if ok && tv.Type != nil {
					underlying := tv.Type.Underlying()
					if _, ok := underlying.(*types.Pointer); ok {
						if named, ok := underlying.(*types.Named); ok {
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
	})

	ast.Fprint(os.Stdout, pass.Fset, pass.Files, nil)
	var buf bytes.Buffer
	err := format.Node(&buf, pass.Fset, pass.Files)
	if err != nil { /* エラー処理 */
		return nil, fmt.Errorf("failed to format node: %w", err)
	}
	fmt.Println(buf.String())

	return nil, nil
}
