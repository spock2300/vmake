package main

import (
	"fmt"
	"runtime"

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/gitusr"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func checkPlatform(tc *toolchain.Toolchain) []doctorFinding {
	var findings []doctorFinding

	if fs.SymlinksSupported() {
		findings = append(findings, doctorFinding{
			Severity: "ok",
			Category: "symlink",
			Message:  "symbolic links can be created",
		})
	} else {
		findings = append(findings, doctorFinding{
			Severity: "error",
			Category: "symlink",
			Message:  "cannot create symbolic links; " + fs.SymlinkHint,
		})
	}

	if runtime.GOOS == "windows" {
		if dir, ok := gitusr.UsrBin(); ok {
			findings = append(findings, doctorFinding{
				Severity: "ok",
				Category: "gitusr",
				Message:  "Git for Windows userland found at " + dir,
			})
		} else {
			findings = append(findings, doctorFinding{
				Severity: "warn",
				Category: "gitusr",
				Message:  "Git for Windows userland not found; sh/tar/unzip/curl may be unresolvable (install the full Git for Windows, not MinGit)",
			})
		}
	}

	if tc == nil {
		return findings
	}
	findings = append(findings, checkToolchain(tc)...)
	return findings
}

func checkToolchain(tc *toolchain.Toolchain) []doctorFinding {
	var findings []doctorFinding
	makeInstallPath := ""
	if tc.Tools.MAKE != "" {
		makeInstallPath = tc.InstallPath
	}
	if path, err := toolchain.ResolveToolPath(tc.MakeTool(), makeInstallPath); err == nil {
		findings = append(findings, doctorFinding{
			Severity: "ok",
			Category: "make",
			Message:  "make found at " + path,
		})
	} else {
		findings = append(findings, doctorFinding{
			Severity: "warn",
			Category: "make",
			Message:  fmt.Sprintf("make %q: %v; p.Make(), preset generation and default make menuconfig require this tool%s", tc.MakeTool(), err, toolHint()),
		})
	}

	if errs := toolchain.ValidateToolchain(tc); len(errs) == 0 {
		findings = append(findings, doctorFinding{
			Severity: "ok",
			Category: "binutils",
			Message:  fmt.Sprintf("selected toolchain %q: all configured tools found", tc.Name),
		})
	} else {
		for _, err := range errs {
			findings = append(findings, doctorFinding{
				Severity: "error",
				Category: "binutils",
				Message:  fmt.Sprintf("selected toolchain %q: %v", tc.Name, err),
			})
		}
	}

	return findings
}

func toolHint() string {
	if runtime.GOOS == "windows" {
		return " (Git for Windows bundles neither a C toolchain nor make; install MinGW-w64 or MSYS2)"
	}
	return ""
}
