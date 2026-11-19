package build

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spock2300/vmake/internal/fs"
)

// compilePlan is the single derivation of a compile command set. The commands
// are both hashed (by the scheduler's plannedAction) and executed, so the
// signature can never describe a command that is not run.
type compilePlan struct {
	Commands       []commandSpec
	Display        commandSpec
	DepPath        string
	DepFiles       []string
	Intermediate   string
	Outputs        []string
	Asm            bool
	ValidateObject bool
}

func (c *Compiler) planCompile(src, objPath string, opts *CompileOptions, workDir string) (compilePlan, error) {
	return c.planCompileHost(runtime.GOOS, src, objPath, opts, workDir)
}

func (c *Compiler) planCompileHost(hostOS, src, objPath string, opts *CompileOptions, workDir string) (compilePlan, error) {
	commandOpts := *opts
	commandOpts.clang = c.clangCC
	plan := compilePlan{
		DepPath: objPath + ".d",
		Outputs: []string{objPath, objPath + ".d"},
		Asm:     commandOpts.Language == "asm" || commandOpts.Language == "asm-cpp",
	}
	compiler, flags := selectCompilerAndFlags(c.ccPath, c.cxxPath, commandOpts.CFlags, commandOpts.CxxFlags, &commandOpts)

	if commandOpts.clang && plan.Asm {
		asmSource := src
		if commandOpts.Language == "asm-cpp" {
			intermediate := objPath + ".s"
			ppDep := plan.DepPath + ".pp"
			ppOpts := commandOpts
			ppOpts.Includes = commandPathsFor(hostOS, workDir, opts.Includes)
			parts := compileArgParts{
				prefix:  []string{"-E", "-x", "assembler-with-cpp", "-MMD", "-MP", "-MF", commandPathFor(hostOS, workDir, ppDep), "-MT", commandPathFor(hostOS, workDir, objPath), "-o", commandPathFor(hostOS, workDir, intermediate)},
				options: appendCompileOptions(nil, &ppOpts, commandFlagsFor(hostOS, workDir, flags)),
				suffix:  []string{commandPathFor(hostOS, workDir, src)},
			}
			plan.Commands = append(plan.Commands, compileCommandForHost(hostOS, c.ccPath, parts, workDir, opts.rspDir))
			plan.DepFiles = []string{ppDep}
			plan.Intermediate = intermediate
			asmSource = intermediate
		}

		asmOpts := commandOpts
		asmOpts.Language = "asm"
		asmDep := plan.DepPath + ".as"
		asmDepPath := asmDep
		if c.targetOS == "darwin" {
			asmDepPath = ""
			asmDep = ""
		}
		if asmDep != "" {
			plan.DepFiles = append(plan.DepFiles, asmDep)
		}
		plan.Commands = append(plan.Commands, compileCommandForHost(hostOS, c.ccPath, compileArgPartsFor(hostOS, &asmOpts, objPath, asmSource, flags, asmDepPath, workDir), workDir, opts.rspDir))
		plan.ValidateObject = true

		plan.Display = commandSpec{Program: c.ccPath, Args: compileArgPartsFor(hostOS, &commandOpts, objPath, src, flags, "", workDir).args()}
		return plan, nil
	}

	plan.Commands = []commandSpec{compileCommandForHost(hostOS, compiler, compileArgPartsFor(hostOS, &commandOpts, objPath, src, flags, plan.DepPath, workDir), workDir, opts.rspDir)}
	plan.Display = commandSpec{Program: compiler, Args: compileArgPartsFor(hostOS, &commandOpts, objPath, src, flags, "", workDir).args()}

	if plan.Asm {
		plan.DepFiles = []string{plan.DepPath}
		if commandOpts.Language == "asm-cpp" {
			plan.DepFiles = append(plan.DepFiles, plan.DepPath+".as")
			plan.Intermediate = strings.TrimSuffix(objPath, filepath.Ext(objPath)) + ".s"
		}
	}
	return plan, nil
}

func (c *Compiler) executeCompilePlan(plan compilePlan, src, objPath, workDir string) (deps []string, err error) {
	if err := fs.EnsureDir(filepath.Dir(resolveWorkPath(workDir, objPath))); err != nil {
		return nil, err
	}
	depPath := plan.DepPath
	defer func() {
		cleanupErr := removeCompileFiles(workDir, depPath+".as", depPath+".pp")
		if err != nil {
			cleanupErr = errors.Join(cleanupErr, removeCompileFiles(workDir, objPath, depPath, plan.Intermediate))
		}
		err = errors.Join(err, cleanupErr)
	}()

	for _, command := range plan.Commands {
		if err := ensureResponseFile(command.response); err != nil {
			return nil, err
		}
		if _, runErr := c.run(command.Program, workDir, command.Args...); runErr != nil {
			return nil, runErr
		}
	}

	if plan.ValidateObject {
		if err := validateAssemblyObject(resolveWorkPath(workDir, objPath), c.targetOS); err != nil {
			return nil, fmt.Errorf("Clang external assembler output: %w; check the toolchain --target and -B flags", err)
		}
	}
	if plan.Asm {
		return mergeAssemblyDeps(src, objPath, plan.DepPath, workDir, plan.Intermediate, plan.DepFiles...)
	}
	deps, err = ParseDepFile(resolveWorkPath(workDir, plan.DepPath))
	if err != nil {
		return nil, fmt.Errorf("failed to parse dep file: %w", err)
	}
	return deps, nil
}

func compileArgPartsFor(hostOS string, opts *CompileOptions, objPath, src string, flags []string, depPath, dir string) compileArgParts {
	commandOpts := *opts
	commandOpts.Includes = commandPathsFor(hostOS, dir, opts.Includes)
	return buildCompileArgParts(&commandOpts, commandPathFor(hostOS, dir, objPath), commandPathFor(hostOS, dir, src), commandFlagsFor(hostOS, dir, flags), commandPathFor(hostOS, dir, depPath))
}

func compileCommandForHost(hostOS, program string, parts compileArgParts, workDir, rspDir string) commandSpec {
	spec := commandSpec{Program: program, Args: parts.args()}
	dir := responseFileDir(hostOS, rspDir)
	if dir == "" {
		return spec
	}
	if len(parts.options) > 0 {
		file := responseFileFor(dir, parts.options)
		spec.Args = append(append(append([]string{}, parts.prefix...), responseFileArg(hostOS, workDir, file)), parts.suffix...)
		spec.response = file
	}
	if commandLineUnits(program, spec.Args) > safeInlineCommandUnits {
		file := responseFileFor(dir, parts.args())
		spec.Args = []string{responseFileArg(hostOS, workDir, file)}
		spec.response = file
	}
	return spec
}
