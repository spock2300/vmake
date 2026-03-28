package build

func BuildCompileArgs(opts *CompileOptions, objPath, src string, flags []string, depPath string) []string {
	args := []string{"-c"}

	if depPath != "" {
		args = append(args, "-MMD", "-MP", "-MF", depPath)
	}

	args = append(args, "-o", objPath)

	for _, inc := range opts.Includes {
		args = append(args, "-I"+inc)
	}

	for _, def := range opts.Defines {
		args = append(args, "-D"+def)
	}

	args = append(args, flags...)
	args = append(args, src)

	return args
}
