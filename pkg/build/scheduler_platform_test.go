package build

import (
	"bytes"
	"debug/elf"
	"debug/pe"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func TestSchedulerPackagePlatformArtifacts(t *testing.T) {
	for _, rootOS := range []string{"linux", "windows"} {
		t.Run(rootOS, func(t *testing.T) {
			targets := map[string]map[string]*api.Target{}
			for _, name := range []string{"linux", "windows", "fallback"} {
				targets[name] = map[string]*api.Target{
					"lib": makeTargetWithDeps("lib").SetKind(api.TargetShared).SetDefault(true),
					"app": makeTargetWithDeps("app", "lib").SetKind(api.TargetBinary).SetDefault(true),
				}
			}
			s, graph := newTestScheduler(t, targets)
			s.platform = api.Platform{OS: rootOS}
			var commands [][]string
			s.linker = &Linker{ccPath: "cc", run: func(_ string, dir string, args ...string) ([]byte, error) {
				commands = append(commands, append([]string{}, args...))
				for i, arg := range args {
					var path string
					if arg == "-o" {
						path = args[i+1]
					} else if strings.HasPrefix(arg, "-Wl,--out-implib=") {
						path = strings.TrimPrefix(arg, "-Wl,--out-implib=")
					}
					if path != "" {
						if err := os.WriteFile(resolveWorkPath(dir, path), []byte("artifact"), 0644); err != nil {
							return nil, err
						}
					}
				}
				return nil, nil
			}}
			for name, info := range s.pkgs {
				info.OutputDir = info.BuildDir
				info.InstallDir = filepath.Join(info.SourceDir, "install")
				if name != "fallback" {
					s.SetPackage(name, api.NewPackage().SetPlatform(api.Platform{OS: name}))
				}
			}
			if err := s.BuildAll(); err != nil {
				t.Fatal(err)
			}
			for name, info := range s.pkgs {
				targetOS := name
				if name == "fallback" {
					targetOS = rootOS
				}
				libName, appName := "liblib.so", "app"
				if targetOS == "windows" {
					libName, appName = "liblib.dll", "app.exe"
				}
				for _, filename := range []string{libName, appName} {
					for _, dir := range []string{info.OutputDir, filepath.Join(info.InstallDir, "lib")} {
						if _, err := os.Stat(filepath.Join(dir, filename)); err != nil {
							t.Errorf("%s package artifact: %v", name, err)
						}
					}
				}
				if targetOS != "windows" {
					continue
				}
				implib := filepath.Join(info.OutputDir, libName+".a")
				if !slices.ContainsFunc(commands, func(args []string) bool {
					return slices.Contains(args, commandPath(info.SourceDir, implib))
				}) {
					t.Errorf("%s consumer did not link import library %s: %v", name, implib, commands)
				}
				installed := filepath.Join(info.InstallDir, "lib", libName+".a")
				if err := os.Remove(installed); err != nil {
					t.Errorf("published import library: %v", err)
					continue
				}
				if err := s.Build(name + ":lib"); err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(installed); err != nil {
					t.Errorf("did not restore published import library: %v", err)
				}
				if err := os.Remove(implib); err != nil {
					t.Fatal(err)
				}
				resolved, err := s.resolveTarget(graph.Nodes[name+":lib"])
				if err != nil {
					t.Fatal(err)
				}
				if !s.needRelink(resolved, nil) {
					t.Error("missing package import library did not require relink")
				}
			}
			consumer := graph.Nodes["linux:app"]
			consumer.Deps = []string{"windows:lib"}
			s.pkgs["windows"].OutputDir = ""
			deps, err := s.collectDepArtifacts(consumer)
			if err != nil {
				t.Fatal(err)
			}
			want := filepath.Join(s.pkgs["windows"].InstallDir, "lib", "liblib.dll.a")
			if !slices.Equal(deps.artifacts, []string{want}) {
				t.Errorf("installed producer artifact = %v, want %s", deps.artifacts, want)
			}
		})
	}
}

func TestSchedulerPackagePlatformLinkPolicy(t *testing.T) {
	for _, kind := range []api.TargetKind{api.TargetBinary, api.TargetShared} {
		t.Run(string(kind), func(t *testing.T) {
			target := makeTargetWithDeps("app").SetKind(kind).SetDefault(true).AddExcludeLibs("private")
			s, _ := newTestScheduler(t, makeTargets("windows", target))
			s.platform = api.Platform{OS: "linux"}
			s.SetPackage("windows", api.NewPackage().SetPlatform(api.Platform{OS: "windows"}))
			s.pkgs["windows"].OutputDir = s.pkgs["windows"].BuildDir
			s.linker, _ = recordingLinker()
			if err := s.Build("windows:app"); err == nil || !strings.Contains(err.Error(), "not supported for PE targets") {
				t.Fatalf("ELF policy on Windows package: %v", err)
			}
		})
	}
}

func TestSchedulerPackagePlatformConcurrentAssembly(t *testing.T) {
	s, graph := newTestScheduler(t, map[string]map[string]*api.Target{
		"linux":   {"asm": makeTargetWithDeps("asm")},
		"windows": {"asm": makeTargetWithDeps("asm")},
	})
	s.platform = api.Platform{OS: "linux"}
	tools := &ResolvedTools{CC: "clang", CCVersion: "clang version test", targetOS: "linux"}
	s.compiler = NewCompiler(tools)
	s.ccWriter = NewCompileCommandsWriter(tools)
	var elfBytes, coffBytes bytes.Buffer
	elfHeader := elf.Header64{Type: uint16(elf.ET_REL), Machine: uint16(elf.EM_X86_64), Version: uint32(elf.EV_CURRENT), Ehsize: 64}
	copy(elfHeader.Ident[:], []byte{0x7f, 'E', 'L', 'F', byte(elf.ELFCLASS64), byte(elf.ELFDATA2LSB), byte(elf.EV_CURRENT)})
	if err := binary.Write(&elfBytes, binary.LittleEndian, elfHeader); err != nil {
		t.Fatal(err)
	}
	if err := binary.Write(&coffBytes, binary.LittleEndian, pe.FileHeader{Machine: pe.IMAGE_FILE_MACHINE_AMD64}); err != nil {
		t.Fatal(err)
	}
	coffBytes.Write(make([]byte, 96-coffBytes.Len()))
	var mu sync.Mutex
	commands := map[string][]string{}
	s.compiler.run = func(_ string, dir string, args ...string) ([]byte, error) {
		mu.Lock()
		commands[dir] = append([]string{}, args...)
		mu.Unlock()
		obj := resolveWorkPath(dir, args[slices.Index(args, "-o")+1])
		data := elfBytes.Bytes()
		if filepath.Base(dir) == "windows" {
			data = coffBytes.Bytes()
		}
		if err := os.WriteFile(obj, data, 0644); err != nil {
			return nil, err
		}
		dep := resolveWorkPath(dir, args[slices.Index(args, "--MD")+2])
		return nil, os.WriteFile(dep, []byte("output.o: source.s\n"), 0644)
	}
	var wg sync.WaitGroup
	for name, info := range s.pkgs {
		info.OutputDir = info.BuildDir
		writeAssemblyFixture(t, info.SourceDir, "source.s", ".byte 1\n")
		pkg := api.NewPackage().SetPlatform(api.Platform{OS: name, Triple: name + "-triple"})
		s.SetPackage(name, pkg)
	}
	for name := range s.pkgs {
		wg.Go(func() {
			resolved := &ResolvedTarget{Node: graph.Nodes[name+":asm"], AllCFlags: []string{"--target=" + s.packages[name].TargetTriple()}}
			if _, _, err := s.compileSource(resolved, "source.s"); err != nil {
				t.Errorf("%s package assembly: %v", name, err)
			}
		})
	}
	wg.Wait()
	if s.compiler.targetOS != "linux" {
		t.Errorf("shared compiler platform changed to %s", s.compiler.targetOS)
	}
	for _, command := range s.ccWriter.commands {
		flag := "--target=" + filepath.Base(command.Directory) + "-triple"
		if !slices.Contains(command.Arguments, flag) || !slices.Contains(commands[command.Directory], flag) {
			t.Errorf("compile command lost package flags: database %v, actual %v", command.Arguments, commands[command.Directory])
		}
	}
}

func TestBuildSubGraphPreservesPackagePlatforms(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}
	git, err = filepath.Abs(git)
	if err != nil {
		t.Fatal(err)
	}
	tc := &toolchain.Toolchain{Name: "platform-test", Tools: toolchain.Tools{CC: git, CXX: git, AR: git}}
	params := &SubGraphParams{
		Platform: api.Platform{OS: "none", Triple: "root-triple"}, RootDir: t.TempDir(),
		AllTargets: map[string]map[string]*api.Target{}, PkgDirs: map[string]*api.PkgDirs{},
		Packages: map[string]*api.Package{}, Needed: map[string]bool{"root": true, "dep": true},
		PkgMeta: map[string]PkgBuildMeta{"root": {}, "dep": {}},
	}
	for name, platform := range map[string]api.Platform{
		"root": {OS: "windows", Triple: "windows-triple"},
		"dep":  {OS: "linux", Triple: "linux-triple"},
	} {
		dirs := api.PkgDirs{SourceDir: filepath.Join(params.RootDir, name), BuildDir: filepath.Join(params.RootDir, name, "build")}
		pkg := api.NewPackage().SetPlatform(platform).SetDirs(dirs)
		params.Packages[name] = pkg
		params.PkgDirs[name] = &dirs
		target := makeTargetWithDeps(name).SetKind(api.TargetVoid).SetDefault(true).SetBuildFunc(func(p *api.Package) error {
			if p.TargetOS() != platform.OS || p.TargetTriple() != platform.Triple {
				return fmt.Errorf("%s platform = %s/%s, want %s/%s", name, p.TargetOS(), p.TargetTriple(), platform.OS, platform.Triple)
			}
			return nil
		})
		if name == "root" {
			target.AddDeps("dep:dep")
		}
		params.AllTargets[name] = map[string]*api.Target{name: target}
	}
	if err := BuildSubGraph("root", tc, tc.Name, api.ModeDebug, params, nil); err != nil {
		t.Fatal(err)
	}
}
