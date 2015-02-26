package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"strings"
	"text/template"

	"go/parser"
	"go/token"
	"os"
)

var bindingsFile = `package {{.packageName}}
/*
DO NOT EDIT THIS FILE
This is an autogenerated file by autobindings
*/

import(
	"github.com/mholt/binding"
)

func ({{.variableName}} *{{.structName}}) FieldMap() binding.FieldMap {
	return binding.FieldMap{ {{$vname := .variableName}}{{range $field, $mapping := .mappings}}
			&{{$vname}}.{{$field}}: "{{$mapping}}",{{end}}
	}
}`

func main() {
	args := os.Args[1:]
	if len(args) < 1 {
		fmt.Println("Usage : bindings {file_name} \n example: bindings file.go")
		return
	}
	generateFieldMap(args[0])
}

func generateFieldMap(fileName string) {
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
			panic(fmt.Sprintf("Unknown type without TypeSec: %v", d))
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
		for _, field := range fields.List {
			if len(field.Names) == 0 {
				continue
			}
			name := field.Names[0].String()
			// if tag for field doesn't exists, create one
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
						mappings[name] = mapping
					}
				}
			} else {
				// I will handle other cases later
				mappings[name] = name
			}
		}
		// opening file for writing content
		writer, err := os.Create(fmt.Sprintf("%s_bindings.go", strings.ToLower(structName)))
		if err != nil {
			fmt.Printf("Error opening file %v", err)
			panic(err)
		}
		defer writer.Close()
		content := new(bytes.Buffer)
		t := template.Must(template.New("bindings").Parse(bindingsFile))
		err = t.Execute(content, map[string]interface{}{
			"packageName":  packageName,
			"variableName": variableName,
			"structName":   structName,
			"mappings":     mappings})
		if err != nil {
			panic(err)
		}
		finalContent, err := format.Source(content.Bytes())
		if err != nil {
			panic(err)
		}
		writer.WriteString(string(finalContent))
	}
}
