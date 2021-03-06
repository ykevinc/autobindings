package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"text/template"
)

var bindingsFile = `package {{.packageName}}
/*
This is an autogenerated file by autobindings
*/

import(
	"github.com/ykevinc/binding"
	"net/http"
	{{if .needsStrings}}"strings"{{end}}
)

func ({{.variableName}} *{{.structName}}) FieldMap(request *http.Request) binding.FieldMap {
	return binding.FieldMap{ {{$vname := .variableName}}{{range $field, $mapping := .mappings}}
			&{{$vname}}.{{$field}}: {{$mapping}},{{end}}
	}
}`

var enumMarshalText = `package {{.packageName}}
/*
This is an autogenerated file by autobindings
*/

func (e {{.enumType}}) MarshalText() ([]byte, error) {
	return []byte(e.String()), nil
}`

var enumBinding = `binding.Field{
			Form: {{.mappingJsonName}},
			Binder: func(fieldName string, formVals []string, errs binding.Errors) binding.Errors {
				val, ok := {{.mappingType}}_value[formVals[0]]
				if !ok {
					errs.Add([]string{fieldName}, binding.DeserializationError, formVals[0])
				}
				enum := {{.mappingType}}(val)
				{{.variableName}}.{{.mappingFieldName}} = enum
				return errs
			},
		}`

var enumArrayBinding = `binding.Field{
			Form: {{.mappingJsonName}},
			Binder: func(fieldName string, formVals []string, errs binding.Errors) binding.Errors {
				{{.variableName}}.{{.mappingFieldName}} = make([]{{.mappingType}}, 0, len(formVals))
				for _, formVal := range strings.Split(formVals[0], ",") {
					val, ok := {{.mappingType}}_value[formVal]
					if !ok {
						errs.Add([]string{fieldName}, binding.DeserializationError, formVals[0])
					}
					{{.variableName}}.{{.mappingFieldName}} = append({{.variableName}}.{{.mappingFieldName}}, {{.mappingType}}(val))
				}
				return errs
			},
		}`

func main() {

	prnt := flag.Bool("print", false, "Output In Console")
	filename := flag.String("file", "", "Input file")

	flag.Parse()

	if *filename == "" {
		fmt.Println("Usage : bindings {file_name}\nExample: bindings file.go")
		return
	}

	generateFieldMap(*filename, *prnt)
}

func generateFieldMap(fileName string, printOnConsole bool) {
	fset := token.NewFileSet() // positions are relative to fset
	// Parse the file given in arguments
	f, err := parser.ParseFile(fset, fileName, nil, parser.ParseComments)
	if err != nil {
		panic(err)
	}
	structMap := map[string]*ast.FieldList{}
	// range over the structs and fill struct map
	for _, d := range f.Scope.Objects {
		ts, ok := d.Decl.(*ast.TypeSpec)
		if !ok {
			continue
		}
		switch ts.Type.(type) {
			case *ast.StructType:
			x, _ := ts.Type.(*ast.StructType)
			structMap[ts.Name.String()] = x.Fields
		}
	}
	// looping through each struct and creating a bindings file for it
	packageName := f.Name
	for structName, fields := range structMap {
		variableName := strings.ToLower(string(structName[0]))
		mappings := map[string]string{}
		protobuffMappings := map[string]string{}
		isArray := map[string]bool{}

		for _, field := range fields.List {
			if len(field.Names) == 0 {
				continue
			}
			name := field.Names[0].String()
			// if tag for field doesn't exist, create one
			if field.Tag == nil {
				mappings[name] = name
			} else if strings.Contains(field.Tag.Value, "json") {
				tags := strings.Replace(field.Tag.Value, "`", "", -1)
				for _, tag := range strings.Split(tags, " ") {
					if strings.Contains(tag, "json") {
						mapping := strings.Replace(tag, "json:\"", "", -1)
						mapping = strings.Replace(mapping, "\"", "", -1)
						if mapping == "-" {
							continue
						}
						mappings[name] = fmt.Sprintf("\"%s\"", mapping)
					}
				}
			} else {
				// I will handle other cases later
				mappings[name] = name
			}

			//see if we have a protobuff enum to handle
			if nil != field.Tag && strings.Contains(field.Tag.Value, "protobuf:") {
				for _, tag := range strings.Split(field.Tag.Value, " ") {
					if strings.Contains(tag, "protobuf:") {
						enumPosition := strings.Index(tag, "enum=")

						if enumPosition > -1 {
							//BUGBUG: Assumes enums are at the end
							enumType := tag[enumPosition+5 : len(tag)-1]
							//fmt.Printf("ENUM AT %d in %s\n",enumPosition, tag)

							//strip off the package name if it matches the current package
							if 0 == strings.Index(enumType, packageName.Name) {
								enumType = enumType[len(packageName.Name)+1:]
							}

							protobuffMappings[name] = enumType
							//fmt.Printf("APPENDED: %s", enumType)
							_, ok := field.Type.(*ast.ArrayType)
							if ok {
								isArray[name] = true
							}
						}
					}
				}
			}

		}

		needsStrings := false
		for k, e := range protobuffMappings {
			// Write a different file per enum, so that they simply overwrite one another in the event
			// that we process them multiple times
			{
				content := new(bytes.Buffer)
				t := template.Must(template.New("enumMarshalText").Parse(enumMarshalText))
				err = t.Execute(content, map[string]interface{}{"packageName": packageName, "enumType": e})
				if err != nil {
					panic(err)
				}
				finalContent, err := format.Source(content.Bytes())
				if err != nil {
					panic(err)
				}
				//fmt.Printf("Content for %s:\n%s\n", e, string(finalContent))
				// opening file for writing content
				writer, err := os.Create(fmt.Sprintf("%s_enum_bindings.go", strings.ToLower(e)))
				if err != nil {
					fmt.Printf("Error opening file %v", err)
					panic(err)
				}
				writer.WriteString(string(finalContent))
				writer.Close()
			}

			//Also, stomp on the simple json mappings with our own protobuff mappings
			{
				binderContent := new(bytes.Buffer)
				var binderTemplate *template.Template
				if isArray[k] {
					binderTemplate = template.Must(template.New("enumArrayBinding").Parse(enumArrayBinding))
					needsStrings = true
				} else {
					binderTemplate = template.Must(template.New("enumBinding").Parse(enumBinding))
				}
				params := map[string]interface{}{"variableName": variableName, "mappingType": e, "mappingFieldName": k, "mappingJsonName": mappings[k]}
				err = binderTemplate.Execute(binderContent, params)
				if err != nil {
					panic(err)
				}
				finalContent, err := format.Source(binderContent.Bytes())
				if err != nil {
					panic(err)
				}
				mappings[k] = string(finalContent)
				//fmt.Println(mappings[k])
			}
		}

		content := new(bytes.Buffer)
		t := template.Must(template.New("bindings").Parse(bindingsFile))
		err = t.Execute(content, map[string]interface{}{
			"packageName":  packageName,
			"variableName": variableName,
			"structName":   structName,
			"mappings":     mappings,
			"needsStrings": needsStrings})
		if err != nil {
			panic(err)
		}

		finalContent, err := format.Source(content.Bytes())
		if err != nil {
			panic(err)
		}
		if printOnConsole {
			fmt.Println(string(finalContent))
			return
		}
		// opening file for writing content
		writer, err := os.Create(fmt.Sprintf("%s_bindings.go", strings.ToLower(structName)))
		if err != nil {
			fmt.Printf("Error opening file %v", err)
			panic(err)
		}
		writer.WriteString(string(finalContent))
		writer.Close()

	}
}
