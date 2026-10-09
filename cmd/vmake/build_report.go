package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spock2300/vmake/internal/jsonio"
	"github.com/spock2300/vmake/pkg/build"
	"github.com/spock2300/vmake/pkg/config"
)

type buildArtifact struct {
	Package     string `json:"package"`
	Target      string `json:"target"`
	BuildPath   string `json:"buildPath"`
	InstallPath string `json:"installPath"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
}

type buildReport struct {
	completed       bool
	SchemaVersion   int                       `json:"schemaVersion"`
	BuildID         string                    `json:"buildId"`
	Root            string                    `json:"root"`
	Status          string                    `json:"status"`
	Started         string                    `json:"started"`
	Finished        string                    `json:"finished"`
	Error           string                    `json:"error"`
	ConfigFile      string                    `json:"configFile"`
	ConfigSHA256    string                    `json:"configSha256"`
	Global          map[string]any            `json:"global"`
	Packages        map[string]map[string]any `json:"packages"`
	Toolchain       string                    `json:"toolchain"`
	Mode            string                    `json:"mode"`
	CompileDatabase string                    `json:"compileDatabase"`
	Targets         []build.TargetResult      `json:"targets"`
	Artifacts       []buildArtifact           `json:"artifacts"`
}

func reportPath(root string) string { return filepath.Join(root, "build", "vmake-build.json") }

func beginBuildReport() (*buildReport, error) {
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	r := &buildReport{SchemaVersion: 1, BuildID: hex.EncodeToString(id), Root: findProjectDir(), Status: "running", Started: time.Now().UTC().Format(time.RFC3339Nano), Targets: []build.TargetResult{}, Artifacts: []buildArtifact{}, Global: map[string]any{}, Packages: map[string]map[string]any{}}
	return r, jsonio.Save(reportPath(r.Root), r)
}

func (r *buildReport) finish(result *error) {
	interrupted := recover()
	if interrupted != nil {
		*result = fmt.Errorf("build panicked: %v", interrupted)
	}
	if *result == nil && !r.completed {
		*result = fmt.Errorf("build interrupted before results were collected")
	}
	r.Finished = time.Now().UTC().Format(time.RFC3339Nano)
	r.Status = "succeeded"
	if *result != nil {
		r.Status = "failed"
		if errors.Is(*result, context.Canceled) {
			r.Status = "cancelled"
		}
		r.Error = (*result).Error()
		r.Artifacts = []buildArtifact{}
	}
	*result = errors.Join(*result, jsonio.Save(reportPath(r.Root), r))
	if interrupted != nil {
		panic(interrupted)
	}
}

func fileDigest(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), n, err
}

func (r *buildReport) bind(ctx *RuntimeContext) error {
	r.ConfigFile, r.ConfigSHA256 = filepath.Base(ctx.ConfigPath), ctx.ConfigDigest
	return jsonio.Save(reportPath(r.Root), r)
}

func (r *buildReport) complete(ctx *RuntimeContext, result *BuildResult) error {
	r.ConfigFile, r.ConfigSHA256 = filepath.Base(ctx.ConfigPath), ctx.ConfigDigest
	_, path, digest, err := config.LoadProjectSnapshot(r.Root)
	if err != nil {
		return err
	}
	if path != ctx.ConfigPath || digest != ctx.ConfigDigest {
		return fmt.Errorf("configuration changed during build")
	}
	r.Global, r.Targets = result.GlobalValues, result.Targets
	r.Toolchain, r.Mode = result.TcName, result.Mode
	r.CompileDatabase = filepath.Join(r.Root, "build", "compile_commands.json")
	if _, err := os.Stat(r.CompileDatabase); os.IsNotExist(err) {
		r.CompileDatabase = ""
	} else if err != nil {
		return err
	}
	for name, buildContext := range result.BuildCtxs {
		r.Packages[name] = maps.Clone(buildContext.CfgVals)
	}

	seen := map[string]bool{}
	for _, file := range result.InstallFiles {
		source, err := filepath.Abs(file.Source)
		if err != nil {
			return err
		}
		installed, err := filepath.Abs(file.Destination)
		if err != nil {
			return err
		}
		key := source + "\x00" + installed
		if seen[key] {
			continue
		}
		hash, size, err := fileDigest(source)
		if err != nil {
			return err
		}
		installedHash, _, err := fileDigest(installed)
		if err != nil {
			return err
		}
		if installedHash != hash {
			return fmt.Errorf("installed artifact differs: %s", installed)
		}
		seen[key] = true
		r.Artifacts = append(r.Artifacts, buildArtifact{file.Package, file.Target, source, installed, size, hash})
	}
	slices.SortFunc(r.Artifacts, func(a, b buildArtifact) int {
		if c := strings.Compare(a.BuildPath, b.BuildPath); c != 0 {
			return c
		}
		return strings.Compare(a.InstallPath, b.InstallPath)
	})
	r.completed = true
	return nil
}

func invalidateBuildReport() error {
	err := os.Remove(reportPath(findProjectDir()))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
