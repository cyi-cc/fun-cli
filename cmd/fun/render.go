package main

// 渲染层共享类型与工具：模板数据结构与 fun v1.3.7 gen.go 保持一致，
// 保证产物字节级不变

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/template"
)

type genType struct {
	GenServiceList []*genServiceType
}

type genMethodType struct {
	MethodName      string
	ReturnValueText string
	DtoText         string
	ArgsText        string
	GenericTypeText string
	IsProxy         bool
	IsStream        bool
}

type genEnumType struct {
	Names        []string
	DisplayNames []string
	Name         string
}

type genImportType struct {
	Name string
}

type genServiceType struct {
	ServiceName       string
	GenMethodTypeList []*genMethodType
	GenImport         []*genImportType
	IsIncludeProxy    bool
	IsIncludeRequest  bool
	IsIncludeStream   bool
}

type genClassType struct {
	Name              string
	GenImport         []*genImportType
	GenClassFieldType []*genClassFieldType
}

type genClassFieldType struct {
	Name string
	Type string
	Tag  string
}

func deduplicateServiceImports(imports []*genImportType) []*genImportType {
	seen := make(map[string]bool)
	var result []*genImportType
	for _, imp := range imports {
		if !seen[imp.Name] {
			seen[imp.Name] = true
			result = append(result, imp)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func parseGenericTypeParams(typeName string) string {
	start := strings.Index(typeName, "[")
	end := strings.LastIndex(typeName, "]")
	paramsStr := typeName[start+1 : end]
	params := strings.Split(paramsStr, ",")
	for i, param := range params {
		LL := strings.Split(strings.TrimSpace(param), ".")
		params[i] = firstLetterToUpper(LL[len(LL)-1])
	}
	return strings.Join(params, "")
}

func getGenericTypeName(typeName string) string {
	start := strings.Index(typeName, "[")
	return typeName[0:start]
}

func camelToSnake(s string) string {
	re := regexp.MustCompile(`([a-z0-9])([A-Z])`)
	snake := re.ReplaceAllString(s, `${1}_${2}`)
	return strings.ToLower(snake)
}

func firstLetterToUpper(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func firstLetterToLower(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// fileSet 渲染产物集合：相对路径 → 内容
type fileSet map[string][]byte

// renderCode 模板渲染（等价旧 genCode 的模板执行部分，写盘由调用方统一处理）
func renderCode(templateContent string, outputFileName string, templateData any, languageName string, files fileSet) {
	tmpl, err := template.New(languageName).Parse(templateContent)
	if err != nil {
		panic(err.Error())
	}
	var buf bytes.Buffer
	err = tmpl.Execute(&buf, templateData)
	if err != nil {
		panic(err.Error())
	}
	files[filepath.Join(languageName, outputFileName+"."+languageName)] = buf.Bytes()
}

// writeFiles 清空输出目录后写入全部产物
func writeFiles(dir string, files fileSet) error {
	if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, files[name], 0o644); err != nil {
			return err
		}
	}
	return nil
}
