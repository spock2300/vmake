package build

import (
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"errors"
	"fmt"
	"os"
)

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
	case "darwin":
		file, err := macho.Open(path)
		if err != nil {
			return fmt.Errorf("expected Mach-O object for target_os %s: %w", targetOS, err)
		}
		defer file.Close()
		if file.Type != macho.TypeObj {
			return fmt.Errorf("expected Mach-O relocatable object for target_os %s, got %s", targetOS, file.Type)
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
