package main

// fun new：脚手架——一条命令生成可运行的 fun 项目骨架

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var reModuleName = regexp.MustCompile(`^[a-zA-Z0-9][\w.\-/]*$`)

func cmdNew(args []string) {
	// 手动解析：允许 flag 放任意位置（go flag 遇位置参数即停，不友好）
	port, local, module := 18080, "", ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		if v, ok := strings.CutPrefix(a, "-port="); ok {
			fmt.Sscanf(v, "%d", &port)
			continue
		}
		if v, ok := strings.CutPrefix(a, "-local="); ok {
			local = v
			continue
		}
		if (a == "-port" || a == "-local") && i+1 < len(args) {
			i++
			if a == "-port" {
				fmt.Sscanf(args[i], "%d", &port)
			} else {
				local = args[i]
			}
			continue
		}
		if strings.HasPrefix(a, "-") {
			fmt.Fprintln(os.Stderr, "fun: 未知参数:", a)
			os.Exit(2)
		}
		if module != "" {
			fmt.Fprintln(os.Stderr, "用法: fun new <项目名|模块路径> [-port 端口] [-local fun源码路径]")
			os.Exit(2)
		}
		module = a
	}
	if module == "" {
		fmt.Fprintln(os.Stderr, "用法: fun new <项目名|模块路径> [-port 端口] [-local fun源码路径]")
		fmt.Fprintln(os.Stderr, "示例: fun new myapp    fun new github.com/me/myapp")
		os.Exit(2)
	}
	if !reModuleName.MatchString(module) || strings.Contains(module, "..") {
		fmt.Fprintln(os.Stderr, "fun: 非法项目名:", module)
		os.Exit(1)
	}
	// 模块路径形如 github.com/me/app 时，目录取最后一段
	dir := module
	if i := strings.LastIndex(module, "/"); i >= 0 {
		dir = module[i+1:]
	}
	if _, err := os.Stat(dir); err == nil {
		fmt.Fprintf(os.Stderr, "fun: 目录 %s 已存在\n", dir)
		os.Exit(1)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "fun: 创建目录失败:", err)
		os.Exit(1)
	}

	gomod := fmt.Sprintf("module %s\n\ngo 1.22\n\nrequire github.com/cyi-cc/fun v1.4.1\n", module)
	if local != "" {
		abs, err := filepath.Abs(local)
		if err != nil {
			abs = local
		}
		gomod += fmt.Sprintf("\nreplace github.com/cyi-cc/fun => %s\n", filepath.ToSlash(abs))
	}
	files := map[string]string{
		"go.mod": gomod,
		"main.go": fmt.Sprintf(`package main

import "github.com/cyi-cc/fun"

type HelloDto struct {
	Name string
}

type HelloSvc struct {
	fun.Ctx
}

// hello world：最小可调用的服务方法
func (s *HelloSvc) Say(dto HelloDto) (string, error) {
	return "hello, " + dto.Name + "!", nil
}

func main() {
	f := fun.New()
	f.BindService(&HelloSvc{})
	f.Start(%d)
}
`, port),
		".gitignore": "/gen\n/log\nfun-docs-edits.json\n",
		"README.md": fmt.Sprintf(`# %s

fun 框架项目。[文档](https://fungo.ink)

## 启动

`+"```bash"+`
go run .
`+"```"+`

## 工具链（fun-cli）

`+"```bash"+`
fun docs          # API 文档站 + 在线调测（热更新）
fun run           # 热更新开发循环
fun gen ts        # 生成 TypeScript 客户端
`+"```"+`
`, module),
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "fun: 写入 %s 失败: %v\n", name, err)
			os.Exit(1)
		}
	}

	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = dir
	tidy.Stdout, tidy.Stderr = os.Stderr, os.Stderr
	tidied := tidy.Run() == nil

	next := "go run ."
	if !tidied {
		next = "go mod tidy && go run ."
	}
	fmt.Printf(`
  已创建 %s（module %s）
  ─────────────────────────────────────────
  %s/go.mod     依赖 github.com/cyi-cc/fun
  %s/main.go    HelloSvc.Say 示例服务
  ─────────────────────────────────────────
  下一步:
    cd %s
    %s   # 启动服务（:%d）
    fun docs        # 文档站在线调测
`, dir, module, dir, dir, dir, next, port)
	if !tidied {
		fmt.Println("\n  ⚠ go mod tidy 失败（可能离线），进入目录后手动执行一次即可")
	}
}
