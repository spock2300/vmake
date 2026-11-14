package build

import (
	"errors"
	"fmt"
	"path/filepath"
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
			ppOpts.Includes = commandPaths(workDir, opts.Includes)
			args := []string{"-E", "-x", "assembler-with-cpp", "-MMD", "-MP", "-MF", commandPath(workDir, ppDep), "-MT", commandPath(workDir, objPath), "-o", commandPath(workDir, intermediate)}
			args = appendCompileOptions(args, &ppOpts, commandFlags(workDir, flags))
			args = append(args, commandPath(workDir, src))
			plan.Commands = append(plan.Commands, commandSpec{Program: c.ccPath, Args: args})
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
		plan.Commands = append(plan.Commands, commandSpec{Program: c.ccPath, Args: compileArgs(&asmOpts, objPath, asmSource, flags, asmDepPath, workDir)})
		plan.ValidateObject = true

		plan.Display = commandSpec{Program: c.ccPath, Args: compileArgs(&commandOpts, objPath, src, flags, "", workDir)}
		return plan, nil
	}

	plan.Commands = []commandSpec{{Program: compiler, Args: compileArgs(&commandOpts, objPath, src, flags, plan.DepPath, workDir)}}
	plan.Display = commandSpec{Program: compiler, Args: compileArgs(&commandOpts, objPath, src, flags, "", workDir)}

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
