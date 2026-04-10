package build

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	vlog "gitee.com/spock2300/vmake/pkg/log"
	"gitee.com/spock2300/vmake/pkg/repo"
)

type stampData struct {
	ConfigHash string `json:"config_hash"`
	SourceRev  string `json:"source_rev,omitempty"`
}

func gitHead(dir string) string {
	rev, err := repo.GetCurrentCommit(dir)
	if err != nil {
		return ""
	}
	return rev
}

func computeConfigHash(baseDir string, configFiles []string) (string, error) {
	if len(configFiles) == 0 {
		return "", nil
	}
	h := sha256.New()
	for _, cf := range configFiles {
		p := filepath.Join(baseDir, cf)
		f, err := os.Open(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", err
		}
		if _, err := io.Copy(h, f); err != nil {
			f.Close()
			return "", err
		}
		f.Close()
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func isStampUpToDate(stampPath, baseDir string, configFiles []string) bool {
	data, err := os.ReadFile(stampPath)
	if err != nil || len(data) == 0 {
		return false
	}
	var stamp stampData
	if err := json.Unmarshal(data, &stamp); err != nil {
		return false
	}

	configHash, err := computeConfigHash(baseDir, configFiles)
	if err != nil {
		return false
	}
	if stamp.ConfigHash != configHash {
		return false
	}

	if stamp.SourceRev != "" {
		rev := gitHead(baseDir)
		if rev != "" && rev != stamp.SourceRev {
			return false
		}
	}

	return true
}

func buildStampData(baseDir string, configFiles []string) stampData {
	configHash, err := computeConfigHash(baseDir, configFiles)
	if err != nil {
		configHash = ""
	}
	return stampData{
		ConfigHash: configHash,
		SourceRev:  gitHead(baseDir),
	}
}

func writeStamp(stampPath string, stamp stampData) {
	data, err := json.Marshal(stamp)
	if err != nil {
		vlog.Error("stamp marshal: %v", err)
		return
	}
	if err := os.WriteFile(stampPath, data, 0644); err != nil {
		vlog.Error("stamp write %s: %v", stampPath, err)
	}
}
