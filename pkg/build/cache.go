package build

import (
	"encoding/json"
	"fmt"
	"os"

	"gitee.com/spock2300/vmake/pkg/toolchain"
)

const CacheVersion = 2

type BuildCache struct {
	Version   int                `json:"version"`
	Toolchain ToolchainMeta      `json:"toolchain"`
	Sources   map[string]*Source `json:"sources"`
}

type ToolchainMeta struct {
	Name    string `json:"name"`
	CCPath  string `json:"cc_path"`
	CXXPath string `json:"cxx_path"`
	Host    string `json:"host"`
}

type Source struct {
	ModTime int64    `json:"mod_time"`
	ObjPath string   `json:"obj_path"`
	Deps    []string `json:"deps"`
}

func NewBuildCache(tc *toolchain.Toolchain) *BuildCache {
	ccPath, _ := toolchain.ResolveToolPath(tc.Tools.CC, tc.InstallPath)
	cxxPath, _ := toolchain.ResolveToolPath(tc.Tools.CXX, tc.InstallPath)
	host := tc.Host
	if host == "" {
		host = toolchain.GetToolchainHost(tc)
	}

	return &BuildCache{
		Version: CacheVersion,
		Toolchain: ToolchainMeta{
			Name:    tc.Name,
			CCPath:  ccPath,
			CXXPath: cxxPath,
			Host:    host,
		},
		Sources: make(map[string]*Source),
	}
}

func LoadCache() (*BuildCache, error) {
	data, err := os.ReadFile("build/cache.json")
	if err != nil {
		return nil, err
	}

	var cache BuildCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("failed to parse cache: %w", err)
	}
	return &cache, nil
}

func (c *BuildCache) Save() error {
	if err := os.MkdirAll("build", 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile("build/cache.json", data, 0644)
}

func (c *BuildCache) NeedFullRebuild(tc *toolchain.Toolchain) bool {
	ccPath, _ := toolchain.ResolveToolPath(tc.Tools.CC, tc.InstallPath)
	cxxPath, _ := toolchain.ResolveToolPath(tc.Tools.CXX, tc.InstallPath)

	return c.Toolchain.Name != tc.Name ||
		c.Toolchain.CCPath != ccPath ||
		c.Toolchain.CXXPath != cxxPath
}

func (c *BuildCache) NeedRebuild(sourcePath string) bool {
	src, ok := c.Sources[sourcePath]
	if !ok {
		return true
	}

	if _, err := os.Stat(src.ObjPath); os.IsNotExist(err) {
		return true
	}

	info, err := os.Stat(sourcePath)
	if err != nil {
		return true
	}
	srcModTime := info.ModTime().Unix()

	if srcModTime > src.ModTime {
		return true
	}

	for _, dep := range src.Deps {
		depInfo, err := os.Stat(dep)
		if err != nil {
			return true
		}
		if depInfo.ModTime().Unix() > src.ModTime {
			return true
		}
	}

	return false
}

func (c *BuildCache) Update(sourcePath, objPath string, deps []string) {
	info, _ := os.Stat(sourcePath)
	var modTime int64
	if info != nil {
		modTime = info.ModTime().Unix()
	}

	c.Sources[sourcePath] = &Source{
		ModTime: modTime,
		ObjPath: objPath,
		Deps:    deps,
	}
}

func CleanObjects() error {
	return os.RemoveAll("build/objects")
}
