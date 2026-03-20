package plugin

type SourceOrigin int

const (
	SourceLocal SourceOrigin = iota
	SourceRemote
)

type Source struct {
	Path      string
	Name      string
	Dir       string
	OutputDir string
	Origin    SourceOrigin
	Force     bool
}
