// Command openapi generates the HTTP route inventory and inferable JSON request
// schemas from the actual Gin registrations, handler comments and struct tags.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

type object = map[string]any
type group struct {
	path                 string
	authenticated, admin bool
}
type generator struct {
	functions map[string]*ast.FuncDecl
	types     map[string]ast.Expr
	paths     map[string]object
	schemas   map[string]any
}

func main() {
	check := flag.Bool("check", false, "fail if the checked-in specification is stale")
	out := flag.String("out", "docs/api/openapi.json", "output path")
	flag.Parse()
	data, err := generate(".")
	if err != nil {
		fail(err)
	}
	if *check {
		current, err := os.ReadFile(*out)
		if err != nil || !bytes.Equal(current, data) {
			fail(fmt.Errorf("OpenAPI is stale; run go run ./tools/openapi"))
		}
		return
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o750); err != nil {
		fail(err)
	}
	if err := os.WriteFile(*out, data, 0o600); err != nil {
		fail(err)
	}
	fmt.Println("Generated", *out)
}

func generate(root string) ([]byte, error) {
	g := &generator{functions: map[string]*ast.FuncDecl{}, types: map[string]ast.Expr{}, paths: map[string]object{}, schemas: map[string]any{}}
	var routes *ast.File
	for _, directory := range []string{"internal/handler", "internal/server"} {
		files, err := filepath.Glob(filepath.Join(root, directory, "*.go"))
		if err != nil {
			return nil, err
		}
		for _, name := range files {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			file, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ParseComments)
			if err != nil {
				return nil, err
			}
			if filepath.Base(name) == "server.go" && directory == "internal/server" {
				routes = file
			}
			for _, declaration := range file.Decls {
				switch d := declaration.(type) {
				case *ast.FuncDecl:
					g.functions[d.Name.Name] = d
				case *ast.GenDecl:
					for _, spec := range d.Specs {
						if t, ok := spec.(*ast.TypeSpec); ok {
							g.types[t.Name.Name] = t.Type
						}
					}
				}
			}
		}
	}
	if routes == nil {
		return nil, fmt.Errorf("server routes not found")
	}
	for _, decl := range routes.Decls {
		if f, ok := decl.(*ast.FuncDecl); ok && f.Name.Name == "setupRoutes" {
			if err := g.walk(f.Body.List, map[string]group{"s.engine": {}}); err != nil {
				return nil, err
			}
		}
	}
	if len(g.paths) < 100 {
		return nil, fmt.Errorf("route extraction incomplete: %d paths", len(g.paths))
	}
	g.schemas["APIError"] = object{"type": "object", "required": []string{"code", "message", "error_code"}, "properties": object{"code": object{"type": "integer", "description": "HTTP status; existing clients retain this contract"}, "message": object{"type": "string"}, "error_code": object{"type": "string", "description": "Stable category or existing domain error code"}}}
	document := object{"openapi": "3.1.0", "info": object{"title": "DraARL HTTP API", "version": "2.0.1-alpha4", "description": "Generated from Gin registrations and request struct tags. Response payloads and protocol-specific authentication are documented in the usage manual; absent response schemas are intentional, not inferred guarantees."}, "paths": g.paths, "components": object{"securitySchemes": object{"bearerAuth": object{"type": "http", "scheme": "bearer", "bearerFormat": "JWT"}}, "schemas": g.schemas}}
	data, err := json.MarshalIndent(document, "", "  ")
	return append(data, '\n'), err
}

func exprName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return exprName(e.X) + "." + e.Sel.Name
	case *ast.CallExpr:
		return exprName(e.Fun) + "()"
	}
	return ""
}
func literal(expr ast.Expr) (string, bool) {
	v, ok := expr.(*ast.BasicLit)
	if !ok || v.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(v.Value)
	return s, err == nil
}
func clone(groups map[string]group) map[string]group {
	result := map[string]group{}
	for k, v := range groups {
		result[k] = v
	}
	return result
}
func applyMiddleware(gr group, args []ast.Expr) group {
	for _, arg := range args {
		name := exprName(arg)
		if strings.Contains(name, "AuthMiddleware") {
			gr.authenticated = true
		}
		if strings.Contains(name, "RequireAdmin") {
			gr.admin = true
		}
	}
	return gr
}

func (g *generator) walk(statements []ast.Stmt, groups map[string]group) error {
	for _, statement := range statements {
		switch st := statement.(type) {
		case *ast.BlockStmt:
			if err := g.walk(st.List, clone(groups)); err != nil {
				return err
			}
		case *ast.IfStmt:
			if err := g.walk(st.Body.List, clone(groups)); err != nil {
				return err
			}
		case *ast.AssignStmt:
			if len(st.Lhs) != 1 || len(st.Rhs) != 1 {
				continue
			}
			call, ok := st.Rhs[0].(*ast.CallExpr)
			if !ok {
				continue
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Group" || len(call.Args) == 0 {
				continue
			}
			parent, ok := groups[exprName(sel.X)]
			if !ok {
				continue
			}
			suffix, ok := literal(call.Args[0])
			if !ok {
				return fmt.Errorf("nonliteral route group")
			}
			parent.path += suffix
			groups[exprName(st.Lhs[0])] = applyMiddleware(parent, call.Args[1:])
		case *ast.ExprStmt:
			call, ok := st.X.(*ast.CallExpr)
			if !ok {
				continue
			}
			if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "registerRadioRoutes" && len(call.Args) == 1 {
				gr, ok := groups[exprName(call.Args[0])]
				if !ok {
					return fmt.Errorf("unknown radio group")
				}
				if err := g.walk(g.functions[id.Name].Body.List, map[string]group{"radio": gr}); err != nil {
					return err
				}
				continue
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			name := exprName(sel.X)
			gr, ok := groups[name]
			if !ok {
				continue
			}
			if sel.Sel.Name == "Use" {
				groups[name] = applyMiddleware(gr, call.Args)
				continue
			}
			method := sel.Sel.Name
			if !strings.Contains("|GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|", "|"+method+"|") || len(call.Args) < 2 {
				continue
			}
			suffix, ok := literal(call.Args[0])
			if !ok {
				return fmt.Errorf("nonliteral route")
			}
			gr = applyMiddleware(gr, call.Args[1:])
			if err := g.operation(gr, method, suffix, call.Args[len(call.Args)-1]); err != nil {
				return err
			}
		}
	}
	return nil
}

var parameterPattern = regexp.MustCompile(`[:*]([A-Za-z_][A-Za-z0-9_]*)`)
var idPattern = regexp.MustCompile(`[^A-Za-z0-9]+`)

func (g *generator) operation(gr group, method, suffix string, handler ast.Expr) error {
	original := gr.path + suffix
	path := parameterPattern.ReplaceAllString(original, "{$1}")
	if g.paths[path] == nil {
		g.paths[path] = object{}
	}
	if _, exists := g.paths[path][strings.ToLower(method)]; exists {
		return fmt.Errorf("duplicate %s %s", method, path)
	}
	name := exprName(handler)
	parts := strings.Split(name, ".")
	fn := g.functions[parts[len(parts)-1]]
	op := object{"operationId": strings.ToLower(method) + "_" + strings.Trim(idPattern.ReplaceAllString(path, "_"), "_"), "summary": name, "x-handler": name, "responses": object{"2XX": object{"description": "Successful response; see handler-specific usage documentation"}, "4XX": object{"description": "Invalid input, authentication, permission or missing resource", "content": object{"application/json": object{"schema": object{"$ref": "#/components/schemas/APIError"}}}}, "5XX": object{"description": "Dependency failure or internal error"}}}
	if gr.authenticated {
		op["security"] = []object{{"bearerAuth": []string{}}}
	}
	if gr.admin {
		op["x-required-role"] = "admin"
	}
	parameters := []object{}
	for _, match := range parameterPattern.FindAllStringSubmatch(original, -1) {
		parameters = append(parameters, object{"name": match[1], "in": "path", "required": true, "schema": object{"type": "string"}})
	}
	if fn != nil {
		if fn.Doc != nil {
			op["description"] = strings.TrimSpace(fn.Doc.Text())
		}
		variables := map[string]ast.Expr{}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if decl, ok := n.(*ast.ValueSpec); ok && decl.Type != nil {
				for _, id := range decl.Names {
					variables[id.Name] = decl.Type
				}
			}
			return true
		})
		queries := map[string]bool{}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if sel.Sel.Name == "Query" || sel.Sel.Name == "DefaultQuery" {
				if key, ok := literal(call.Args[0]); ok && !queries[key] {
					queries[key] = true
					parameters = append(parameters, object{"name": key, "in": "query", "required": false, "schema": object{"type": "string"}})
				}
			}
			if sel.Sel.Name == "ShouldBindJSON" {
				if address, ok := call.Args[0].(*ast.UnaryExpr); ok {
					if typ := variables[exprName(address.X)]; typ != nil {
						op["requestBody"] = object{"required": true, "content": object{"application/json": object{"schema": g.schema(typ, 0)}}}
					}
				}
			}
			return true
		})
	}
	if len(parameters) > 0 {
		op["parameters"] = parameters
	}
	g.paths[path][strings.ToLower(method)] = op
	return nil
}

func (g *generator) schema(expr ast.Expr, depth int) object {
	if depth > 8 {
		return object{"type": "object"}
	}
	switch t := expr.(type) {
	case *ast.StarExpr:
		return object{"anyOf": []object{g.schema(t.X, depth+1), {"type": "null"}}}
	case *ast.ArrayType:
		return object{"type": "array", "items": g.schema(t.Elt, depth+1)}
	case *ast.MapType:
		return object{"type": "object", "additionalProperties": g.schema(t.Value, depth+1)}
	case *ast.Ident:
		switch t.Name {
		case "string":
			return object{"type": "string"}
		case "bool":
			return object{"type": "boolean"}
		case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "byte":
			return object{"type": "integer"}
		case "float32", "float64":
			return object{"type": "number"}
		}
		if declaration, ok := g.types[t.Name]; ok {
			if _, seen := g.schemas[t.Name]; !seen {
				g.schemas[t.Name] = object{}
				g.schemas[t.Name] = g.schema(declaration, depth+1)
			}
			return object{"$ref": "#/components/schemas/" + t.Name}
		}
	case *ast.SelectorExpr:
		if exprName(t) == "time.Time" {
			return object{"type": "string", "format": "date-time"}
		}
	case *ast.StructType:
		properties := object{}
		required := []string{}
		for _, field := range t.Fields.List {
			if len(field.Names) == 0 {
				continue
			}
			tag := ""
			if field.Tag != nil {
				tag, _ = strconv.Unquote(field.Tag.Value)
			}
			jsonTag := reflect.StructTag(tag).Get("json")
			if jsonTag == "-" {
				continue
			}
			name := strings.Split(jsonTag, ",")[0]
			if name == "" {
				name = field.Names[0].Name
			}
			properties[name] = g.schema(field.Type, depth+1)
			if strings.Contains(","+reflect.StructTag(tag).Get("binding")+",", ",required,") {
				required = append(required, name)
			}
		}
		result := object{"type": "object", "properties": properties}
		if len(required) > 0 {
			result["required"] = required
		}
		return result
	}
	return object{}
}

func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
