package main

import (
	"encoding/json"
	"io"
	"path/filepath"
)

const jsonOutputSchemaVersion = 1

var jsonOutput bool

type buildJSONEnvelope struct {
	SchemaVersion   int               `json:"schemaVersion"`
	Command         string            `json:"command"`
	Status          string            `json:"status"`
	ReportPath      string            `json:"reportPath"`
	InstallManifest string            `json:"installManifest"`
	Invocation      string            `json:"invocation"`
	Data            *buildReport      `json:"data"`
	Diagnostics     []buildDiagnostic `json:"diagnostics"`
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
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(newBuildJSONEnvelope(report))
}
