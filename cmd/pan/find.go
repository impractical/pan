package main

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strconv"

	"golang.org/x/tools/go/packages"
)

const interfacePkg = `package ifacepkg

type SQLTableNamer interface {
    GetSQLTableName() string
}
`

func getTableNamerInterface() (*types.Interface, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "iface.go", interfacePkg, 0)
	if err != nil {
		panic(err)
	}

	config := &types.Config{
		Error: func(e error) {
			fmt.Println(e)
		},
		Importer: importer.Default(),
	}

	info := types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
		Defs:  make(map[*ast.Ident]types.Object),
		Uses:  make(map[*ast.Ident]types.Object),
	}

	pkg, err := config.Check("genval", fset, []*ast.File{file}, &info)
	if err != nil {
		return nil, fmt.Errorf("error parsing dummy interface code (this is always an error in pan): %w", err)
	}

	namerLookup := pkg.Scope().Lookup("SQLTableNamer")
	if namerLookup == nil {
		return nil, errors.New("dummy interface code doesn't include SQLTableNamer (this is always an error in pan)") //nolint:err113 // there's nothing to be done about this at runtime, it's for the end user
	}
	namerUnderlying := namerLookup.Type().Underlying()
	namer, ok := namerUnderlying.(*types.Interface)
	if !ok {
		return nil, fmt.Errorf("SQLTableNamer expected to be *types.Interface, got %T; this is always an error in pan", namerUnderlying) //nolint:err113 // there's nothing to be done about this at runtime, it's for the end user
	}
	return namer, nil
}

type SQLTableNamerImpl struct {
	Type        types.Type
	SourceDir   string
	Name        string
	PackageName string
	PackagePath string
	Table       string
}

func FindSQLTableNamers(ctx context.Context, dir string, pkgPatterns []string) (map[string][]SQLTableNamerImpl, error) {
	sqlTableNamer, err := getTableNamerInterface()
	if err != nil {
		return nil, err
	}
	pkgs, err := packages.Load(&packages.Config{
		Mode:    packages.NeedTypes | packages.NeedName | packages.NeedSyntax,
		Context: ctx,
		Dir:     dir,
		Tests:   true,
	}, pkgPatterns...)
	if err != nil {
		return nil, err
	}

	results := map[string][]SQLTableNamerImpl{}
	typeToTable := map[string]string{}
	for _, pkg := range pkgs {
		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			ptr := types.NewPointer(obj.Type())
			if types.Implements(ptr.Underlying(), sqlTableNamer) {
				results[pkg.Dir] = append(results[pkg.Dir], SQLTableNamerImpl{
					Type:        obj.Type(),
					SourceDir:   pkg.Dir,
					Name:        name,
					PackageName: pkg.Name,
					PackagePath: pkg.PkgPath,
				})
			}
		}
		if len(results[pkg.Dir]) < 1 {
			continue
		}
		for _, file := range pkg.Syntax {
			ast.Inspect(file, func(node ast.Node) bool {
				if node == nil {
					return false
				}

				if _, ok := node.(*ast.File); ok {
					return true
				}

				funcDecl, hasFuncDecl := node.(*ast.FuncDecl)
				if !hasFuncDecl {
					return false
				}

				if funcDecl.Name.Name != "GetSQLTableName" {
					return false
				}

				if len(funcDecl.Body.List) != 1 {
					return false
				}

				returnStmt, hasReturnStmt := funcDecl.Body.List[0].(*ast.ReturnStmt)
				if !hasReturnStmt {
					return false
				}

				if len(returnStmt.Results) != 1 {
					return false
				}

				tableName, hasTableName := returnStmt.Results[0].(*ast.BasicLit)
				if !hasTableName {
					return false
				}

				if tableName.Kind != token.STRING {
					return false
				}

				if len(funcDecl.Recv.List) != 1 {
					return false
				}

				var methodName string
				switch methodType := funcDecl.Recv.List[0].Type.(type) {
				case *ast.Ident:
					methodName = methodType.Name
				case *ast.StarExpr:
					nonStar, hasIdent := methodType.X.(*ast.Ident)
					if !hasIdent {
						return false
					}
					methodName = nonStar.Name
				default:
					return false
				}

				typeToTable[pkg.Name+"."+methodName], err = strconv.Unquote(tableName.Value)
				if err != nil {
					return false
				}

				return false
			})
		}
	}
	for dir, impls := range results {
		uniqueImpls := map[string]SQLTableNamerImpl{}
		for _, impl := range impls {
			if table, ok := typeToTable[impl.PackageName+"."+impl.Name]; ok {
				impl.Table = table
			}
			uniqueImpls[impl.PackageName+"."+impl.Name] = impl
		}
		implementerTypes := slices.Collect(maps.Values(uniqueImpls))
		slices.SortFunc(implementerTypes, func(first, second SQLTableNamerImpl) int {
			if first.PackagePath < second.PackagePath {
				return -1
			}
			if second.PackagePath < first.PackagePath {
				return 1
			}
			if first.Name < second.Name {
				return -1
			}
			if first.Name > second.Name {
				return 1
			}
			return 0
		})
		results[dir] = implementerTypes
	}

	return results, nil
}
