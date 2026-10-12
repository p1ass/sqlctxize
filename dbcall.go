package sqlctxize

import (
	"go/ast"
	"go/types"
	"slices"
)

var dbPackages = []string{"database/sql", "github.com/jmoiron/sqlx"}

type dbRule struct {
	newName string
	// nilOptions is set for Begin variants, whose context-aware versions take a *sql.TxOptions after ctx.
	nilOptions bool
}

var renamedDBCalls = map[string]dbRule{
	"Begin":     {newName: "BeginTx", nilOptions: true},
	"Beginx":    {newName: "BeginTxx", nilOptions: true},
	"MustBegin": {newName: "MustBeginTx", nilOptions: true},
}

var contextDBCalls = []string{
	"Connect", "Exec", "Get", "LoadFile", "MustExec", "NamedExec", "NamedQuery", "NamedStmt", "Ping", "Prepare",
	"PrepareNamed", "Preparex", "Query", "QueryRow", "QueryRowx", "Queryx", "Select", "Stmt", "Stmtx",
}

func ruleFor(name string) (dbRule, bool) {
	if r, ok := renamedDBCalls[name]; ok {
		return r, true
	}
	if slices.Contains(contextDBCalls, name) {
		return dbRule{newName: name + "Context"}, true
	}
	return dbRule{}, false
}

// matchDBCall reports how to rewrite call when it is a database/sql or sqlx call, or a call through an interface, whose context-aware variant accepts the same arguments.
func matchDBCall(info *types.Info, call *ast.CallExpr) *dbRule {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	rule, ok := ruleFor(sel.Sel.Name)
	if !ok {
		return nil
	}
	var replacement types.Object
	if selection := info.Selections[sel]; selection != nil {
		if selection.Kind() != types.MethodVal {
			return nil
		}
		fn := selection.Obj().(*types.Func)
		if !isDBPackage(fn.Pkg()) && !types.IsInterface(selection.Recv()) {
			return nil
		}
		replacement, _, _ = types.LookupFieldOrMethod(selection.Recv(), true, fn.Pkg(), rule.newName)
	} else if x, ok := sel.X.(*ast.Ident); ok {
		pkgName, ok := info.Uses[x].(*types.PkgName)
		if !ok || !isDBPackage(pkgName.Imported()) {
			return nil
		}
		replacement = pkgName.Imported().Scope().Lookup(rule.newName)
	}
	fn, ok := replacement.(*types.Func)
	if !ok || !acceptsArgs(info, fn.Type().(*types.Signature), call, rule) {
		return nil
	}
	return &rule
}

func isDBPackage(pkg *types.Package) bool {
	return pkg != nil && slices.Contains(dbPackages, pkg.Path())
}

func acceptsArgs(info *types.Info, sig *types.Signature, call *ast.CallExpr, rule dbRule) bool {
	params := sig.Params()
	lead := 1
	if rule.nilOptions {
		lead = 2
	}
	if params.Len() < lead || !isContext(params.At(0).Type()) {
		return false
	}
	if rule.nilOptions && !isPointer(params.At(1).Type()) {
		return false
	}
	rest := params.Len() - lead
	for i, arg := range call.Args {
		t := info.TypeOf(arg)
		if t == nil {
			return false
		}
		var want types.Type
		switch {
		case sig.Variadic() && i >= rest-1:
			want = params.At(params.Len() - 1).Type()
			if !call.Ellipsis.IsValid() {
				want = want.(*types.Slice).Elem()
			}
		case i < rest:
			want = params.At(lead + i).Type()
		default:
			return false
		}
		if !types.AssignableTo(t, want) {
			return false
		}
	}
	if sig.Variadic() {
		return len(call.Args) >= rest-1
	}
	return len(call.Args) == rest
}

func isPointer(t types.Type) bool {
	_, ok := t.Underlying().(*types.Pointer)
	return ok
}
