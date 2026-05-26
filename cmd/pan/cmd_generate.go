package main

import (
	"context"
	"fmt"
	"go/types"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"impractical.co/clif"
	"impractical.co/pan"

	"github.com/dave/jennifer/jen"
)

var generateCmd = clif.Command{
	Name:                "generate",
	Description:         "generate the column mapping files",
	DetailedDescription: "pan generate finds all the struct types in the passed packages that implement the impractical.co/pan.SQLTableNamer interface and generates a companion type and constant for them. The companion types define a method for each struct field mapped to a SQL column, and return the SQL column name when called. impractical.co/pan.Flag values can be passed to quote or qualify returned column names. The constant provides a global value of the companion type, for ease of calling and avoidance of allocations.",
	UsageExample:        "pan generate ./pkg1 ./pkg2/pkg3",
	ArgsAccepted:        true,
	Flags: []clif.FlagDef{
		{
			Name:        "--type-suffix",
			Description: "the suffix to append to the struct type's name to construct the companion type's name",
			Default: []clif.FlagValue{
				{
					HasValue:     true,
					Raw:          "Columns",
					CanonicalKey: "--type-suffix",
				},
			},
		},
		{
			Name:        "--table-constant-suffix",
			Description: "the suffix to append to the type name to create a constant holding the table's name. Only works if the SQLTableNamer just returns a string literal in its GetSQLTableName function. Set to \"\" to disable",
			Default: []clif.FlagValue{
				{
					HasValue:     true,
					Raw:          "Table",
					CanonicalKey: "--table-constant-suffix",
				},
			},
		},
		{
			Name:          "--all-columns-method",
			Description:   "the name of the method that should be generated to return all of the column names as a slice of strings. Can be specified multiple times. Will use the first name for each type that doesn't conflict with a field on the type, or omit the method if no non-conflicting name is specified. Set to the empty string to disable generating this method.",
			AllowMultiple: true,
			Default: []clif.FlagValue{
				{
					HasValue:     true,
					Raw:          "List",
					CanonicalKey: "--all-columns-method",
				},
				{
					HasValue:     true,
					Raw:          "All",
					CanonicalKey: "--all-columns-method",
				},
				{
					HasValue:     true,
					Raw:          "PanList",
					CanonicalKey: "--all-columns-method",
				},
				{
					HasValue:     true,
					Raw:          "PanAll",
					CanonicalKey: "--all-columns-method",
				},
			},
		},
		{
			Name:        "--generated-filename",
			Description: "the filename to save the generated code under in each package",
			Default: []clif.FlagValue{
				{
					HasValue:     true,
					Raw:          "pan_gen.go",
					CanonicalKey: "--generated-filename",
				},
			},
		},
	},
	Handler: generateHandlerBuilder{},
}

type generateHandlerBuilder struct{}

// Build creates a Handler by parsing the flags and args into the
// appropriate handler type.
func (generateHandlerBuilder) Build(ctx context.Context, flags clif.FlagSet, args []string, resp *clif.Response) clif.Handler { //nolint:ireturn // Have to return an interface to implement an interface
	var handler generateHandler
	err := flags.As(ctx, &handler)
	if err != nil {
		resp.Error.Write([]byte("Error parsing flags: " + err.Error())) //nolint:errcheck // nothing to be done if we can't report errors
		resp.Code = 1
		return nil
	}
	handler.Packages = args
	return handler
}

type generateHandler struct {
	TypeSuffix          string   `flag:"type-suffix"`
	ColumnMismatchCheck bool     `flag:"column-mismatch-check"`
	AllColumnsMethod    []string `flag:"all-columns-method"`
	GeneratedFilename   string   `flag:"generated-filename"`
	TableConstantSuffix string   `flag:"table-constant-suffix"`
	Packages            []string `flag:"-"`
}

// Handle is a method that will be called when the command is executed.
// It should contain the business logic of the command.
func (handler generateHandler) Handle(ctx context.Context, resp *clif.Response) {
	implPackages, err := FindSQLTableNamers(ctx, "", handler.Packages)
	if err != nil {
		fmt.Fprintln(resp.Error, "Error searching for SQLTableNamer implementations:", err) //nolint:errcheck // nothing to be done if we can't report errors
		resp.Code = 1
		return
	}
	for dir, impls := range implPackages {
		handler.generateForPackage(ctx, dir, impls, resp)
		if resp.Code > 0 {
			return
		}
	}
}

func (handler generateHandler) generateForPackage(_ context.Context, dir string, impls []SQLTableNamerImpl, resp *clif.Response) {
	var packageName, packagePath string
	columnsByFieldByType := map[string]map[string]string{}
	tables := map[string]string{}
	for _, impl := range impls {
		if packageName == "" {
			packageName = impl.PackageName
		}
		if packagePath == "" {
			packagePath = impl.PackagePath
		}
		structInfo, ok := impl.Type.Underlying().(*types.Struct)
		if !ok {
			fmt.Fprintf(resp.Error, "%s in %s (package %s) is a %T, not a *types.Struct\r\n", impl.Name, impl.SourceDir, impl.PackagePath, impl.Type.Underlying()) //nolint:errcheck // nothing to be done if we can't report errors
			resp.Code = 1
			return
		}
		columnsByFieldByType[impl.Name] = map[string]string{}
		if impl.Table != "" {
			tables[impl.Name] = impl.Table
		}
		for fieldNum := range structInfo.NumFields() {
			field := structInfo.Field(fieldNum)
			tag := structInfo.Tag(fieldNum)
			columnName := pan.ColumnName(field.Name(), tag)
			if columnName == "" {
				continue
			}
			columnsByFieldByType[impl.Name][field.Name()] = columnName
		}
	}
	file := jen.NewFile(packageName)
	file.HeaderComment(`Code generated by impractical.co/pan. DO NOT EDIT.`)
	typeNames := slices.Collect(maps.Keys(columnsByFieldByType))
	slices.Sort(typeNames)
	file.Const().DefsFunc(func(defsGroup *jen.Group) {
		for _, name := range typeNames {
			defsGroup.Comment(fmt.Sprintf("%s provides helper functions to retrieve the mapped column names for fields on [%s].", name+handler.TypeSuffix, name))
			defsGroup.Id(name + handler.TypeSuffix).Id("columns_" + name).Op("=").Lit(0)
		}
		if handler.TableConstantSuffix != "" {
			defsGroup.Line()
			tableNames := slices.Collect(maps.Keys(tables))
			slices.Sort(tableNames)
			for _, name := range tableNames {
				table := tables[name]
				defsGroup.Commentf("%s is the name of the table mapped to %s.", name+handler.TableConstantSuffix, name)
				defsGroup.Id(name + handler.TableConstantSuffix).Op("=").Lit(table)
			}
		}
	})
	allMethodFuncsByTypes := map[string]string{}
	for _, name := range typeNames {
		tableName, tableKnown := tables[name]
		table := jen.Id(name + handler.TableConstantSuffix)
		if handler.TableConstantSuffix == "" {
			table = jen.Lit(tableName)
		}
		methodTable := table
		if !tableKnown {
			table = jen.Qual("impractical.co/pan", "Table").Call(jen.Id(name).Block())
			methodTable = jen.Id("tableName")
		}
		file.Type().Id("columns_" + name).Int8()
		fields := columnsByFieldByType[name]
		fieldNames := slices.Collect(maps.Keys(fields))
		slices.Sort(fieldNames)
		for _, fieldName := range fieldNames {
			column := fields[fieldName]
			file.Commentf("%s returns the database column name (%q) that [%s.%s] maps to.", fieldName, column, name, fieldName)
			file.Func().Params(jen.Id("columns_" + name)).Id(fieldName).Params(jen.Id("flags").Op("...").Qual("impractical.co/pan", "Flag")).String().Block(
				jen.Return(jen.Qual("impractical.co/pan", "DecorateColumn").Call(jen.Lit(column), table, jen.Id("flags").Op("..."))),
			)
		}
		for _, methodName := range handler.AllColumnsMethod {
			if methodName == "" {
				continue
			}
			if slices.Contains(fieldNames, methodName) {
				continue
			}
			allMethodFuncsByTypes[name] = methodName
			file.Comment(fmt.Sprintf("%s returns a sorted list of all database columns mapped to [%s].", methodName, name))
			file.Func().Params(jen.Id("columns_" + name)).Id(methodName).Params(jen.Id("flags").Op("...").Qual("impractical.co/pan", "Flag")).Index().String().BlockFunc(func(funcValues *jen.Group) {
				if !tableKnown {
					funcValues.Id("tableName").Op(":=").Qual("impractical.co/pan", "Table").Call(jen.Id(name).Block())
				}
				funcValues.Return(jen.Index().String().ValuesFunc(func(returnValues *jen.Group) {
					for _, fieldName := range fieldNames {
						column := fields[fieldName]
						returnValues.Line().Qual("impractical.co/pan", "DecorateColumn").Call(jen.Lit(column), methodTable, jen.Id("flags").Op("..."))
					}
					returnValues.Line().Empty()
				}))
			})
			break
		}
	}
	testFile := jen.NewFile(packageName)
	testFile.HeaderComment(`Code generated by impractical.co/pan. DO NOT EDIT.`)
	testFile.Func().Id("TestColumnMismatch").Params(jen.Id("t").Op("*").Qual("testing", "T")).BlockFunc(func(testGroup *jen.Group) {
		for _, name := range typeNames {
			columns := slices.Collect(maps.Values(columnsByFieldByType[name]))
			slices.Sort(columns)
			testGroup.Id("sortedColumns_"+name).Op(":=").Qual("impractical.co/pan", "Columns").Call(jen.Id(name).Block())
			testGroup.Qual("slices", "Sort").Call(jen.Id("sortedColumns_" + name))
			if allMethod, ok := allMethodFuncsByTypes[name]; ok {
				testGroup.Id("sortedGenColumns_" + name).Op(":=").Id(name + handler.TypeSuffix).Dot(allMethod).Call()
			} else {
				testGroup.Id("sortedGenColumns_" + name).Op(":=").Index().String().ValuesFunc(func(columnsGroup *jen.Group) {
					for _, column := range columns {
						columnsGroup.Line().Lit(column)
					}
					columnsGroup.Line().Empty()
				})
			}
			testGroup.Qual("slices", "Sort").Call(jen.Id("sortedGenColumns_" + name))
			testGroup.If(
				jen.Op("!").Qual("slices", "Equal").Call(
					jen.Id("sortedColumns_"+name),
					jen.Id("sortedGenColumns_"+name),
				).BlockFunc(func(ifBlock *jen.Group) {
					ifBlock.Id("t").Dot("Fatalf").Call(jen.Lit(fmt.Sprintf("Column mismatch for %s.%s! This is usually caused by an out-of-date %s file. Try regenerating it.\r\n\r\nRuntime values: %%v\r\nGenerated values: %%v", packagePath, name, handler.GeneratedFilename)), jen.Id("sortedColumns_"+name), jen.Id("sortedGenColumns_"+name))
				}),
			)
		}
	})
	outputPath := filepath.Join(dir, handler.GeneratedFilename)
	output, err := os.Create(outputPath) //nolint:gosec // the arguments come from a CLI; no new security issues are created
	if err != nil {
		fmt.Fprintf(resp.Error, "Error writing to %s: %s\r\n", outputPath, err.Error()) //nolint:errcheck // nothing to be done if we can't report errors
		resp.Code = 1
		return
	}
	defer func() {
		err = output.Close()
		if err != nil {
			fmt.Fprintf(resp.Error, "Error closing %s after writing: %s\r\n", outputPath, err.Error()) //nolint:errcheck // nothing to be done if we can't report errors
			resp.Code = 1
		}
	}()
	err = file.Render(output)
	if err != nil {
		fmt.Fprintf(resp.Error, "Error generating helper code for %s: %s\r\n", packagePath, err.Error()) //nolint:errcheck // nothing to be done if we can't report errors
		resp.Code = 1
		return
	}
	outputFilename, outputExtension, _ := strings.Cut(handler.GeneratedFilename, ".")
	testOutputFilename := outputFilename + "_test." + outputExtension
	testOutputPath := filepath.Join(dir, testOutputFilename)
	testOutput, err := os.Create(testOutputPath) //nolint:gosec // variable inclusion is unavoidable and doesn't open up a new capability
	if err != nil {
		fmt.Fprintf(resp.Error, "Error writing to %s: %s\r\n", testOutputPath, err.Error()) //nolint:errcheck // nothing to be done if we can't report errors
		resp.Code = 1
		return
	}
	defer func() {
		err = testOutput.Close()
		if err != nil {
			fmt.Fprintf(resp.Error, "Error closing %s after writing: %s\r\n", testOutputPath, err.Error()) //nolint:errcheck // nothing to be done if we can't report errors
			resp.Code = 1
		}
	}()
	err = testFile.Render(testOutput)
	if err != nil {
		fmt.Fprintf(resp.Error, "Error generating helper code test for %s: %s\r\n", packagePath, err.Error()) //nolint:errcheck // nothing to be done if we can't report errors
		resp.Code = 1
		return
	}
	fmt.Fprintln(resp.Output, "Generated SQL column mapping code for", packagePath, "in", outputPath) //nolint:errcheck // nothing to be done if we can't report to stdout
}
