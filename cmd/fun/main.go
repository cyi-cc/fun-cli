package main

// fun-cli：fun 框架的配套命令行工具
//
//	fun new          脚手架：生成可运行的 fun 项目骨架
//	fun gen [ts|go]  生成 TypeScript / Go 客户端
//	fun run          热更新开发循环
//	fun docs         拉起 API 文档站（参数表单 + 在线发送测试）

import (
	"fmt"
	"os"
)

const version = "1.4.1"

const usage = `fun-cli ` + version + ` — fun 框架配套工具

用法:
  fun new <项目名|模块路径> [-port 端口]
      脚手架：生成 go.mod + 示例服务 + README，go mod tidy 后即可 go run。

  fun gen [ts|go] [-o 目录] [-p 模块路径] [-from meta.json]
      生成客户端代码（fun 核心直接产出，与内置 GenCode 完全一致）。
      扫描模块内 BindService/BindServiceForGen 调用，在对应包目录临时
      生成辅助测试执行采集，跑完即删；或 -from 直接读元数据 JSON。

  fun run [-p 包路径] [-- 程序参数]
      热更新：改 .go/go.mod/go.sum 自动重编译重启。先编译成功再切换，
      编译失败保留旧进程。Ctrl+C 退出。

  fun docs [-addr 地址] [-upstream 后端URL] [-p 包路径] [-no-run] [-from meta.json]
      API 文档站：服务/方法树、DTO 参数表单、State 编辑、一键发送；
      流式方法实时展示 NDJSON 行。默认自动 go run 拉起后端并反向代理 /cell。

示例:
  fun new myapp
  fun gen ts -o frontend/src/api
  fun run -p ./cmd/server
  fun docs -upstream http://127.0.0.1:9000 -p .

安装:
  go install github.com/cyi-cc/fun-cli@latest
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "new":
		cmdNew(os.Args[2:])
	case "gen":
		cmdGen(os.Args[2:])
	case "run":
		cmdRun(os.Args[2:])
	case "docs":
		cmdDocs(os.Args[2:])
	case "-v", "version":
		fmt.Println("fun-cli", version)
	default:
		fmt.Fprintf(os.Stderr, "未知命令: %s\n\n", os.Args[1])
		fmt.Print(usage)
		os.Exit(2)
	}
}
