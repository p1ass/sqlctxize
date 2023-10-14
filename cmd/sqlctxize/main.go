package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"golang.org/x/tools/go/packages"
	"os"
	"strings"
)

var overwrite = flag.Bool("w", false, "overwrite source file")
var dir = flag.String("dir", ".", "treat argument as directory")

var stdDBMethods = map[string]string{
	"Query":    "QueryContext",
	"Exec":     "ExecContext",
	"Prepare":  "PrepareContext",
	"QueryRow": "QueryRowContext",
}

var sqlxMethods = map[string]string{
	"Connect":      "ConnectContext",
	"Get":          "GetContext",
	"MustExec":     "MustExecContext",
	"NamedExec":    "NamedExecContext",
	"NamedQuery":   "NamedQueryContext",
	"PrepareNamed": "PrepareNamedContext",
	"QueryRowx":    "QueryRowxContext",
	"Queryx":       "QueryxContext",
	"Select":       "SelectContext",
}

func main() {
	flag.Parse()
	dirpath := *dir

	files, err := os.ReadDir(dirpath)
	if err != nil {
		fmt.Println("Failed to read directory:", err)
		return
	}

	fset := token.NewFileSet()
	parsedFiles := []*ast.File{}
	parsedFileNames := []string{}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		filename := file.Name()
		if ext := ".go"; len(filename) > len(ext) && filename[len(filename)-len(ext):] == ext {
			p := dirpath + "/" + filename
			node, err := parser.ParseFile(fset, p, nil, parser.ParseComments)
			if err != nil {
				fmt.Printf("Failed to parse the file %s: %v\n", p, err)
				return
			}
			parsedFiles = append(parsedFiles, node)
			parsedFileNames = append(parsedFileNames, filename)
		}
	}

	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedTypes | packages.NeedTypesSizes | packages.NeedSyntax | packages.NeedTypesInfo,
		Dir:  dirpath,
	}

	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		fmt.Println("Failed to load packages:", err)
		return
	}

	for _, pkg := range pkgs {
		if len(pkg.Errors) > 0 {
			fmt.Fprintln(os.Stderr, "Errors loading package:", pkg.Errors)
			continue
		}

		for _, file := range pkg.Syntax {
			file = addContextImport(file)

			ast.Inspect(file, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.CallExpr:
					// database/sqlのメソッド呼び出しかどうかを判定
					if selExpr, ok := node.Fun.(*ast.SelectorExpr); ok {
						tv, ok := pkg.TypesInfo.Types[selExpr.X]
						if ok && tv.Type != nil {
							if ptr, ok := tv.Type.(*types.Pointer); ok {
								if named, ok := ptr.Elem().(*types.Named); ok {
									if named.Obj().Pkg().Path() == "database/sql" && named.Obj().Name() == "DB" {
										if newMethod, exists := stdDBMethods[selExpr.Sel.Name]; exists {
											selExpr.Sel.Name = newMethod
											ctxExpr := ast.NewIdent("ctx")
											node.Args = append([]ast.Expr{ctxExpr}, node.Args...)
										}
									}
								}
							}
						}
					}
					// sqlxのメソッド呼び出しかどうかを判定
					if funSelExpr, ok := node.Fun.(*ast.SelectorExpr); ok {
						if selType := pkg.TypesInfo.TypeOf(funSelExpr.X); selType != nil {
							recvTypeStr := strings.TrimPrefix(selType.String(), "*")
							if newMethod, exists := sqlxMethods[funSelExpr.Sel.Name]; strings.Contains(recvTypeStr, "github.com/jmoiron/sqlx") && exists {
								funSelExpr.Sel.Name = newMethod
								ctxExpr := ast.NewIdent("ctx")
								node.Args = append([]ast.Expr{ctxExpr}, node.Args...)
							}
						}
					}

				case *ast.FuncDecl:
					// ctxを引数に追加する処理
					if !hasHttpParams(node) && !hasEchoHandlerParams(node) && !hasEchoMiddlewareParams(node) && !isMainFunc(node) {
						addContextParam(node.Type)
						modifyFuncCalls(node.Name.Name, file)
					}

					// hasHttpParams がtrueの場合は関数のBodyの先頭に ctx := r.Context() を追加する
					if hasHttpParams(node) {
						addCtxVariableFromHttpRequest(node)
					}
					if hasEchoHandlerParams(node) {
						addCtxVariableFromEchoContext(node.Body)
					}
				case *ast.FuncLit:
					// 関数リテラルの場合は関数のBodyの先頭に ctx := c.Request().Context() を追加する
					if hasEchoHandlerParamsFuncType(node.Type) {
						addCtxVariableFromEchoContext(node.Body)
					}
				}
				return true
			})

			if *overwrite {
				fileName := pkg.Fset.File(file.Pos()).Name()
				var buf bytes.Buffer
				err = format.Node(&buf, pkg.Fset, file)
				if err != nil {
					fmt.Fprintln(os.Stderr, "Error formatting code:", err)
					continue
				}
				if err := os.WriteFile(fileName, buf.Bytes(), 0664); err != nil {
					fmt.Fprintln(os.Stderr, "Error writing to file:", err)
				}
			} else {
				var buf bytes.Buffer
				err = format.Node(&buf, fset, file)
				if err != nil {
					fmt.Println("Failed to format the file:", err)
					return
				}
				fmt.Println(buf.String())
			}
		}
	}
}

// 関数のBodyの先頭に ctx := r.Context() を追加する
func addCtxVariableFromHttpRequest(node *ast.FuncDecl) {
	ctxExpr := ast.NewIdent("ctx")
	rExpr := &ast.CallExpr{
		Fun: &ast.SelectorExpr{
			X:   ast.NewIdent("r"),
			Sel: ast.NewIdent("Context"),
		},
	}
	assignStmt := &ast.AssignStmt{
		Lhs: []ast.Expr{ctxExpr},
		Tok: token.DEFINE,
		Rhs: []ast.Expr{rExpr},
	}
	node.Body.List = append([]ast.Stmt{assignStmt}, node.Body.List...)
}

// 関数のBodyの先頭に ctx := c.Request().Context() を追加する
func addCtxVariableFromEchoContext(body *ast.BlockStmt) {
	ctxExpr := ast.NewIdent("ctx")
	rExpr := &ast.CallExpr{
		Fun: &ast.SelectorExpr{
			X: &ast.CallExpr{
				Fun: &ast.SelectorExpr{
					X:   ast.NewIdent("c"),
					Sel: ast.NewIdent("Request"),
				},
			},
			Sel: ast.NewIdent("Context"),
		},
	}
	assignStmt := &ast.AssignStmt{
		Lhs: []ast.Expr{ctxExpr},
		Tok: token.DEFINE,
		Rhs: []ast.Expr{rExpr},
	}
	body.List = append([]ast.Stmt{assignStmt}, body.List...)
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

func hasEchoHandlerParams(funDecl *ast.FuncDecl) bool {
	if len(funDecl.Type.Params.List) < 1 {
		return false
	}
	firstParam := funDecl.Type.Params.List[0]

	return isSelectorExprOfType(firstParam.Type, "echo", "Context")
}
func hasEchoHandlerParamsFuncType(funcType *ast.FuncType) bool {
	if len(funcType.Params.List) < 1 {
		return false
	}
	firstParam := funcType.Params.List[0]

	return isSelectorExprOfType(firstParam.Type, "echo", "Context")
}

func hasEchoMiddlewareParams(funDecl *ast.FuncDecl) bool {
	if len(funDecl.Type.Params.List) < 1 {
		return false
	}
	firstParam := funDecl.Type.Params.List[0]

	return isSelectorExprOfType(firstParam.Type, "echo", "HandlerFunc")
}

func isMainFunc(funDecl *ast.FuncDecl) bool {
	return funDecl.Name.Name == "main"
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

		switch fun := callExpr.Fun.(type) {
		// メソッド
		case *ast.SelectorExpr:
			if fun.Sel.Name != name {
				return true
			}
		// 関数
		case *ast.Ident:
			if fun.Name != name {
				return true
			}
		default:
			return true
		}

		// 既にctxが最初の引数として存在しているか確認
		if len(callExpr.Args) > 0 {
			firstArg, ok := callExpr.Args[0].(*ast.Ident)
			if ok && (firstArg.Name == "ctx" || firstArg.Name == "c") {
				return true
			}
		}

		// ctxを最初の引数として追加
		ctxExpr := ast.NewIdent("ctx")
		callExpr.Args = append([]ast.Expr{ctxExpr}, callExpr.Args...)
		return true
	})
}

func addContextImport(file *ast.File) *ast.File {
	contextImported := false
	for _, imp := range file.Imports {
		if imp.Path.Value == "\"context\"" {
			contextImported = true
			break
		}
	}

	if !contextImported {
		// contextパッケージがimportされていない場合、追加する
		newImport := &ast.ImportSpec{
			Path: &ast.BasicLit{
				Kind:  token.STRING,
				Value: "\"context\"",
			},
		}
		file.Decls = append([]ast.Decl{
			&ast.GenDecl{
				Tok:   token.IMPORT,
				Specs: []ast.Spec{newImport},
			},
		}, file.Decls...)
	}

	return file
}
