package snapshot

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

const (
	snapshotDir  = "test_data/_snapshot"
	baselineName  = "baseline"
	updateEnvKey = "VMAKE_SNAPSHOT_UPDATE"
)

var (
	skipProjects = map[string]bool{
		"07_subbuild_codegen": true,
		"08_with_package":     true,
		"09_with_curl":        true,
		"10_local_repo":       true,
	}
	updateSnapshots = flag.Bool("update", false, "regenerate baseline snapshots")
)

type fileEntry struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	Hash string `json:"hash"`
}

type manifestEntry struct {
	Name    string `json:"name"`
	Source  string `json:"source"`
	Path    string `json:"path,omitempty"`
	URL     string `json:"url,omitempty"`
	Version string `json:"version,omitempty"`
	Ref     string `json:"ref,omitempty"`
}

type redactedManifest struct {
	Toolchain string           `json:"toolchain"`
	Mode      string           `json:"mode"`
	Packages  []manifestEntry  `json:"packages"`
}

type snapshot struct {
	Project       string      `json:"project"`
	BuildArgs     []string    `json:"build_args"`
	Manifest      redactedManifest `json:"manifest"`
	InstallFiles  []fileEntry `json:"install_files"`
	BuildFiles    []fileEntry `json:"build_files"`
	InstallTreeHash string   `json:"install_tree_hash"`
}

type project struct {
	name string
	dir  string
}

func projectRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine test file path")
	}
	dir := filepath.Dir(file)
	for {
		hasGoMod := false
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			hasGoMod = true
		}
		hasCmd := false
		if _, err := os.Stat(filepath.Join(dir, "cmd", "vmake")); err == nil {
			hasCmd = true
		}
		if hasGoMod && hasCmd {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("vmake repo root (with go.mod and cmd/vmake) not found walking upward from " + file)
		}
		dir = parent
	}
}

func hasBuildGo(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "build.go")); err == nil {
		return true
	}
	found := false
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || found {
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			if skipDirsForDiscovery[name] || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Name() == "build.go" {
			found = true
		}
		return nil
	})
	return found
}

var skipDirsForDiscovery = map[string]bool{
	"build": true, "install": true, "vmake_deps": true, "node_modules": true, "vendor": true,
}

func discoverProjects(t *testing.T, root string) []project {
	t.Helper()
	var projects []project
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read %s: %v", root, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
			continue
		}
		if skipProjects[name] {
			continue
		}
		if skipDirsForDiscovery[name] {
			continue
		}
		dir := filepath.Join(root, name)
		if !hasBuildGo(dir) {
			continue
		}
		projects = append(projects, project{name: name, dir: dir})
	}
	return projects
}

func cleanProject(dir string) error {
	patterns := []string{"build", "install", "vmake_deps", ".vmake", ".cache"}
	for _, p := range patterns {
		if err := os.RemoveAll(filepath.Join(dir, p)); err != nil {
			return err
		}
	}
	return nil
}

func runVmake(t *testing.T, root, dir string, args ...string) error {
	t.Helper()
	bin := filepath.Join(root, "vmake")
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("vmake binary not found at %s; run 'go build -o vmake ./cmd/vmake'", bin)
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "VMAKE_DIR="+root)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("vmake output:\n%s", string(out))
		return fmt.Errorf("vmake %s in %s: %w", strings.Join(args, " "), dir, err)
	}
	return nil
}

func normalizeBytes(data []byte, root string) []byte {
	normalized := bytes.ReplaceAll(data, []byte(root), []byte("<ROOT>"))
	home, err := os.UserHomeDir()
	if err == nil && home != "" && home != root {
		normalized = bytes.ReplaceAll(normalized, []byte(home), []byte("<HOME>"))
	}
	return normalized
}

func hashFile(path, root string) (string, int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	normalized := normalizeBytes(data, root)
	h := sha256.Sum256(normalized)
	return hex.EncodeToString(h[:]), int64(len(normalized)), nil
}

func collectTree(root, base, subdir string, filter func(rel string) bool) ([]fileEntry, error) {
	var entries []fileEntry
	start := filepath.Join(base, subdir)
	err := filepath.Walk(start, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(start, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if filter != nil && !filter(rel) {
			return nil
		}
		h, size, err := hashFile(path, root)
		if err != nil {
			return err
		}
		entries = append(entries, fileEntry{Path: rel, Size: size, Hash: h})
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

func installFilter(rel string) bool {
	return rel != "manifest.json"
}

func buildFilter(rel string) bool {
	return rel == "compile_commands.json"
}

func redactManifest(path string) (redactedManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return redactedManifest{}, err
	}
	var raw struct {
		Vmake     string `json:"vmake"`
		Toolchain string `json:"toolchain"`
		Mode      string `json:"mode"`
		Generated string `json:"generated"`
		Packages  []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			Source  string `json:"source"`
			URL     string `json:"url"`
			Ref     string `json:"ref"`
			Path    string `json:"path"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return redactedManifest{}, err
	}
	r := redactedManifest{Toolchain: raw.Toolchain, Mode: raw.Mode}
	for _, p := range raw.Packages {
		e := manifestEntry{
			Name:   p.Name,
			Source: p.Source,
			Path:   p.Path,
			URL:    p.URL,
		}
		if p.Source != "local" {
			e.Version = p.Version
			e.Ref = p.Ref
		}
		r.Packages = append(r.Packages, e)
	}
	return r, nil
}

func treeHash(files []fileEntry) string {
	var b bytes.Buffer
	for _, f := range files {
		fmt.Fprintf(&b, "%s %s\n", f.Path, f.Hash)
	}
	h := sha256.Sum256(b.Bytes())
	return hex.EncodeToString(h[:])
}

func takeSnapshot(t *testing.T, root string, p project) snapshot {
	t.Helper()
	manifestPath := filepath.Join(p.dir, "install", "manifest.json")
	manifest, err := redactManifest(manifestPath)
	if err != nil {
		t.Fatalf("redact manifest: %v", err)
	}
	installFiles, err := collectTree(root, p.dir, "install", installFilter)
	if err != nil {
		t.Fatalf("collect install tree: %v", err)
	}
	buildFiles, err := collectTree(root, p.dir, "build", buildFilter)
	if err != nil {
		t.Fatalf("collect build tree: %v", err)
	}
	return snapshot{
		Project:         p.name,
		BuildArgs:       []string{"build", "--install"},
		Manifest:        manifest,
		InstallFiles:    installFiles,
		BuildFiles:      buildFiles,
		InstallTreeHash: treeHash(installFiles),
	}
}

func baselinePath(root string, p project) string {
	return filepath.Join(root, snapshotDir, baselineName, p.name+".json")
}

func loadBaseline(path string) (snapshot, error) {
	var s snapshot
	data, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, err
	}
	return s, nil
}

func saveBaseline(path string, s snapshot) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
}

func shouldUpdate() bool {
	return *updateSnapshots || os.Getenv(updateEnvKey) == "1"
}

func diffFiles(label string, want, got []fileEntry) string {
	wantMap := make(map[string]fileEntry, len(want))
	for _, f := range want {
		wantMap[f.Path] = f
	}
	gotMap := make(map[string]fileEntry, len(got))
	for _, f := range got {
		gotMap[f.Path] = f
	}
	all := make(map[string]struct{}, len(want)+len(got))
	for k := range wantMap {
		all[k] = struct{}{}
	}
	for k := range gotMap {
		all[k] = struct{}{}
	}
	var paths []string
	for k := range all {
		paths = append(paths, k)
	}
	sort.Strings(paths)
	var b strings.Builder
	for _, p := range paths {
		w, ok1 := wantMap[p]
		g, ok2 := gotMap[p]
		switch {
		case !ok2:
			fmt.Fprintf(&b, "  [%s] missing: %s\n", label, p)
		case !ok1:
			fmt.Fprintf(&b, "  [%s] extra:   %s\n", label, p)
		case w.Hash != g.Hash:
			fmt.Fprintf(&b, "  [%s] changed: %s\n    want %s\n    got  %s\n", label, p, w.Hash[:12], g.Hash[:12])
		}
	}
	return b.String()
}

func diffManifest(want, got redactedManifest) string {
	w, _ := json.MarshalIndent(want, "", "  ")
	g, _ := json.MarshalIndent(got, "", "  ")
	if bytes.Equal(w, g) {
		return ""
	}
	return fmt.Sprintf("  manifest:\n    want %s\n    got  %s\n", w, g)
}

func runSnapshot(t *testing.T, root string, p project) {
	t.Helper()
	if err := cleanProject(p.dir); err != nil {
		t.Fatalf("clean: %v", err)
	}
	if err := runVmake(t, root, p.dir, "build", "--install"); err != nil {
		t.Fatalf("build: %v", err)
	}
	got := takeSnapshot(t, root, p)

	path := baselinePath(root, p)
	if shouldUpdate() {
		if err := saveBaseline(path, got); err != nil {
			t.Fatalf("save baseline: %v", err)
		}
		t.Logf("baseline updated: %s", path)
		return
	}

	want, err := loadBaseline(path)
	if err != nil {
		if os.IsNotExist(err) {
			t.Fatalf("no baseline at %s; run with -update to create", path)
		}
		t.Fatalf("load baseline: %v", err)
	}

	var diff strings.Builder
	if d := diffManifest(want.Manifest, got.Manifest); d != "" {
		fmt.Fprintln(&diff, d)
	}
	if d := diffFiles("install", want.InstallFiles, got.InstallFiles); d != "" {
		fmt.Fprintln(&diff, d)
	}
	if d := diffFiles("build", want.BuildFiles, got.BuildFiles); d != "" {
		fmt.Fprintln(&diff, d)
	}

	if diff.Len() > 0 {
		t.Errorf("snapshot drift:\n%s", diff.String())
	}
}

func TestSnapshotsTestData(t *testing.T) {
	root := projectRoot(t)
	projects := discoverProjects(t, filepath.Join(root, "test_data"))
	if len(projects) == 0 {
		t.Fatal("no projects discovered in test_data/")
	}
	for _, p := range projects {
		p := p
		t.Run(p.name, func(t *testing.T) {
			runSnapshot(t, root, p)
		})
	}
}

func TestSnapshotsTestLinux(t *testing.T) {
	root := projectRoot(t)
	fw := filepath.Join(root, "test_linux", "17_firmware")
	if _, err := os.Stat(filepath.Join(fw, "build.go")); err != nil {
		t.Skip("test_linux/17_firmware not present")
	}
	p := project{name: "17_firmware", dir: fw}
	t.Run(p.name, func(t *testing.T) {
		runSnapshot(t, root, p)
	})
}
