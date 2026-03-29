package api

type GenRuleKind string

const (
	GenRuleBinHeader GenRuleKind = "binheader"
)

type GenRule struct {
	kind       GenRuleKind
	input      string
	outputStem string
}

func (r *GenRule) Kind() GenRuleKind  { return r.kind }
func (r *GenRule) Input() string      { return r.input }
func (r *GenRule) OutputStem() string { return r.outputStem }
