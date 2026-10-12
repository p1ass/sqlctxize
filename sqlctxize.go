// Package sqlctxize rewrites database/sql and sqlx calls into their context-aware variants and threads a context.Context to them.
package sqlctxize

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"go/version"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
)

type Options struct {
	Dir     string
	Exclude []string
}

type FileResult struct {
	Path    string
	Content []byte
}

// Rewrite returns the rewritten content of every file under opts.Dir that changes. It does not write files.
func Rewrite(opts Options) ([]FileResult, error) {
	root, err := filepath.Abs(opts.Dir)
	if err != nil {
		return nil, err
	}
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports |
			packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedModule,
		Dir: root,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("load packages: %w", err)
	}
	if err := checkPackages(pkgs); err != nil {
		return nil, err
	}

	r := newRewriter()
	for _, pkg := range pkgs {
		if isExcluded(root, pkg, opts.Exclude) {
			continue
		}
		r.addPackage(root, pkg)
	}
	r.plan()
	return r.render()
}

func checkPackages(pkgs []*packages.Package) error {
	var errs []error
	for _, pkg := range pkgs {
		if pkg.Module != nil && pkg.Module.GoVersion != "" {
			want := "go" + pkg.Module.GoVersion
			if version.Compare(want, runtime.Version()) > 0 {
				return tooNewError(pkg.Module.Path, want, runtime.Version())
			}
		}
		for _, e := range pkg.Errors {
			errs = append(errs, errors.New(e.Error()))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("the target packages must compile before rewriting:\n%w", errors.Join(errs...))
	}
	return nil
}

func tooNewError(module, want, have string) error {
	return fmt.Errorf("module %s requires %s, but sqlctxize was built with %s; reinstall it with a newer toolchain: GOTOOLCHAIN=%s go install github.com/p1ass/sqlctxize/cmd/sqlctxize@latest",
		module, want, have, version.Lang(want)+".0")
}

func isExcluded(root string, pkg *packages.Package, patterns []string) bool {
	if len(pkg.GoFiles) == 0 {
		return true
	}
	rel, err := filepath.Rel(root, filepath.Dir(pkg.GoFiles[0]))
	if err != nil || strings.HasPrefix(rel, "..") {
		return true
	}
	rel = filepath.ToSlash(rel)
	for _, p := range patterns {
		p = strings.TrimSuffix(strings.TrimPrefix(filepath.ToSlash(p), "./"), "/")
		if p == "" {
			continue
		}
		if rel == p || strings.HasPrefix(rel, p+"/") {
			return true
		}
		if ok, _ := path.Match(p, rel); ok {
			return true
		}
	}
	return false
}

type file struct {
	pkg       *packages.Package
	ast       *ast.File
	path      string
	src       []byte
	edits     []edit
	needsCtxt bool
}

type edit struct {
	pos  int
	end  int
	text string
}

type funcDecl struct {
	decl    *ast.FuncDecl
	file    *file
	addable bool
}

// A site is a call that needs a context: a DB call to rewrite, or a call to a function that gains a ctx parameter.
type site struct {
	call  *ast.CallExpr
	file  *file
	funcs []ast.Node
	db    *dbRule
	// callee is set for calls to functions declared in the target packages.
	callee string
}

type rewriter struct {
	files  []*file
	decls  map[string]*funcDecl
	sites  []*site
	calls  map[*ast.Ident]bool
	values map[string]bool
	ifaces []*types.Interface

	addParam map[string]bool
}

func newRewriter() *rewriter {
	return &rewriter{
		decls:    map[string]*funcDecl{},
		calls:    map[*ast.Ident]bool{},
		values:   map[string]bool{},
		addParam: map[string]bool{},
	}
}

func (r *rewriter) addPackage(root string, pkg *packages.Package) {
	r.collectInterfaces(pkg.Types)
	for _, imp := range pkg.Types.Imports() {
		r.collectInterfaces(imp)
	}
	for i, f := range pkg.Syntax {
		name := pkg.CompiledGoFiles[i]
		if !strings.HasSuffix(name, ".go") || !strings.HasPrefix(name, root+string(filepath.Separator)) {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			continue
		}
		fl := &file{pkg: pkg, ast: f, path: name, src: src}
		r.files = append(r.files, fl)
		r.collectDecls(fl)
	}
}

func (r *rewriter) collectInterfaces(pkg *types.Package) {
	scope := pkg.Scope()
	for _, name := range scope.Names() {
		tn, ok := scope.Lookup(name).(*types.TypeName)
		if !ok || !tn.Exported() && tn.Pkg() != pkg {
			continue
		}
		if iface, ok := tn.Type().Underlying().(*types.Interface); ok && iface.NumMethods() > 0 && !slices.Contains(r.ifaces, iface) {
			r.ifaces = append(r.ifaces, iface)
		}
	}
}

func (r *rewriter) collectDecls(fl *file) {
	info := fl.pkg.TypesInfo
	for _, d := range fl.ast.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		obj, ok := info.Defs[fd.Name].(*types.Func)
		if !ok {
			continue
		}
		r.decls[funcKey(obj)] = &funcDecl{decl: fd, file: fl, addable: r.canAddParam(fl, fd, obj)}
	}
	ast.Inspect(fl.ast, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if id := calleeIdent(call.Fun); id != nil {
				r.calls[id] = true
			}
		}
		return true
	})
}

func (r *rewriter) canAddParam(fl *file, fd *ast.FuncDecl, obj *types.Func) bool {
	if fd.Recv == nil && (fd.Name.Name == "init" || fd.Name.Name == "main" && fl.pkg.Name == "main") {
		return false
	}
	if declaresCtx(fl.pkg.TypesInfo, fd) {
		return false
	}
	sig := obj.Type().(*types.Signature)
	if sig.Recv() == nil {
		return true
	}
	recv := sig.Recv().Type()
	if p, ok := recv.(*types.Pointer); ok {
		recv = p.Elem()
	}
	named, ok := recv.(*types.Named)
	if !ok || named.TypeParams().Len() > 0 {
		return true
	}
	for _, iface := range r.ifaces {
		if m, _, _ := types.LookupFieldOrMethod(iface, false, nil, obj.Name()); m == nil {
			continue
		}
		if types.Implements(named, iface) || types.Implements(types.NewPointer(named), iface) {
			return false
		}
	}
	return true
}

func calleeIdent(fun ast.Expr) *ast.Ident {
	switch f := ast.Unparen(fun).(type) {
	case *ast.Ident:
		return f
	case *ast.SelectorExpr:
		return f.Sel
	case *ast.IndexExpr:
		return calleeIdent(f.X)
	case *ast.IndexListExpr:
		return calleeIdent(f.X)
	}
	return nil
}

func funcKey(fn *types.Func) string {
	return fn.Origin().FullName()
}

func (r *rewriter) collectSites(fl *file) {
	info := fl.pkg.TypesInfo
	var funcs []ast.Node
	var visit func(n ast.Node) bool
	visit = func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			funcs = append(funcs, n)
			ast.Inspect(funcBody(n), visit)
			funcs = funcs[:len(funcs)-1]
			return false
		case *ast.CallExpr:
			s := &site{call: n, file: fl, funcs: slices.Clone(funcs)}
			if rule := matchDBCall(info, n); rule != nil {
				s.db = rule
				r.sites = append(r.sites, s)
			} else if id := calleeIdent(n.Fun); id != nil {
				if fn, ok := info.Uses[id].(*types.Func); ok {
					if _, ok := r.decls[funcKey(fn)]; ok {
						s.callee = funcKey(fn)
						r.sites = append(r.sites, s)
					}
				}
			}
		}
		return true
	}
	ast.Inspect(fl.ast, visit)
}

func funcBody(n ast.Node) ast.Node {
	switch n := n.(type) {
	case *ast.FuncDecl:
		if n.Body == nil {
			return &ast.BlockStmt{}
		}
		return n.Body
	case *ast.FuncLit:
		return n.Body
	}
	return nil
}

func funcType(n ast.Node) *ast.FuncType {
	switch n := n.(type) {
	case *ast.FuncDecl:
		return n.Type
	case *ast.FuncLit:
		return n.Type
	}
	return nil
}

// plan decides which functions gain a ctx parameter. A function gains one when a site inside it has no other context source, which in turn makes its callers sites.
func (r *rewriter) plan() {
	seen := map[*packages.Package]bool{}
	for _, fl := range r.files {
		r.collectSites(fl)
		if seen[fl.pkg] {
			continue
		}
		seen[fl.pkg] = true
		for id, obj := range fl.pkg.TypesInfo.Uses {
			if fn, ok := obj.(*types.Func); ok && !r.calls[id] {
				r.values[funcKey(fn)] = true
			}
		}
	}
	for _, d := range r.decls {
		key := funcKey(d.file.pkg.TypesInfo.Defs[d.decl.Name].(*types.Func))
		if r.values[key] {
			d.addable = false
		}
	}
	for changed := true; changed; {
		changed = false
		for _, s := range r.sites {
			if !r.active(s) {
				continue
			}
			src := r.resolve(s)
			if src.kind != sourceParam {
				continue
			}
			key := funcKey(s.file.pkg.TypesInfo.Defs[src.owner.(*ast.FuncDecl).Name].(*types.Func))
			if !r.addParam[key] {
				r.addParam[key] = true
				changed = true
			}
		}
	}
}

func (r *rewriter) active(s *site) bool {
	return s.db != nil || r.addParam[s.callee]
}

type sourceKind int

const (
	sourceExisting sourceKind = iota
	sourceLocal
	sourceParam
	sourceInline
)

// A ctxSource tells where a site gets its context from. For sourceLocal, owner gets "ctx := init" at the top of its body.
type ctxSource struct {
	kind  sourceKind
	owner ast.Node
	expr  string
	init  string
}

func (r *rewriter) resolve(s *site) ctxSource {
	info := s.file.pkg.TypesInfo
	pos := s.call.Pos()
	scopes := map[*types.Scope]ast.Node{}
	for _, fn := range s.funcs {
		scopes[info.Scopes[funcType(fn)]] = fn
	}
	for sc := s.file.pkg.Types.Scope().Innermost(pos); sc != nil && sc != s.file.pkg.Types.Scope(); sc = sc.Parent() {
		if name := visibleContextVar(sc, pos); name != "" {
			return ctxSource{kind: sourceExisting, expr: name}
		}
		fn, ok := scopes[sc]
		if !ok {
			continue
		}
		if init := requestContext(info, funcType(fn)); init != "" {
			if declaresCtx(info, fn) {
				return ctxSource{kind: sourceInline, expr: init}
			}
			return ctxSource{kind: sourceLocal, owner: fn, expr: "ctx", init: init}
		}
		if fd, ok := fn.(*ast.FuncDecl); ok {
			obj := info.Defs[fd.Name].(*types.Func)
			if d := r.decls[funcKey(obj)]; d != nil && d.addable {
				return ctxSource{kind: sourceParam, owner: fd, expr: "ctx"}
			}
			if declaresCtx(info, fd) {
				return ctxSource{kind: sourceInline, expr: "context.Background()"}
			}
			return ctxSource{kind: sourceLocal, owner: fd, expr: "ctx", init: "context.Background()"}
		}
	}
	return ctxSource{kind: sourceInline, expr: "context.Background()"}
}

func visibleContextVar(sc *types.Scope, pos token.Pos) string {
	var names []string
	for _, name := range sc.Names() {
		v, ok := sc.Lookup(name).(*types.Var)
		if !ok || !isContext(v.Type()) || name == "_" {
			continue
		}
		if _, obj := sc.LookupParent(name, pos); obj != v {
			continue
		}
		if name == "ctx" {
			return name
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	return names[0]
}

func requestContext(info *types.Info, ft *ast.FuncType) string {
	for _, field := range ft.Params.List {
		t := info.TypeOf(field.Type)
		for _, name := range field.Names {
			if name.Name == "_" {
				continue
			}
			switch {
			case isEchoContext(t):
				return name.Name + ".Request().Context()"
			case isHTTPRequest(t):
				return name.Name + ".Context()"
			}
		}
	}
	return ""
}

// declaresCtx reports whether fn already declares ctx at its top level, where adding another ctx would be a redeclaration.
func declaresCtx(info *types.Info, fn ast.Node) bool {
	sc := info.Scopes[funcType(fn)]
	return sc != nil && sc.Lookup("ctx") != nil
}

func isNamed(t types.Type, pkgPaths []string, name string) bool {
	if t == nil {
		return false
	}
	named, ok := types.Unalias(t).(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Name() == name && slices.Contains(pkgPaths, named.Obj().Pkg().Path())
}

func isContext(t types.Type) bool {
	return isNamed(t, []string{"context"}, "Context")
}

func isEchoContext(t types.Type) bool {
	return isNamed(t, []string{"github.com/labstack/echo/v4", "github.com/labstack/echo"}, "Context")
}

func isHTTPRequest(t types.Type) bool {
	p, ok := types.Unalias(t).(*types.Pointer)
	return ok && isNamed(p.Elem(), []string{"net/http"}, "Request")
}

func (r *rewriter) render() ([]FileResult, error) {
	introduced := map[ast.Node]bool{}
	for _, s := range r.sites {
		if !r.active(s) {
			continue
		}
		src := r.resolve(s)
		if src.kind == sourceLocal && !introduced[src.owner] {
			introduced[src.owner] = true
			body := funcBody(src.owner).(*ast.BlockStmt)
			text := "\nctx := " + src.init
			if off := s.file.offset(body.Lbrace) + 1; off < len(s.file.src) && s.file.src[off] != '\n' {
				text += "\n"
			}
			s.file.insert(body.Lbrace+1, text)
		}
		if strings.HasPrefix(src.init, "context.") || strings.HasPrefix(src.expr, "context.") {
			s.file.needsCtxt = true
		}
		args := src.expr
		if s.db != nil {
			sel := ast.Unparen(s.call.Fun).(*ast.SelectorExpr)
			s.file.replace(sel.Sel.Pos(), sel.Sel.End(), s.db.newName)
			if s.db.nilOptions {
				args += ", nil"
			}
		}
		if len(s.call.Args) > 0 {
			args += ", "
		}
		s.file.insert(s.call.Lparen+1, args)
	}
	for key := range r.addParam {
		d := r.decls[key]
		params := d.decl.Type.Params
		text := "ctx context.Context"
		if len(params.List) > 0 {
			text += ", "
		}
		d.file.insert(params.Opening+1, text)
		d.file.needsCtxt = true
	}

	var results []FileResult
	for _, fl := range r.files {
		if len(fl.edits) == 0 {
			continue
		}
		out, err := fl.apply()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", fl.path, err)
		}
		if !bytes.Equal(out, fl.src) {
			results = append(results, FileResult{Path: fl.path, Content: out})
		}
	}
	return results, nil
}

func (f *file) offset(p token.Pos) int {
	return f.pkg.Fset.Position(p).Offset
}

func (f *file) insert(p token.Pos, text string) {
	off := f.offset(p)
	f.edits = append(f.edits, edit{pos: off, end: off, text: text})
}

func (f *file) replace(p, end token.Pos, text string) {
	f.edits = append(f.edits, edit{pos: f.offset(p), end: f.offset(end), text: text})
}

func (f *file) apply() ([]byte, error) {
	sort.SliceStable(f.edits, func(i, j int) bool { return f.edits[i].pos > f.edits[j].pos })
	out := slices.Clone(f.src)
	for _, e := range f.edits {
		out = slices.Concat(out[:e.pos], []byte(e.text), out[e.end:])
	}
	if f.needsCtxt {
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, f.path, out, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		astutil.AddImport(fset, parsed, "context")
		var buf bytes.Buffer
		if err := format.Node(&buf, fset, parsed); err != nil {
			return nil, err
		}
		out = buf.Bytes()
	}
	return format.Source(out)
}
