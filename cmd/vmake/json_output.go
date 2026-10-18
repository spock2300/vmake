package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spock2300/vmake/internal/fs"
	vlog "github.com/spock2300/vmake/pkg/log"
)

const jsonOutputSchemaVersion = 1

var (
	jsonOutput          bool
	outputFlag          string
	jsonEnvelopeWritten bool
	preReportJSON       *preReportRequest
	processStdout       io.Writer = os.Stdout
)

type buildJSONEnvelope struct {
	SchemaVersion   int               `json:"schemaVersion"`
	Command         string            `json:"command"`
	Status          string            `json:"status"`
	ReportPath      string            `json:"reportPath,omitempty"`
	InstallManifest string            `json:"installManifest,omitempty"`
	Invocation      string            `json:"invocation,omitempty"`
	Data            *buildReport      `json:"data"`
	Diagnostics     []buildDiagnostic `json:"diagnostics"`
}

type preReportRequest struct {
	command   string
	output    string
	requested bool
}

var preReportValueFlags = map[string]bool{
	"--toolchain":    true,
	"--mode":         true,
	"--manifest":     true,
	"--jobs":         true,
	"-j":             true,
	"--prefix":       true,
	"-p":             true,
	"--install-type": true,
	"--project":      true,
	"-C":             true,
}

func detectPreReportJSON(args []string) *preReportRequest {
	request := &preReportRequest{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if request.command == "" && i+1 < len(args) {
				request.command = args[i+1]
			}
			break
		}
		switch {
		case arg == "--json":
			request.requested = true
			continue
		case arg == "--output":
			request.requested = true
			if i+1 < len(args) {
				request.output = args[i+1]
				i++
			}
			continue
		case strings.HasPrefix(arg, "--output="):
			request.requested = true
			request.output = strings.TrimPrefix(arg, "--output=")
			continue
		case preReportValueFlags[arg]:
			i++
			continue
		case strings.HasPrefix(arg, "-") && arg != "-":
			continue
		}
		if request.command == "" {
			request.command = arg
		}
	}
	if !request.requested {
		return nil
	}
	switch request.command {
	case reportCommandBuild, reportCommandRebuild, reportCommandTest:
		return request
	}
	return nil
}

func emitPreReportJSON(err error) {
	request := preReportJSON
	if request == nil || jsonEnvelopeWritten {
		return
	}
	jsonEnvelopeWritten = true
	code := "VMAKE_E_BUILD"
	var usage *usageError
	if errors.As(err, &usage) {
		code = "VMAKE_E_USAGE"
	}
	document := &buildJSONEnvelope{
		SchemaVersion: jsonOutputSchemaVersion,
		Command:       request.command,
		Status:        "error",
		Diagnostics:   []buildDiagnostic{{Severity: "error", Code: code, Message: err.Error()}},
	}
	if writeErr := writeJSONDocument(processStdout, document, request.output); writeErr != nil {
		vlog.Error("Error: %v", writeErr)
	}
}

func newBuildJSONEnvelope(report *buildReport) *buildJSONEnvelope {
	status := "error"
	if report.Status == "succeeded" {
		status = "ok"
	}
	diagnostics := report.Diagnostics
	if diagnostics == nil {
		diagnostics = []buildDiagnostic{}
	}
	command := report.Command
	if command == "" {
		command = reportCommandBuild
	}
	return &buildJSONEnvelope{
		SchemaVersion:   jsonOutputSchemaVersion,
		Command:         command,
		Status:          status,
		ReportPath:      reportPath(report.Root),
		InstallManifest: installManifestPath(report.Root),
		Invocation:      command,
		Data:            report,
		Diagnostics:     diagnostics,
	}
}

func installManifestPath(root string) string {
	prefix := prefixFlag
	if prefix == "" {
		prefix = filepath.Join(root, "install")
	}
	if absolute, err := filepath.Abs(prefix); err == nil {
		prefix = absolute
	}
	return filepath.Join(prefix, "manifest.json")
}

func writeBuildJSONEnvelope(writer io.Writer, report *buildReport) error {
	jsonEnvelopeWritten = true
	return writeJSONDocument(writer, newBuildJSONEnvelope(report), outputFlag)
}

func writeJSONDocument(fallback io.Writer, document any, output string) error {
	if output == "" || output == "-" {
		return encodeJSONDocument(fallback, document)
	}
	path, err := filepath.Abs(output)
	if err != nil {
		return fmt.Errorf("resolve output path %s: %w", output, err)
	}
	dir := filepath.Dir(path)
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("output directory %s does not exist", dir)
		}
		return fmt.Errorf("write %s: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("output directory %s is not a directory", dir)
	}
	file, err := os.CreateTemp(dir, ".vmake-result-*.tmp")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	temp := file.Name()
	renamed := false
	defer func() {
		if !renamed {
			os.Remove(temp)
		}
	}()
	if err := encodeJSONDocument(file, document); err != nil {
		file.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Chmod(temp, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := fs.RenameRetry(temp, path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	renamed = true
	return nil
}

func encodeJSONDocument(writer io.Writer, document any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(document)
}
