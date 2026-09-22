package build

import (
	"debug/elf"
	"debug/pe"
	"errors"
	"fmt"
	"os"
)

func (c *Compiler) compileClangAssembly(src, objPath string, opts *CompileOptions, flags []string, workDir string) (deps []string, err error) {
	depPath := objPath + ".d"
	asmDep := depPath + ".as"
	ppDep := depPath + ".pp"
	intermediate := ""
	defer func() {
		err = errors.Join(err, removeCompileFiles(workDir, asmDep, ppDep))
		if err != nil {
			err = errors.Join(err, removeCompileFiles(workDir, objPath, depPath, intermediate))
		}
	}()

	asmSource := src
	depFiles := []string{asmDep}
	if opts.Language == "asm-cpp" {
		intermediate = objPath + ".s"
		ppOpts := *opts
		ppOpts.Includes = commandPaths(workDir, opts.Includes)
		args := []string{"-E", "-x", "assembler-with-cpp", "-MMD", "-MP", "-MF", commandPath(workDir, ppDep), "-MT", commandPath(workDir, objPath), "-o", commandPath(workDir, intermediate)}
		args = appendCompileOptions(args, &ppOpts, commandFlags(workDir, flags))
		args = append(args, commandPath(workDir, src))
		if _, err := c.run(c.ccPath, workDir, args...); err != nil {
			return nil, err
		}
		asmSource = intermediate
		depFiles = append([]string{ppDep}, depFiles...)
	}

	asmOpts := *opts
	asmOpts.Language = "asm"
	args := compileArgs(&asmOpts, objPath, asmSource, flags, asmDep, workDir)
	if _, err := c.run(c.ccPath, workDir, args...); err != nil {
		return nil, err
	}
	if err := validateAssemblyObject(resolveWorkPath(workDir, objPath), c.targetOS); err != nil {
		return nil, fmt.Errorf("Clang external assembler output: %w; check the toolchain --target and -B flags", err)
	}
	return mergeAssemblyDeps(src, objPath, depPath, workDir, intermediate, depFiles...)
}

func validateAssemblyObject(path, targetOS string) error {
	switch targetOS {
	case "windows":
		file, err := pe.Open(path)
		if err != nil {
			return fmt.Errorf("expected COFF object for target_os %s: %w", targetOS, err)
		}
		defer file.Close()
		if file.OptionalHeader != nil || file.Characteristics&pe.IMAGE_FILE_EXECUTABLE_IMAGE != 0 {
			return fmt.Errorf("expected COFF object for target_os %s, got executable image", targetOS)
		}
	case "linux", "none":
		file, err := elf.Open(path)
		if err != nil {
			return fmt.Errorf("expected ELF object for target_os %s: %w", targetOS, err)
		}
		defer file.Close()
		if file.Type != elf.ET_REL {
			return fmt.Errorf("expected ELF relocatable object for target_os %s, got %s", targetOS, file.Type)
		}
	default:
		return fmt.Errorf("Clang external assembler object validation is unsupported for target_os %q", targetOS)
	}
	return nil
}

func removeCompileFiles(workDir string, paths ...string) error {
	var errs []error
	for _, path := range paths {
		if path == "" {
			continue
		}
		if err := os.Remove(resolveWorkPath(workDir, path)); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("remove assembly output %s: %w", path, err))
		}
	}
	return errors.Join(errs...)
}
