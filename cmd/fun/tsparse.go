package main

// 解析 fun 生成的 TypeScript 客户端产物,重建文档站所需的元数据。
// 生成物即元数据:fun 自己的反射保证类型准确,CLI 只做只读解析

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	reServiceClass = regexp.MustCompile(`(?m)^export default class (\w+) \{`)
	reStreamMethod = regexp.MustCompile(`(?m)^  async (\w+)\((?:dto:\s?([^,]+), )?onMessage: \(data: (.+?)\) => unknown`)
	reDataMethod   = regexp.MustCompile(`(?m)^  async (\w+)\((?:dto:\s?([^,]+), )?options\?: RequestOptions\): Promise<result<(.+?)>>`)
	reInterface    = regexp.MustCompile(`(?s)export default interface (\w+) \{([^}]*)\}`)
	reEnum         = regexp.MustCompile(`(?s)enum (\w+) \{([^}]*)\}`)
	reEnumMember   = regexp.MustCompile(`(?m)^\s+(\w+),?\s*$`)
	reDisplayNames = regexp.MustCompile(`(?s)function displayNames\(\):\s*string\[\]\s*\{(.*?)\}`)
	reQuoted       = regexp.MustCompile(`"([^"]*)"`)
	reField        = regexp.MustCompile(`(?m)^\s+([\w?]+):(.+)$`)
)

// enumInfo 枚举的英文成员名与显示名（displayNames() 可缺省）
type enumInfo struct{ names, display []string }

// parseTsDir 解析 GenTs 输出目录(ts/ 子目录),重建 FunMeta。
// 两遍解析:先收集全部 enum/interface 原始声明,再解析字段引用,
// 避免文件顺序(字母序)导致前置类型解析不到
func parseTsDir(tsDir string) (*FunMeta, error) {
	if _, err := os.Stat(filepath.Join(tsDir, "client.ts")); err != nil {
		return nil, fmt.Errorf("%s 不是 fun 生成的 ts 目录(缺 client.ts)", tsDir)
	}

	enums := map[string]enumInfo{}
	type rawField struct {
		Name     string
		TsType   string
		Optional bool
	}
	ifaceRaw := map[string][]rawField{}
	ifaceNode := map[string]*MetaType{}
	var serviceFiles []string

	entries, err := os.ReadDir(tsDir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".ts") {
			continue
		}
		if e.Name() == "client.ts" || e.Name() == "fun.ts" {
			continue
		}
		body, err := os.ReadFile(filepath.Join(tsDir, e.Name()))
		if err != nil {
			continue
		}
		src := string(body)
		if reServiceClass.MatchString(src) {
			serviceFiles = append(serviceFiles, src)
			continue
		}
		if m := reEnum.FindStringSubmatch(src); m != nil {
			var names []string
			for _, line := range reEnumMember.FindAllStringSubmatch(m[2], -1) {
				names = append(names, line[1])
			}
			var display []string
			if dm := reDisplayNames.FindStringSubmatch(src); dm != nil {
				for _, q := range reQuoted.FindAllStringSubmatch(dm[1], -1) {
					display = append(display, q[1])
				}
			}
			enums[m[1]] = enumInfo{names: names, display: display}
			continue
		}
		if m := reInterface.FindStringSubmatch(src); m != nil {
			var fields []rawField
			for _, f := range reField.FindAllStringSubmatch(m[2], -1) {
				name, tsType := f[1], strings.TrimSpace(f[2])
				fields = append(fields, rawField{
					Name:     strings.TrimSuffix(name, "?"),
					TsType:   tsType,
					Optional: strings.HasSuffix(name, "?"),
				})
			}
			ifaceRaw[m[1]] = fields
			ifaceNode[m[1]] = &MetaType{Kind: "struct", Name: m[1]}
		}
	}

	// 第二遍:填充 interface 字段(引用此时全部可见)
	resolve := func(tsType string) *MetaType { return parseTsType(tsType, ifaceNode, enums) }
	for name, fields := range ifaceRaw {
		node := ifaceNode[name]
		for _, f := range fields {
			ft := resolve(f.TsType)
			if f.Optional {
				ft.Optional = true
			}
			node.Fields = append(node.Fields, MetaField{Name: f.Name, Type: ft})
		}
	}

	meta := &FunMeta{}
	for _, src := range serviceFiles {
		svcName := reServiceClass.FindStringSubmatch(src)[1]
		svc := MetaService{Name: svcName}
		for _, m := range reDataMethod.FindAllStringSubmatch(src, -1) {
			mm := MetaMethod{Name: m[1], Returns: resolve(m[3])}
			if m[3] == "void" {
				mm.Void = true
				mm.Returns = nil
			}
			if m[2] != "" {
				mm.DTO = resolve(m[2])
			}
			svc.Methods = append(svc.Methods, mm)
		}
		for _, m := range reStreamMethod.FindAllStringSubmatch(src, -1) {
			mm := MetaMethod{Name: m[1], IsStream: true}
			if m[2] != "" {
				mm.DTO = resolve(m[2])
			}
			if m[3] != "any" {
				mm.Returns = resolve(m[3])
			}
			svc.Methods = append(svc.Methods, mm)
		}
		sortMetaService(&svc)
		meta.Services = append(meta.Services, svc)
	}
	sortMeta(meta)
	return meta, nil
}

// parseTsType TS 类型串 → 元数据类型树
func parseTsType(s string, interfaces map[string]*MetaType, enums map[string]enumInfo) *MetaType {
	s = strings.TrimSpace(s)
	// 流式 dto 工厂类型 "X | (() => X)" 取值类型部分
	if i := strings.Index(s, " | ("); i >= 0 {
		s = s[:i]
	}
	// fun v1.3.7 流式模板对 DTO 会多写一层 "dto:" 前缀,兼容剥离
	s = strings.TrimPrefix(s, "dto:")
	t := &MetaType{}
	if strings.HasSuffix(s, " | null") {
		t.Optional = true
		s = strings.TrimSuffix(s, " | null")
	}
	if strings.HasSuffix(s, "[]") {
		t.Kind = "slice"
		t.Elem = parseTsType(strings.TrimSuffix(s, "[]"), interfaces, enums)
		return t
	}
	switch s {
	case "number":
		t.Kind, t.Name = "int", "int64"
	case "boolean":
		t.Kind, t.Name = "bool", "bool"
	case "string":
		t.Kind, t.Name = "string", "string"
	case "any":
		t.Kind, t.Name = "string", "any"
	default:
		if info, ok := enums[s]; ok {
			t.Kind, t.Name = "enum", s
			t.Names = info.names
			t.DisplayNames = info.display
			return t
		}
		if it, ok := interfaces[s]; ok {
			return it
		}
		t.Kind, t.Name = "struct", s // 定义在其他文件(如跨包),仅名字引用
	}
	return t
}

func sortMetaService(svc *MetaService) {
	for i := 1; i < len(svc.Methods); i++ {
		for j := i; j > 0 && svc.Methods[j].Name < svc.Methods[j-1].Name; j-- {
			svc.Methods[j], svc.Methods[j-1] = svc.Methods[j-1], svc.Methods[j]
		}
	}
}

func sortMeta(meta *FunMeta) {
	for i := 1; i < len(meta.Services); i++ {
		for j := i; j > 0 && meta.Services[j].Name < meta.Services[j-1].Name; j-- {
			meta.Services[j], meta.Services[j-1] = meta.Services[j-1], meta.Services[j]
		}
	}
}
