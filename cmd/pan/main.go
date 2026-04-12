package main

import (
	"context"
	"os"
	"os/signal"
	"runtime/debug"

	"impractical.co/clif"
)

const DefaultTypeSuffix = "Columns"

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	version := "unknown"
	info, ok := debug.ReadBuildInfo()
	if ok {
		version = info.Main.Version
	}

	app := clif.Application{
		Name:                "pan",
		Version:             version,
		Description:         "A CLI for generating mapping between SQL table columns and Go types.",
		DetailedDescription: "pan helps manage the files that are generated as aids for the impractical.co/pan package. Its main command, `pan generate`, finds all the structs in one or more packages that implement the impractical.co/pan.SQLTableNamer interface and generates types for them. These types have a method on them for each struct field mapped to a SQL column, and the methods return the SQL column name, optionally with various quotation options. This allows the compiler-assisted referencing of database column names from within your Go code.",
		Commands: []clif.Command{
			generateCmd,
		},
		Flags: []clif.FlagDef{
			{
				Name:        "--help",
				Aliases:     []string{"-h"},
				Description: "Show usage information.",
				OnlyToggle:  true,
				IsToggle:    true,
			},
			{
				Name:        "--version",
				Description: "Show version information.",
				OnlyToggle:  true,
				IsToggle:    true,
			},
		},
		GlobalMiddleware: []clif.Middleware{
			clif.HelpMiddleware{
				Flags: []string{"help"},
			},
			clif.VersionMiddleware{
				Flags: []string{"version"},
			},
		},
	}
	os.Exit(app.Run(ctx))
}
