package build

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spock2300/vmake/pkg/api"
)

func (s *Scheduler) needsCXXLink(node *BuildNode, sources []string) (bool, error) {
	return s.needsCXXLinkVisited(node, sources, make(map[string]bool))
}

func (s *Scheduler) needsCXXLinkVisited(node *BuildNode, sources []string, visited map[string]bool) (bool, error) {
	for _, source := range sources {
		if sourceLanguage(source) == "cxx" {
			return true, nil
		}
	}
	for _, name := range node.Deps {
		if visited[name] {
			continue
		}
		visited[name] = true
		dep, err := s.graph.GetNode(name)
		if err != nil {
			return false, err
		}
		kind := dep.Target.Kind()
		if !dep.Target.IsLinkable() || dep.Target.Prebuilt() != "" || (kind != api.TargetStatic && kind != api.TargetObject) {
			continue
		}
		if node.Target.Kind() == api.TargetObject || (node.Target.Kind() == api.TargetStatic && kind != api.TargetObject) {
			continue
		}
		info, err := s.pkgInfoOf(dep.PkgName)
		if err != nil {
			return false, err
		}
		files, err := targetSourceFiles(dep.Target, info)
		if err != nil {
			return false, err
		}
		if needsCXX, err := s.needsCXXLinkVisited(dep, files, visited); needsCXX || err != nil {
			return needsCXX, err
		}
	}
	return false, nil
}

func externalLinkInputs(workDir, targetOS, libraryPath string, links, flags []string) []string {
	var dirs, inputs, args []string
	libraries := append([]string(nil), links...)
	addFile := func(name string) {
		path := resolveWorkPath(workDir, name)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			inputs = append(inputs, path)
		}
	}
	for _, flag := range flags {
		if linkerArgs, ok := strings.CutPrefix(flag, "-Wl,"); ok {
			args = append(args, strings.Split(linkerArgs, ",")...)
		} else if flag != "-Xlinker" {
			args = append(args, flag)
		}
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "-install_name", "-dylib_install_name", "-soname", "--soname", "-h", "-o", "-Map", "--out-implib", "-rpath", "--rpath", "-rpath-link", "--rpath-link", "-framework", "-weak_framework", "-reexport_framework", "-lazy_framework":
			i++
			continue
		}

		if (arg == "-L" || arg == "-l" || arg == "--library" || arg == "--library-path") && i+1 < len(args) {
			i++
			if len(arg) > 2 {
				arg += "="
			}
			arg += args[i]
		}
		switch {
		case strings.HasPrefix(arg, "--library-path="):
			dirs = append(dirs, strings.TrimPrefix(arg, "--library-path="))
		case strings.HasPrefix(arg, "--library="):
			libraries = append(libraries, strings.TrimPrefix(arg, "--library="))
		case strings.HasPrefix(arg, "-L") && len(arg) > 2:
			dirs = append(dirs, arg[2:])
		case strings.HasPrefix(arg, "-l") && len(arg) > 2:
			libraries = append(libraries, arg[2:])
		case !strings.HasPrefix(arg, "-") && isLibraryArtifact(arg):
			addFile(arg)
		}
	}
	dirs = append(dirs, filepath.SplitList(libraryPath)...)
	libraries = unique(libraries)
	for _, dir := range unique(dirs) {
		for _, lib := range libraries {
			for _, name := range linkLibraryNames(lib, targetOS) {
				addFile(filepath.Join(dir, name))
			}
		}
	}
	return unique(inputs)
}

func linkLibraryNames(lib, targetOS string) []string {
	if name, exact := strings.CutPrefix(lib, ":"); exact {
		return []string{name}
	}
	base := "lib" + lib
	switch targetOS {
	case "darwin":
		return []string{base + ".dylib", base + ".tbd", base + ".so", base + ".a"}
	case "windows":
		return []string{base + ".dll.a", lib + ".dll.a", base + ".a", lib + ".lib", base + ".lib", "cyg" + lib + ".dll", base + ".dll", lib + ".dll"}
	default:
		return []string{base + ".so", base + ".a"}
	}
}
