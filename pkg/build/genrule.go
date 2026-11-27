package build

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/pkg/api"
	vlog "github.com/spock2300/vmake/pkg/log"
)

var hexChars = [16]byte{'0', '1', '2', '3', '4', '5', '6', '7', '8', '9', 'a', 'b', 'c', 'd', 'e', 'f'}

func runGenRules(rules []api.GenRule, generatedDir, workDir string) error {
	return runGenRulesContext(context.Background(), rules, generatedDir, workDir)
}

func runGenRulesContext(ctx context.Context, rules []api.GenRule, generatedDir, workDir string) error {
	if len(rules) == 0 {
		return nil
	}

	if err := os.MkdirAll(generatedDir, 0755); err != nil {
		return fmt.Errorf("create generated dir: %w", err)
	}

	stems := make(map[string]string, len(rules))
	for _, rule := range rules {
		if err := ctx.Err(); err != nil {
			return err
		}
		if rule.Kind() != api.GenRuleBinHeader {
			return fmt.Errorf("unsupported gen rule kind: %s", rule.Kind())
		}

		if prev, ok := stems[rule.OutputStem()]; ok {
			return fmt.Errorf("duplicate gen rule output stem %q from %q and %q", rule.OutputStem(), prev, rule.Input())
		}
		stems[rule.OutputStem()] = rule.Input()

		input := resolveWorkPath(workDir, rule.Input())
		data, err := os.ReadFile(input)
		if err != nil {
			return fmt.Errorf("read gen rule input %s: %w", rule.Input(), err)
		}
		identity := sha256.New()
		fmt.Fprint(identity, rule.Input(), "\x00")
		identity.Write(data)
		marker := "/* vmake gen: " + hex.EncodeToString(identity.Sum(nil)) + " */"
		output := filepath.Join(generatedDir, rule.OutputStem()+".h")
		if genRuleMatchesInput(output, marker) {
			vlog.Info("  GEN [skip] %s -> %s", rule.Input(), rule.OutputStem()+".h")
			continue
		}

		vlog.Info("  GEN %s -> %s", rule.Input(), rule.OutputStem()+".h")
		if err := generateBinHeaderContext(ctx, data, marker, output); err != nil {
			return err
		}
	}

	return nil
}

func genRuleMatchesInput(output, marker string) bool {
	file, err := os.Open(output)
	if err != nil {
		return false
	}
	defer file.Close()
	line, err := bufio.NewReader(file).ReadString('\n')
	if err != nil && line == "" {
		return false
	}
	return strings.TrimRight(line, "\r\n") == marker
}

func generateBinHeaderContext(ctx context.Context, data []byte, marker, output string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(filepath.Dir(output), ".vmake-gen-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	staged := filepath.Join(dir, filepath.Base(output))
	mode := os.FileMode(0666)
	readOnlyTarget := false
	if current, err := os.Stat(output); err == nil {
		mode = current.Mode().Perm()
		readOnlyTarget = mode&0200 == 0
	} else if !os.IsNotExist(err) {
		return err
	}
	if runtime.GOOS == "windows" {
		mode |= 0200
	}
	f, err := os.OpenFile(staged, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	writeErr := writeBinHeader(ctx, f, data, marker)
	if err := errors.Join(writeErr, f.Close()); err != nil {
		return fmt.Errorf("write %s: %w", output, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if runtime.GOOS == "windows" && readOnlyTarget {
		_ = os.Chmod(output, mode)
	}
	return fs.RenameRetry(staged, output)
}

func writeBinHeader(ctx context.Context, dest io.Writer, data []byte, marker string) error {
	w := bufio.NewWriterSize(dest, 32*1024)
	if _, err := fmt.Fprintf(w, "%s\n", marker); err != nil {
		return err
	}

	var line [82]byte
	n := len(data)

	for i := 0; i < n; i += 16 {
		if err := ctx.Err(); err != nil {
			return err
		}
		chunk := data[i:]
		if len(chunk) > 16 {
			chunk = chunk[:16]
		}
		pos := 0
		for j, b := range chunk {
			if j > 0 {
				line[pos] = ','
				pos++
			}
			line[pos] = '0'
			line[pos+1] = 'x'
			line[pos+2] = hexChars[b>>4]
			line[pos+3] = hexChars[b&0x0f]
			pos += 4
		}
		line[pos] = ','
		line[pos+1] = '\n'
		if _, err := w.Write(line[:pos+2]); err != nil {
			return err
		}
	}

	return w.Flush()
}
