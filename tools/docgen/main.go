// Command docgen writes the generated pages of the user documentation.
//
// The CLI reference comes from the Cobra command tree, and the configuration
// reference comes from the structs in internal/types plus the defaults in
// internal/config. Run it from the repository root:
//
//	go run ./tools/docgen
//	go run ./tools/docgen -check
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/noket-dev/noket/internal/cli"
	"github.com/noket-dev/noket/internal/config"
	"github.com/noket-dev/noket/internal/types"
)

func main() {
	out := flag.String("out", "docs/src/content/docs/reference", "output directory, relative to the repository root")
	check := flag.Bool("check", false, "report stale generated files instead of writing them")
	flag.Parse()

	root := cli.New("dev")
	// Cobra adds these during Execute, which the generator never calls.
	root.InitDefaultVersionFlag()
	root.InitDefaultCompletionCmd()

	files := map[string][]byte{}
	for name, data := range generateCLI(root) {
		files[filepath.Join(*out, name)] = data
	}

	cfg, err := generateConfig()
	if err != nil {
		fail(err)
	}
	files[filepath.Join(*out, "config.md")] = cfg

	if err := emit(files, *out, *check); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

// emit writes files, or with check reports every file that differs. Generated
// CLI pages that no longer belong to a command are removed or reported too.
func emit(files map[string][]byte, out string, check bool) error {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	var stale []string
	for _, path := range paths {
		current, err := os.ReadFile(path)
		switch {
		case err == nil && bytes.Equal(current, files[path]):
			continue
		case err == nil:
			stale = append(stale, path)
		case os.IsNotExist(err):
			stale = append(stale, path)
		default:
			return err
		}
	}

	orphans, err := orphanCLIPages(out, files)
	if err != nil {
		return err
	}
	stale = append(stale, orphans...)

	if check {
		if len(stale) == 0 {
			return nil
		}
		return fmt.Errorf("generated docs are out of date:\n  %s\nrun: go run ./tools/docgen",
			strings.Join(stale, "\n  "))
	}

	for _, path := range stale {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, files[path], 0o644); err != nil {
			return err
		}
	}
	fmt.Printf("wrote %d generated pages under %s\n", len(paths), out)
	return nil
}

func orphanCLIPages(out string, files map[string][]byte) ([]string, error) {
	dir := filepath.Join(out, "cli")
	var orphans []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		if _, ok := files[path]; !ok {
			orphans = append(orphans, path)
		}
		return nil
	})
	return orphans, err
}

// generateCLI renders one page per visible command, keyed by a path relative to
// the output directory. The command tree is alphabetical because Cobra sorts
// subcommands, which keeps the output stable.
func generateCLI(root *cobra.Command) map[string][]byte {
	files := map[string][]byte{}
	var walk func(cmd *cobra.Command, path []string, order int)
	walk = func(cmd *cobra.Command, path []string, order int) {
		name := "cli/index.md"
		title := "noket"
		if len(path) > 0 {
			name = filepath.Join(append([]string{"cli"}, path...)...) + ".md"
			title = "noket " + strings.Join(path, " ")
		}
		files[name] = renderCommand(cmd, title, path, order)

		for i, child := range visibleCommands(cmd) {
			walk(child, append(path, child.Name()), i+1)
		}
	}
	walk(root, nil, 0)
	return files
}

func renderCommand(cmd *cobra.Command, title string, path []string, order int) []byte {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "title: %s\n", yamlString(title))
	fmt.Fprintf(&b, "description: %s\n", yamlString(oneLine(cmd.Short)))
	if order > 0 {
		b.WriteString("sidebar:\n")
		fmt.Fprintf(&b, "  order: %d\n", order)
	}
	b.WriteString("---\n\n")

	if len(path) > 0 {
		parent := "noket"
		if len(path) > 1 {
			parent = "noket " + strings.Join(path[:len(path)-1], " ")
			fmt.Fprintf(&b, "%s\n\n", cmd.Short)
			fmt.Fprintf(&b, "Part of [`%s`](/%s).\n\n", parent, strings.TrimSuffix(strings.TrimPrefix(strings.Join(append([]string{"reference", "cli"}, path[:len(path)-1]...), "/"), "/"), "/")+"/")
		} else {
			fmt.Fprintf(&b, "%s\n\n", cmd.Short)
			fmt.Fprintf(&b, "Part of [`%s`](/reference/cli/).\n\n", parent)
		}
	} else {
		fmt.Fprintf(&b, "%s\n\n", cmd.Short)
	}

	b.WriteString("## Usage\n\n```sh\n")
	if len(visibleCommands(cmd)) > 0 {
		b.WriteString(cmd.CommandPath() + " <command> [flags]")
	} else {
		b.WriteString(cmd.UseLine())
	}
	b.WriteString("\n```\n\n")

	if cmd.Long != "" && oneLine(cmd.Long) != oneLine(cmd.Short) {
		b.WriteString(oneLine(cmd.Long))
		b.WriteString("\n\n")
	}

	if children := visibleCommands(cmd); len(children) > 0 {
		b.WriteString("## Commands\n\n| Command | Description |\n| --- | --- |\n")
		for _, child := range children {
			linkPath := append(append([]string{"reference", "cli"}, path...), child.Name())
			fmt.Fprintf(&b, "| [`%s`](/%s/) | %s |\n", child.Name(), strings.Join(linkPath, "/"), escapeCell(child.Short))
		}
		b.WriteString("\n")
	}

	writeFlags(&b, "Flags", cmd.NonInheritedFlags())
	writeFlags(&b, "Inherited flags", cmd.InheritedFlags())

	return []byte(b.String())
}

func writeFlags(b *strings.Builder, title string, flags *pflag.FlagSet) {
	rows := flagRows(flags)
	if len(rows) == 0 {
		return
	}
	fmt.Fprintf(b, "## %s\n\n| Flag | Type | Default | Description |\n| --- | --- | --- | --- |\n", title)
	for _, row := range rows {
		fmt.Fprintf(b, "| %s | `%s` | %s | %s |\n", row.name, row.kind, row.def, row.usage)
	}
	b.WriteString("\n")
}

func visibleCommands(cmd *cobra.Command) []*cobra.Command {
	var out []*cobra.Command
	for _, child := range cmd.Commands() {
		if child.Name() == "help" {
			continue
		}
		if child.IsAvailableCommand() {
			out = append(out, child)
		}
	}
	return out
}

type flagRow struct {
	name  string
	kind  string
	def   string
	usage string
}

func flagRows(flags *pflag.FlagSet) []flagRow {
	var rows []flagRow
	flags.VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		name := "`--" + f.Name + "`"
		if f.Shorthand != "" {
			name = "`-" + f.Shorthand + "`, " + name
		}
		rows = append(rows, flagRow{
			name:  name,
			kind:  friendlyFlagType(f.Value.Type()),
			def:   defaultCell(f),
			usage: escapeCell(f.Usage),
		})
	})
	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })
	return rows
}

func friendlyFlagType(typ string) string {
	switch typ {
	case "stringArray", "stringSlice":
		return "string array"
	case "stringToString":
		return "string map"
	default:
		return typ
	}
}

func defaultCell(f *pflag.Flag) string {
	switch f.DefValue {
	case "", "false", "0", "[]", "nil":
		return "—"
	}
	return "`" + f.DefValue + "`"
}

func escapeCell(s string) string {
	s = oneLine(s)
	s = strings.ReplaceAll(s, "|", `\|`)
	if s == "" {
		return "—"
	}
	return s
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func yamlString(s string) string {
	return strconv.Quote(s)
}

// generateConfig renders the config.toml reference from the struct comments in
// internal/types and the defaults applied by internal/config.
func generateConfig() ([]byte, error) {
	fields, err := parseStructs("internal/types/config.go")
	if err != nil {
		return nil, err
	}

	var b strings.Builder
	b.WriteString(`---
title: Configuration file
description: Every key in config.toml, with its type, default, and meaning.
sidebar:
  order: 1
---

` + "`noket` and `noketd` read their local configuration from `config.toml` in the [config directory](/reference/files/). " +
		"The file is the source of truth for the upstream list: `noket upstream add|edit|remove` and upstream changes made in the Noket console both update it. " +
		"Editing it by hand is supported.\n\n" +
		"The decoder rejects unknown keys, so a typo fails with the line number instead of being silently ignored. " +
		"`noketd` reloads the file on its next heartbeat; a parse failure keeps the previous configuration. " +
		"If the file changes while an edit is open, the command stops instead of overwriting the newer file.\n\n")

	def := config.Default()
	defaults := map[string]reflect.Value{
		"APIConfig":    reflect.ValueOf(def.API),
		"Tunnel":       reflect.ValueOf(def.Tunnel),
		"NoketdConfig": reflect.ValueOf(def.Noketd),
		"Upstream":     reflect.Zero(reflect.TypeOf(types.Upstream{})),
	}

	sections := []struct {
		Type  string
		Table string
	}{
		{"APIConfig", "[api]"},
		{"Tunnel", "[tunnel]"},
		{"NoketdConfig", "[noketd]"},
		{"Upstream", "[[upstreams]]"},
	}

	for _, section := range sections {
		list, ok := fields[section.Type]
		if !ok {
			return nil, fmt.Errorf("struct %s not found in internal/types/config.go", section.Type)
		}
		fmt.Fprintf(&b, "## `%s`\n\n| Key | Type | Default | Description |\n| --- | --- | --- | --- |\n", section.Table)
		value := defaults[section.Type]
		for _, field := range list {
			fmt.Fprintf(&b, "| `%s` | `%s` | %s | %s |\n",
				field.toml, friendlyType(field.typ), field.defaultValue(value), escapeCell(field.doc))
		}
		b.WriteString("\n")
	}

	example, err := os.ReadFile("config.example.toml")
	if err != nil {
		return nil, fmt.Errorf("read config.example.toml: %w", err)
	}
	b.WriteString("## Full example\n\nThe file below is [`config.example.toml`](https://github.com/noket-dev/noket/blob/main/config.example.toml) from the repository.\n\n```toml\n")
	b.Write(example)
	if len(example) > 0 && example[len(example)-1] != '\n' {
		b.WriteString("\n")
	}
	b.WriteString("```\n")

	return []byte(b.String()), nil
}

type structField struct {
	name string
	toml string
	typ  string
	doc  string
}

func (f structField) defaultValue(value reflect.Value) string {
	if !value.IsValid() {
		return "—"
	}
	field := value.FieldByName(f.name)
	if !field.IsValid() {
		return "—"
	}
	if f.typ == "Duration" {
		d := time.Duration(field.Int())
		if d == 0 {
			return "—"
		}
		return "`" + d.String() + "`"
	}
	switch field.Kind() {
	case reflect.String:
		if field.String() == "" {
			return "—"
		}
		return "`" + field.String() + "`"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if field.Int() == 0 {
			return "—"
		}
		return "`" + strconv.FormatInt(field.Int(), 10) + "`"
	case reflect.Slice, reflect.Map:
		if field.Len() == 0 {
			return "—"
		}
		return "—"
	case reflect.Struct:
		return "—"
	default:
		return "—"
	}
}

func parseStructs(path string) (map[string][]structField, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	result := map[string][]structField{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				continue
			}
			var list []structField
			for _, field := range structType.Fields.List {
				if len(field.Names) != 1 || field.Tag == nil {
					continue
				}
				toml := tomlTag(field.Tag.Value)
				if toml == "" || toml == "-" {
					continue
				}
				var typeBuf bytes.Buffer
				if err := printer.Fprint(&typeBuf, fset, field.Type); err != nil {
					return nil, err
				}
				doc := ""
				if field.Doc != nil {
					doc = field.Doc.Text()
				} else if field.Comment != nil {
					doc = field.Comment.Text()
				}
				list = append(list, structField{
					name: field.Names[0].Name,
					toml: toml,
					typ:  typeBuf.String(),
					doc:  oneLine(doc),
				})
			}
			result[typeSpec.Name.Name] = list
		}
	}
	return result, nil
}

func tomlTag(raw string) string {
	raw = strings.Trim(raw, "`")
	for _, part := range strings.Fields(raw) {
		key, value, ok := strings.Cut(part, ":")
		if !ok || key != "toml" {
			continue
		}
		value = strings.Trim(value, `"`)
		name, _, _ := strings.Cut(value, ",")
		return name
	}
	return ""
}

func friendlyType(typ string) string {
	switch typ {
	case "Duration":
		return "duration string"
	case "[]string":
		return "string array"
	case "map[string]string":
		return "string map"
	case "int64":
		return "integer"
	case "[]Upstream":
		return "array of upstream tables"
	default:
		return typ
	}
}
