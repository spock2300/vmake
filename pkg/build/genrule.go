package build

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/pkg/api"
	vlog "github.com/spock2300/vmake/pkg/log"
)

var hexChars = [16]byte{'0', '1', '2', '3', '4', '5', '6', '7', '8', '9', 'a', 'b', 'c', 'd', 'e', 'f'}

func runGenRules(rules []api.GenRule, generatedDir string) error {
	if len(rules) == 0 {
		return nil
	}

	if err := os.MkdirAll(generatedDir, 0755); err != nil {
		return fmt.Errorf("create generated dir: %w", err)
	}

	stems := make(map[string]string, len(rules))
	for _, rule := range rules {
		if rule.Kind() != api.GenRuleBinHeader {
			return fmt.Errorf("unsupported gen rule kind: %s", rule.Kind())
		}

		if prev, ok := stems[rule.OutputStem()]; ok {
			return fmt.Errorf("duplicate gen rule output stem %q from %q and %q", rule.OutputStem(), prev, rule.Input())
		}
		stems[rule.OutputStem()] = rule.Input()

		if !fs.FileExists(rule.Input()) {
			return fmt.Errorf("gen rule input not found: %s", rule.Input())
		}

		output := filepath.Join(generatedDir, rule.OutputStem()+".h")

		if !needGenRule(rule.Input(), output) {
			vlog.Info("  GEN [skip] %s -> %s", rule.Input(), rule.OutputStem()+".h")
			continue
		}

		vlog.Info("  GEN %s -> %s", rule.Input(), rule.OutputStem()+".h")
		if err := generateBinHeader(rule.Input(), output); err != nil {
			return err
		}
	}

	return nil
}

func needGenRule(input, output string) bool {
	outInfo, err := os.Stat(output)
	if err != nil {
		return true
	}

	inInfo, err := os.Stat(input)
	if err != nil {
		return true
	}

	return inInfo.ModTime().After(outInfo.ModTime())
}

func generateBinHeader(input, output string) error {
	data, err := os.ReadFile(input)
	if err != nil {
		return fmt.Errorf("read %s: %w", input, err)
	}

	f, err := os.Create(output)
	if err != nil {
		return fmt.Errorf("create %s: %w", output, err)
	}
	defer f.Close()

	w := bufio.NewWriterSize(f, 32*1024)
	defer w.Flush()

	var line [82]byte
	n := len(data)

	for i := 0; i < n; i += 16 {
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
			return fmt.Errorf("write %s: %w", output, err)
		}
	}

	return nil
}
