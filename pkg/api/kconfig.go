package api

type KConfigEntry struct {
	name           string
	description    string
	configPath     string
	srcDir         string
	menuconfigCmd  string
	presets        []string
	defaultPreset  string
	selectedPreset string
}

func (k *KConfigEntry) Name() string           { return k.name }
func (k *KConfigEntry) Description() string    { return k.description }
func (k *KConfigEntry) ConfigPath() string     { return k.configPath }
func (k *KConfigEntry) SrcDir() string         { return k.srcDir }
func (k *KConfigEntry) Presets() []string      { return k.presets }
func (k *KConfigEntry) DefaultPreset() string  { return k.defaultPreset }
func (k *KConfigEntry) SelectedPreset() string { return k.selectedPreset }
func (k *KConfigEntry) MenuconfigCmd() string  { return k.menuconfigCmd }

func (k *KConfigEntry) SetDescription(desc string) *KConfigEntry {
	k.description = desc
	return k
}

func (k *KConfigEntry) SetConfigPath(path string) *KConfigEntry {
	k.configPath = path
	return k
}

func (k *KConfigEntry) SetSrcDir(dir string) *KConfigEntry {
	k.srcDir = dir
	return k
}

func (k *KConfigEntry) SetMenuconfigCmd(cmd string) *KConfigEntry {
	k.menuconfigCmd = cmd
	return k
}

func (k *KConfigEntry) AddPreset(name string) *KConfigEntry {
	k.presets = append(k.presets, name)
	return k
}

func (k *KConfigEntry) SetDefault(presetName string) *KConfigEntry {
	k.defaultPreset = presetName
	return k
}

func (k *KConfigEntry) SetSelectedPreset(name string) *KConfigEntry {
	k.selectedPreset = name
	return k
}
