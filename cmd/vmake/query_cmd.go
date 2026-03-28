package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gitee.com/spock2300/vmake/pkg/api"
	"gitee.com/spock2300/vmake/pkg/config"
	vlog "gitee.com/spock2300/vmake/pkg/log"
	"gitee.com/spock2300/vmake/pkg/resolver"

	"github.com/spf13/cobra"
)

func newQueryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "query",
		Short: "Show dependency tree",
		Run:   runQuery,
	}
}

func runQuery(cmd *cobra.Command, args []string) {
	vlog.SetLevel(vlog.Quiet)

	ctx, err := initContext()
	if err != nil {
		vlog.Error("%v", err)
		os.Exit(1)
	}

	if err := runRequirePhase(ctx, false); err != nil {
		vlog.Error("Phase 1 (OnRequire) failed: %v", err)
		os.Exit(1)
	}

	if err := ctx.Resolver.ResolveDeferred(); err != nil {
		vlog.Error("Phase 2 (ResolveDeferred) failed: %v", err)
		os.Exit(1)
	}
	ctx.Resolver.UpdateOrder()

	if err := runConfigPhase(ctx); err != nil {
		vlog.Error("Phase 2 (OnConfig) failed: %v", err)
		os.Exit(1)
	}

	pkgDirs := GetPackageDirs(ctx.DepGraph)
	globalValues := config.BuildGlobalValues(ctx.Config)
	workDir, _ := os.Getwd()

	graph := ctx.DepGraph.Packages
	order := ctx.Resolver.GetOrder()

	localSet := make(map[string]bool)
	dependedOn := make(map[string]bool)
	for _, name := range order {
		node := graph[name]
		if node == nil {
			continue
		}
		if node.IsLocal() {
			localSet[name] = true
		}
		for _, d := range node.Deps {
			dependedOn[d] = true
		}
	}

	roots := make([]string, 0)
	for name := range localSet {
		if !dependedOn[name] {
			roots = append(roots, name)
		}
	}
	sort.Strings(roots)

	visited := make(map[string]bool)
	for i, root := range roots {
		if i > 0 {
			fmt.Fprintln(os.Stdout)
		}
		printTree(os.Stdout, graph, ctx, pkgDirs, globalValues, workDir, root, "", true, true, visited)
	}
}

func formatPkgDir(pkgDir, workDir string) string {
	if pkgDir == "" {
		return ""
	}
	rel, err := filepath.Rel(workDir, pkgDir)
	if err != nil {
		rel = pkgDir
	}
	return fmt.Sprintf("[%s]", rel)
}

func formatPkgParts(name string, kinds []targetKindInfo, pkgDir, workDir string, ctx *RuntimeContext, globalValues map[string]any) []string {
	var parts []string
	if len(kinds) > 0 {
		parts = append(parts, kinds[0].name, fmt.Sprintf("(%s)", kinds[0].kind))
	} else {
		parts = append(parts, name)
	}
	if dir := formatPkgDir(pkgDir, workDir); dir != "" {
		parts = append(parts, dir)
	}
	if opts := formatOptions(ctx, name, globalValues); opts != "" {
		parts = append(parts, opts)
	}
	return parts
}

func printTree(
	w *os.File,
	graph map[string]*resolver.PackageNode,
	ctx *RuntimeContext,
	pkgDirs map[string]string,
	globalValues map[string]any,
	workDir, name, prefix string,
	isRoot, isLast bool,
	visited map[string]bool,
) {
	node := graph[name]
	if node == nil {
		return
	}

	connector := ""
	if !isRoot {
		if isLast {
			connector = "└── "
		} else {
			connector = "├── "
		}
	}

	isLocal := node.IsLocal()
	isVisited := visited[name]
	visited[name] = true

	if isLocal {
		kinds := collectTargetKinds(node.Pkg, name, pkgDirs[name], ctx, globalValues)
		if len(kinds) > 0 {
			parts := formatPkgParts(name, kinds, pkgDirs[name], workDir, ctx, globalValues)
			fmt.Fprintf(w, "%s%s%s\n", prefix, connector, strings.Join(parts, " "))
			for _, k := range kinds[1:] {
				indent := prefix
				if !isRoot {
					indent += "    "
				}
				fmt.Fprintf(w, "%s%s (%s)\n", indent, k.name, k.kind)
			}
		} else {
			parts := formatPkgParts(name, nil, pkgDirs[name], workDir, ctx, globalValues)
			fmt.Fprintf(w, "%s%s%s\n", prefix, connector, strings.Join(parts, " "))
		}
	} else {
		parts := formatPkgParts(name, nil, "", workDir, ctx, globalValues)
		fmt.Fprintf(w, "%s%s%s\n", prefix, connector, strings.Join(parts, " "))
	}

	if isVisited {
		return
	}

	deps := node.Deps
	if len(deps) == 0 {
		return
	}

	for i, dep := range deps {
		isLastDep := i == len(deps)-1
		var childPrefix string
		if isRoot {
			childPrefix = ""
		} else if isLast {
			childPrefix = prefix + "    "
		} else {
			childPrefix = prefix + "│   "
		}
		printTree(w, graph, ctx, pkgDirs, globalValues, workDir, dep, childPrefix, false, isLastDep, visited)
	}
}

type targetKindInfo struct {
	name string
	kind string
}

func collectTargetKinds(pkg *api.Package, name, dir string, ctx *RuntimeContext, globalValues map[string]any) []targetKindInfo {
	if pkg == nil {
		return nil
	}

	entry := config.GetEntry(ctx.Config, name)
	buildCtx := api.NewBuildContext(name, entry.Options)
	if opts, ok := ctx.AllOptions[name]; ok {
		buildCtx.SetOptions(opts)
	}
	buildCtx.MergeGlobals(ctx.GlobalOptions, globalValues)
	buildCtx.SetDryRun(true)

	pkg.ExecBuildFuncs(dir, func(fn api.BuildFunc) {
		fn(buildCtx)
	})

	targets := buildCtx.GetTargets()
	if len(targets) == 0 {
		return nil
	}

	result := make([]targetKindInfo, 0, len(targets))
	for _, t := range targets {
		result = append(result, targetKindInfo{name: t.Name(), kind: string(t.Kind())})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].name != result[j].name {
			return result[i].name < result[j].name
		}
		return result[i].kind < result[j].kind
	})
	return result
}

func formatOptions(ctx *RuntimeContext, name string, globalValues map[string]any) string {
	opts, ok := ctx.AllOptions[name]
	if !ok {
		return ""
	}

	entry := config.GetEntry(ctx.Config, name)
	acc := api.NewConfigAccessor(entry.Options, opts)
	acc.MergeGlobals(ctx.GlobalOptions, globalValues)

	var parts []string
	for _, oname := range sortedKeys(opts) {
		o := opts[oname]
		if o.IsGlobal() {
			continue
		}
		var val any
		switch o.Type() {
		case api.OptionBool:
			val = acc.Bool(oname)
		case api.OptionInt:
			val = acc.Int(oname)
		default:
			val = acc.String(oname)
		}
		parts = append(parts, fmt.Sprintf("%s=%v", oname, val))
	}

	return strings.Join(parts, ", ")
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
