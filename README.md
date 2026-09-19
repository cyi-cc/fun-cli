# fun-cli

[fun](https://git.cyi.cc/chiyi/fun) 框架的配套命令行工具。**不修改 fun 框架任何一行**，
兼容 v1.3.7+；纯 Go 标准库实现，`go install` 即用。

## 安装

```bash
go install github.com/cyi-cc/fun-cli/cmd/fun@latest
```

## 工作原理(为什么 fun 可以零改动)

fun 的服务注册全在 `BindService`/`BindServiceForGen` 调用里。fun-cli 用 AST 扫描定位
这些调用(支持 `&pkg.Svc{}`、同包 `&Svc{}`、`for _, x := range []any{...}` 循环注册),
在被调用包目录**临时生成一个 `go test` 辅助文件**——测试文件天然能访问包内类型
(服务定义在 `package main` 也不怕)——执行后立即删除。生成由 fun 公开的
`BindServiceForGen + SetOutput + GenCode` 完成，产物与内置生成器完全一致。

## 三个命令

### fun gen —— 生成客户端代码

```bash
fun gen                       # Go + TS → ./gen
fun gen ts                    # 只生成 TypeScript
fun gen go                    # 只生成 Go
fun gen ts -o ./frontend/src/api
fun gen -from meta.json       # 直接渲染元数据 JSON(CI 友好)
```

### fun run —— 热更新

```bash
fun run                       # 改 .go/go.mod/go.sum 自动重编译重启
fun run -p ./cmd/server
fun run -- -config dev.yaml   # 透传程序参数
```

先编译成功再切换进程;编译失败保留旧进程。Ctrl+C 连子进程一起退出。

### fun docs —— API 文档站

```bash
fun docs                                        # 自动 go run . 拉起后端
fun docs -upstream http://127.0.0.1:9000        # 指定后端地址
fun docs -no-run                                # 后端已在别处运行
```

- 浅色文档风格:侧栏服务/方法树(stream 徽标)+ 搜索(`/` 快速聚焦)
- **分享**:标题旁一键复制深链(`#服务.方法?req=...`),自带请求体,打开即还原
- 参数表:字段/类型/示例值/说明;枚举下拉、嵌套 DTO 分层、可空标注
- **说明**:方法说明与字段备注可编辑,自动保存到 `fun-docs-edits.json`
  (启动时自动创建,`-edits` 可改路径);返回类型递归展示
- **请求**:表单实时生成 JSON,可直接编辑后发送
- **State**:"添加字段"式键值编辑(存 localStorage),值注入请求 `state` 对象——
  键以后端 Guard 读取为准,前端只负责填值
- **响应**:格式化 JSON 语法高亮;NDJSON 流式方法逐行实时追加
- 文档站反向代理 `/cell` 到后端,同源无 CORS 问题

## 元数据 JSON

`fun docs -from`/`fun gen -from` 消费的元数据格式(由 TS 产物解析重建):

```json
{"services":[{"name":"userSvc","methods":[
  {"name":"login","dto":{"kind":"struct","name":"loginDto","fields":[
    {"name":"email","type":{"kind":"string","name":"string"}}]}},
   "returns":{"kind":"string","name":"string"}}]}]}
```

## 已知边界

- fun v1.3.7 的 TS 模板对流式+DTO 方法会生成 `dto: dto:X` 的双前缀类型
  (框架自身的模板怪癖)。fun-cli 的文档站解析已兼容;生成的 TS 文件如需编译,
  该怪癖仍随产物保留——按"fun 零改动"约定不去修它。

## 开发

```bash
go test ./...   # 渲染回归 + 解析回环 + node 门控的 TS 客户端行为测试
```
