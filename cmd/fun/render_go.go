package main

// Go 渲染器：fun v1.3.7 gen_go.go 的等价移植，输入从 reflect.Type 换成元数据类型树

import "strings"

type renderGo struct{}

// typeName 等价旧 typeToTemplateType：指针 → "*" 前缀，切片 → "[]"，其余用原始类型名
func (renderGo) typeName(t *MetaType) string {
	prefix := ""
	if t.Optional {
		prefix = "*"
	}
	if t.Kind == "slice" {
		return prefix + "[]" + renderGo{}.typeName(t.Elem)
	}
	return prefix + t.Name
}

func (r renderGo) genService(svc MetaService, serviceContext *genServiceType, files fileSet) {
	for _, gm := range svc.Methods {
		var returnValueText string
		var dtoText string
		var argsText string
		var genericTypeText string

		// () error：无数据返回，客户端类型为 Void
		if gm.Void {
			genericTypeText = "Void"
			returnValueText = "Result[Void]"
			if gm.DTO != nil {
				v := r.typeName(gm.DTO)
				if !strings.Contains(v, "[]") && strings.Contains(v, "[") {
					dtoText += "dto " + getGenericTypeName(v) + parseGenericTypeParams(v)
				} else {
					dtoText += "dto " + v
				}
				argsText += ",dto"
				r.genStruct(gm.DTO, files)
			}
			serviceContext.GenMethodTypeList = append(serviceContext.GenMethodTypeList, &genMethodType{
				MethodName:      gm.Name,
				ReturnValueText: returnValueText,
				DtoText:         dtoText,
				ArgsText:        argsText,
				GenericTypeText: genericTypeText,
			})
			continue
		}

		if gm.IsStream {
			serviceContext.IsIncludeStream = true
			if gm.Returns == nil {
				genericTypeText = "any"
				returnValueText = "Void"
			} else {
				t := r.typeName(gm.Returns)
				if !strings.Contains(t, "[]") && strings.Contains(t, "[") {
					genericTypeText = getGenericTypeName(t) + parseGenericTypeParams(t)
				} else {
					genericTypeText = t
				}
				returnValueText = genericTypeText
				r.genReturnTypes(gm.Returns, files)
			}
		} else {
			t := r.typeName(gm.Returns)
			if !strings.Contains(t, "[]") && strings.Contains(t, "[") {
				returnValueText = getGenericTypeName(t) + parseGenericTypeParams(t)
			} else {
				returnValueText = t
			}
			genericTypeText = returnValueText
			r.genReturnTypes(gm.Returns, files)
			returnValueText = "Result[" + returnValueText + "]"
		}

		if gm.DTO != nil {
			v := r.typeName(gm.DTO)
			if !strings.Contains(v, "[]") && strings.Contains(v, "[") {
				dtoText += "dto " + getGenericTypeName(v) + parseGenericTypeParams(v)
			} else {
				dtoText += "dto " + v
			}
			argsText += ",dto"
			r.genStruct(gm.DTO, files)
		}

		serviceContext.GenMethodTypeList = append(serviceContext.GenMethodTypeList, &genMethodType{
			MethodName:      gm.Name,
			ReturnValueText: returnValueText,
			DtoText:         dtoText,
			ArgsText:        argsText,
			GenericTypeText: genericTypeText,
			IsStream:        gm.IsStream,
		})
	}
	renderCode(templateGo{}.genServiceTemplate(), camelToSnake(svc.Name), serviceContext, "go", files)
}

// genReturnTypes 递归生成返回类型涉及的 struct/enum 定义
func (r renderGo) genReturnTypes(returnType *MetaType, files fileSet) {
	if returnType.Kind == "struct" {
		r.genStruct(returnType, files)
	}
	if returnType.Kind == "slice" && returnType.Elem.Kind == "struct" {
		r.genStruct(returnType.Elem, files)
	}
	if returnType.Kind == "enum" {
		r.getEnum(returnType, files)
	}
}

func (r renderGo) renderAll(meta *FunMeta, files fileSet) {
	genContext := genType{GenServiceList: []*genServiceType{}}

	for _, svc := range meta.Services {
		serviceContext := &genServiceType{
			ServiceName:       svc.Name,
			GenMethodTypeList: []*genMethodType{},
		}
		genContext.GenServiceList = append(genContext.GenServiceList, serviceContext)
		r.genService(svc, serviceContext, files)
	}
	renderCode(templateGo{}.genDefaultServiceTemplate(), "fun", genContext, "go", files)
}

func (r renderGo) genStruct(t *MetaType, files fileSet) *genImportType {
	var structTemplate genClassType
	if strings.Contains(t.Name, "[]") == false && strings.Contains(t.Name, "[") {
		structTemplate = genClassType{
			Name: getGenericTypeName(t.Name) + parseGenericTypeParams(t.Name),
		}
	} else {
		structTemplate = genClassType{
			Name: t.Name,
		}
	}

	for _, field := range t.Fields {
		fieldType := field.Type
		jsType := r.typeName(fieldType)
		tag := "`json:\"" + firstLetterToLower(field.Name) + "\"`"
		if !strings.Contains(jsType, "[]") && strings.Contains(jsType, "[") {
			structTemplate.GenClassFieldType = append(structTemplate.GenClassFieldType, &genClassFieldType{
				Name: field.Name,
				Type: getGenericTypeName(jsType) + parseGenericTypeParams(jsType),
				Tag:  tag,
			})
		} else {
			structTemplate.GenClassFieldType = append(structTemplate.GenClassFieldType, &genClassFieldType{
				Name: field.Name,
				Type: jsType,
				Tag:  tag,
			})
		}

		if fieldType.Kind == "struct" {
			r.genStruct(fieldType, files)
		}
		if fieldType.Kind == "slice" && fieldType.Elem.Kind == "struct" {
			r.genStruct(fieldType.Elem, files)
		}
		if fieldType.Kind == "enum" {
			r.getEnum(fieldType, files)
		}
	}

	renderCode(
		templateGo{}.genStructTemplate(),
		camelToSnake(structTemplate.Name),
		structTemplate,
		"go",
		files,
	)
	return &genImportType{}
}

func (r renderGo) getEnum(t *MetaType, files fileSet) *genImportType {
	var enumTemplate genEnumType
	enumTemplate.Names = t.Names
	enumTemplate.DisplayNames = t.DisplayNames
	enumTemplate.Name = t.Name

	renderCode(
		templateGo{}.genEnumTemplate(),
		camelToSnake(t.Name),
		enumTemplate,
		"go",
		files,
	)
	return &genImportType{}
}
