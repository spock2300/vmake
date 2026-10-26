package api

const ChipOptionName = "chip"

type ChipProfile struct {
	Name               string
	PreferredToolchain string
	TargetOS           string
	TargetTriple       string
}
