package main

import (
	"os/exec"
	"runtime"
	"strings"

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/gitusr"
)

// checkPlatform reports the status of the platform prerequisites vmake
// depends on: symbolic links, the Git for Windows userland, make, and the
// C toolchain.
func checkPlatform() []doctorFinding {
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

	if path, err := exec.LookPath("make"); err == nil {
		findings = append(findings, doctorFinding{
			Severity: "ok",
			Category: "make",
			Message:  "make found at " + path,
		})
	} else {
		findings = append(findings, doctorFinding{
			Severity: "warn",
			Category: "make",
			Message:  "make not found on PATH; p.Make(), EnsureConfig and 'vmake config' will fail" + toolHint(),
		})
	}

	if missing := missingTools("gcc", "g++", "ar", "ranlib", "strip", "nm", "objcopy"); len(missing) == 0 {
		findings = append(findings, doctorFinding{
			Severity: "ok",
			Category: "binutils",
			Message:  "gcc/g++/ar/ranlib/strip/nm/objcopy all found on PATH",
		})
	} else {
		findings = append(findings, doctorFinding{
			Severity: "warn",
			Category: "binutils",
			Message:  "missing build tools on PATH: " + strings.Join(missing, ", ") + toolHint(),
		})
	}

	return findings
}

func toolHint() string {
	if runtime.GOOS == "windows" {
		return " (Git for Windows bundles neither a C toolchain nor make; install MinGW-w64 or MSYS2)"
	}
	return ""
}

func missingTools(names ...string) []string {
	var missing []string
	for _, name := range names {
		if _, err := exec.LookPath(name); err != nil {
			missing = append(missing, name)
		}
	}
	return missing
}
