package main

// AST 扫描:在用户模块里找 BindService / BindServiceForGen 调用点,
// 按目录聚合——fun 框架零改动,采集全靠 fun 公开 API

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type serviceRef struct {
	Alias    string // &pkg.Svc 的 pkg 别名;同包引用为空
	Import   string // pkg 对应的导入路径
	TypeName string
}

// modulePath 读 go.mod 的 module 行
func modulePath(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("找不到 go.mod（%s）: %w", root, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module ")), nil
		}
	}
	return "", fmt.Errorf("go.mod 中没有 module 声明")
}

// findModuleRoot 从 dir 向上找 go.mod
func findModuleRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(abs, "go.mod")); err == nil {
			return abs, nil
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", fmt.Errorf("从 %s 向上未找到 go.mod", dir)
		}
		abs = parent
	}
}

var scanSkipDirs = map[string]bool{
	".git": true, "vendor": true, "node_modules": true, ".fun-tmp": true, "testdata": true,
}

// scanBindTargets 扫描模块内全部 .go 文件的 BindService* 调用,
// 按目录聚合成待生成辅助测试的包目标(测试文件可访问包内类型,
// 服务定义在 package main 也能采集)
func scanBindTargets(root string) ([]helperTarget, error) {
	byDir := map[string]*helperTarget{}
	seen := map[string]bool{}

	add := func(dir, pkgName string, r serviceRef) {
		key := dir + "|" + r.Alias + "|" + r.Import + "|" + r.TypeName
		if seen[key] {
			return
		}
		seen[key] = true
		t, ok := byDir[dir]
		if !ok {
			t = &helperTarget{Dir: dir, Pkg: pkgName}
			byDir[dir] = t
		}
		t.Refs = append(t.Refs, r)
	}

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && (scanSkipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return nil // 语法不过的文件(生成物/草稿)跳过
		}

		// 该文件可见的导入:别名/包名 → 导入路径
		imports := map[string]string{}
		for _, imp := range file.Imports {
			importPath, _ := strconv.Unquote(imp.Path.Value)
			alias := filepath.Base(importPath)
			if imp.Name != nil {
				alias = imp.Name.Name
			}
			imports[alias] = importPath
		}

		// 服务列表变量:for _, x := range []any{&A{}, &B{}} / x := []any{...}
		// (BindServiceForGen(svc) 循环注册的常见写法)
		parsePtrComposite := func(e ast.Expr) (serviceRef, bool) {
			u, ok := e.(*ast.UnaryExpr)
			if !ok || u.Op != token.AND {
				return serviceRef{}, false
			}
			lit, ok := u.X.(*ast.CompositeLit)
			if !ok {
				return serviceRef{}, false
			}
			switch t := lit.Type.(type) {
			case *ast.Ident:
				return serviceRef{TypeName: t.Name}, true
			case *ast.SelectorExpr:
				if pkg, ok := t.X.(*ast.Ident); ok {
					if importPath, ok := imports[pkg.Name]; ok {
						return serviceRef{Alias: pkg.Name, Import: importPath, TypeName: t.Sel.Name}, true
					}
				}
			}
			return serviceRef{}, false
		}
		collectSlice := func(lit *ast.CompositeLit) []serviceRef {
			var refs []serviceRef
			for _, el := range lit.Elts {
				if r, ok := parsePtrComposite(el); ok {
					refs = append(refs, r)
				}
			}
			return refs
		}
		sliceVars := map[string][]serviceRef{}
		var collectVars func(n ast.Node) bool
		collectVars = func(n ast.Node) bool {
			switch st := n.(type) {
			case *ast.RangeStmt:
				if v, ok := st.Value.(*ast.Ident); ok {
					if lit, ok := st.X.(*ast.CompositeLit); ok {
						sliceVars[v.Name] = collectSlice(lit)
					}
				}
			case *ast.AssignStmt:
				if len(st.Lhs) == 1 {
					if id, ok := st.Lhs[0].(*ast.Ident); ok {
						if lit, ok := st.Rhs[0].(*ast.CompositeLit); ok {
							sliceVars[id.Name] = collectSlice(lit)
						}
					}
				}
			}
			return true
		}
		ast.Inspect(file, collectVars)

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if sel.Sel.Name != "BindService" && sel.Sel.Name != "BindServiceForGen" {
				return true
			}
			if len(call.Args) == 0 {
				return true
			}
			// 参数形如 &pkg.Svc{} / &Svc{}
			var typeExpr ast.Expr
			if a, ok := call.Args[0].(*ast.UnaryExpr); ok && a.Op == token.AND {
				if lit, ok := a.X.(*ast.CompositeLit); ok {
					typeExpr = lit.Type
				}
			}
			if ident, ok := call.Args[0].(*ast.Ident); ok {
				// BindServiceForGen(svc):循环/切片变量注册
				for _, r := range sliceVars[ident.Name] {
					add(filepath.Dir(path), file.Name.Name, r)
				}
				return true
			}
			if typeExpr == nil {
				return true
			}
			switch t := typeExpr.(type) {
			case *ast.Ident:
				add(filepath.Dir(path), file.Name.Name, serviceRef{TypeName: t.Name})
			case *ast.SelectorExpr:
				if pkg, ok := t.X.(*ast.Ident); ok {
					if importPath, ok := imports[pkg.Name]; ok {
						add(filepath.Dir(path), file.Name.Name, serviceRef{
							Alias: pkg.Name, Import: importPath, TypeName: t.Sel.Name,
						})
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	dirs := make([]string, 0, len(byDir))
	for d := range byDir {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	targets := make([]helperTarget, 0, len(dirs))
	for _, d := range dirs {
		targets = append(targets, *byDir[d])
	}
	return targets, nil
}
