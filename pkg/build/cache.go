package build

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"gitee.com/spock2300/vmake/pkg/toolchain"
)

const CacheVersion = 3

type BuildCache struct {
	Version   int                `json:"version"`
	Toolchain ToolchainMeta      `json:"toolchain"`
	Mode      string             `json:"mode,omitempty"`
	Sources   map[string]*Source `json:"sources"`
	mu        sync.RWMutex       `json:"-"`
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

func LoadCache(tcName string) (*BuildCache, error) {
	data, err := os.ReadFile(fmt.Sprintf("build/%s/cache.json", tcName))
	if err != nil {
		return nil, err
	}

	var cache BuildCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("failed to parse cache: %w", err)
	}
	return &cache, nil
}

func (c *BuildCache) Save(tcName string) error {
	dir := fmt.Sprintf("build/%s", tcName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(fmt.Sprintf("%s/cache.json", dir), data, 0644)
}

func (c *BuildCache) NeedFullRebuild(tc *toolchain.Toolchain) bool {
	ccPath, _ := toolchain.ResolveToolPath(tc.Tools.CC, tc.InstallPath)
	cxxPath, _ := toolchain.ResolveToolPath(tc.Tools.CXX, tc.InstallPath)

	return c.Toolchain.Name != tc.Name ||
		c.Toolchain.CCPath != ccPath ||
		c.Toolchain.CXXPath != cxxPath
}

func (c *BuildCache) NeedRebuild(sourcePath string) bool {
	return c.GetIfValid(sourcePath) == nil
}

func (c *BuildCache) GetIfValid(sourcePath string) *Source {
	c.mu.RLock()
	defer c.mu.RUnlock()

	src, ok := c.Sources[sourcePath]
	if !ok {
		return nil
	}

	if _, err := os.Stat(src.ObjPath); os.IsNotExist(err) {
		return nil
	}

	info, err := os.Stat(sourcePath)
	if err != nil {
		return nil
	}

	if info.ModTime().Unix() > src.ModTime {
		return nil
	}

	for _, dep := range src.Deps {
		depInfo, err := os.Stat(dep)
		if err != nil || depInfo.ModTime().Unix() > src.ModTime {
			return nil
		}
	}

	return &Source{
		ModTime: src.ModTime,
		ObjPath: src.ObjPath,
		Deps:    src.Deps,
	}
}

func (c *BuildCache) Update(sourcePath, objPath string, deps []string) {
	maxModTime := int64(0)

	if info, err := os.Stat(sourcePath); err == nil {
		maxModTime = info.ModTime().Unix()
	}

	for _, dep := range deps {
		if info, err := os.Stat(dep); err == nil {
			if t := info.ModTime().Unix(); t > maxModTime {
				maxModTime = t
			}
		}
	}

	c.mu.Lock()
	c.Sources[sourcePath] = &Source{
		ModTime: maxModTime,
		ObjPath: objPath,
		Deps:    deps,
	}
	c.mu.Unlock()
}

func CleanObjects(tcName string) error {
	return os.RemoveAll(fmt.Sprintf("build/%s/objects", tcName))
}
