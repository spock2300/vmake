package main

import (
	"encoding/json"
	"io"
)

const jsonOutputSchemaVersion = 1

var jsonOutput bool

type buildJSONEnvelope struct {
	SchemaVersion int               `json:"schemaVersion"`
	Command       string            `json:"command"`
	Status        string            `json:"status"`
	Data          *buildReport      `json:"data"`
	Diagnostics   []buildDiagnostic `json:"diagnostics"`
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
	return &buildJSONEnvelope{
		SchemaVersion: jsonOutputSchemaVersion,
		Command:       report.Command,
		Status:        status,
		Data:          report,
		Diagnostics:   diagnostics,
	}
}

func writeBuildJSONEnvelope(writer io.Writer, report *buildReport) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(newBuildJSONEnvelope(report))
}
