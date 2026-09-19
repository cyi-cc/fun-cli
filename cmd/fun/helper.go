package main

// 采集辅助:在 BindService 调用所在的包目录临时生成 fun_gen_helper_test.go
// (测试文件可访问包内类型,天然解决"服务定义在 package main"不可 import 的问题),
// go test 执行完毕后删除。fun 框架本体零改动,只用其公开 API

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const helperTestFile = "fun_gen_helper_test.go"
const helperTestName = "TestFunGenHelper"

// helperTarget 一个待生成辅助测试的包目录
type helperTarget struct {
	Dir  string
	Pkg  string
	Refs []serviceRef
}

type helperRef struct {
	Alias    string // 引用别名(&pkg.Svc 的 pkg);同包引用为空
	Import   string // 引用的导入路径
	TypeName string
}

// runFunGen 扫描绑定调用,生成辅助测试并执行:
// lang: "ts" / "go" / "both";out 为 GenCode 输出目录(会被 fun 清空重建)
func runFunGen(moduleRoot, lang, out string) error {
	targets, err := scanBindTargets(moduleRoot)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return fmt.Errorf("未在模块中找到 BindService/BindServiceForGen 调用。" +
			"请确认服务注册代码,或用 -from 直接指定元数据 JSON 文件")
	}

	// 清理保险:先移除可能的历史残留
	defer cleanupHelpers(targets)
	cleanupHelpers(targets)

	pkgs := make([]string, 0, len(targets))
	for _, t := range targets {
		if err := writeHelperTest(moduleRoot, t); err != nil {
			return err
		}
		rel, err := filepath.Rel(moduleRoot, t.Dir)
		if err != nil {
			return err
		}
		pkg := "./" + filepath.ToSlash(rel)
		pkgs = append(pkgs, pkg)
	}

	args := append([]string{"test", "-vet=off", "-count=1", "-run", "^" + helperTestName + "$"}, pkgs...)
	cmd := exec.Command("go", args...)
	cmd.Dir = moduleRoot
	cmd.Env = append(os.Environ(), "FUN_OUT="+out, "FUN_LANG="+lang)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		cleanupHelpers(targets)
		return fmt.Errorf("采集执行失败: %w", err)
	}
	return nil
}

func cleanupHelpers(targets []helperTarget) {
	for _, t := range targets {
		os.Remove(filepath.Join(t.Dir, helperTestFile))
	}
}

func writeHelperTest(moduleRoot string, t helperTarget) error {
	// 汇总需要的外部导入(去重,按路径排序)
	imports := map[string]string{} // alias → path
	for _, r := range t.Refs {
		if r.Alias != "" {
			imports[r.Alias] = r.Import
		}
	}
	paths := make([]string, 0, len(imports))
	for _, p := range imports {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	aliasOf := func(path string) string {
		for a, p := range imports {
			if p == path {
				return a
			}
		}
		return ""
	}

	var imp, binds strings.Builder
	imp.WriteString("\t\"os\"\n\t\"testing\"\n\n\t\"github.com/cyi-cc/fun\"\n")
	for _, p := range paths {
		fmt.Fprintf(&imp, "\t%s %q\n", aliasOf(p), p)
	}
	for _, r := range t.Refs {
		if r.Alias != "" {
			fmt.Fprintf(&binds, "\tf.BindServiceForGen(&%s.%s{})\n", r.Alias, r.TypeName)
		} else {
			fmt.Fprintf(&binds, "\tf.BindServiceForGen(&%s{})\n", r.TypeName)
		}
	}

	src := fmt.Sprintf(`// fun-cli 临时采集辅助(自动生成,执行完即删除)
package %s

import (
%s)

func %s(t *testing.T) {
	f := fun.New()
%s	fun.SetOutput(os.Getenv("FUN_OUT"))
	switch os.Getenv("FUN_LANG") {
	case "ts":
		fun.GenCode(fun.GenTs{})
	case "go":
		fun.GenCode(fun.GenGo{})
	default:
		fun.GenCode(fun.GenGo{}, fun.GenTs{})
	}
}
`, t.Pkg, imp.String(), helperTestName, binds.String())
	return os.WriteFile(filepath.Join(t.Dir, helperTestFile), []byte(src), 0o644)
}
