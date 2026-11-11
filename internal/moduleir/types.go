// Package moduleir validates the bounded pure-module extraction profiles.
package moduleir

const (
	Schema          = "lean-go-ir/v2"
	Profile         = "pure-module/v1"
	SchemaV3        = "lean-go-ir/v3"
	ProfileV2       = "pure-module/v2"
	SchemaV4        = "lean-go-ir/v4"
	ProfileV3       = "pure-module/v3"
	Stage           = "pure-mono"
	LeanVersion     = "4.35.0"
	LeanCommit      = "eb6cafb7290e8bd5f2937522d8d804f58f02bafc"
	MaxBytes        = 4 << 20
	MaxNodes        = 10000
	MaxCodeDepth    = 256
	MaxTypes        = 256
	MaxFunctions    = 256
	MaxConstructors = 256
	MaxArity        = 64
	MaxNameBytes    = 512
)

type Param struct {
	ID   int    `json:"id"`
	Type string `json:"type"`
}
type Constructor struct {
	Name   string   `json:"name"`
	Tag    int      `json:"tag"`
	Fields []string `json:"fields"`
}
type TypeDecl struct {
	Name         string        `json:"name"`
	Constructors []Constructor `json:"constructors"`
}
type Claim struct {
	Name      string   `json:"name"`
	Statement string   `json:"statement"`
	Axioms    []string `json:"axioms"`
}
type Rewrite struct {
	Source  string   `json:"source"`
	Target  string   `json:"target"`
	Theorem string   `json:"theorem"`
	Axioms  []string `json:"axioms"`
}
type Value struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	Args []int  `json:"args"`
}
type Alt struct {
	Constructor string  `json:"constructor"`
	Params      []Param `json:"params"`
	Body        *Code   `json:"body"`
}
type Join struct {
	ID     int     `json:"id"`
	Params []Param `json:"params"`
	Body   *Code   `json:"body"`
}
type Code struct {
	Op     string `json:"op"`
	Bind   *Param `json:"bind,omitempty"`
	Value  *Value `json:"value,omitempty"`
	Next   *Code  `json:"next,omitempty"`
	Var    int    `json:"var,omitempty"`
	Type   string `json:"type,omitempty"`
	Result string `json:"result,omitempty"`
	Alts   []Alt  `json:"alts,omitempty"`
	Join   *Join  `json:"join,omitempty"`
	Target int    `json:"target,omitempty"`
	Args   []int  `json:"args,omitzero"`
}
type Function struct {
	Name   string  `json:"name"`
	Params []Param `json:"params"`
	Result string  `json:"result"`
	Body   *Code   `json:"body"`
}
type Provenance struct {
	Name         string    `json:"name"`
	Dependencies []string  `json:"dependencies"`
	Rewrites     []Rewrite `json:"rewrites"`
}

type Program struct {
	Schema      string            `json:"schema"`
	Profile     string            `json:"profile"`
	Stage       string            `json:"stage"`
	LeanVersion string            `json:"lean_version"`
	LeanCommit  string            `json:"lean_commit"`
	Module      string            `json:"module"`
	Root        string            `json:"root"`
	Inputs      map[string]string `json:"inputs"`
	Types       []TypeDecl        `json:"types"`
	Claims      []Claim           `json:"claims"`
	Rewrites    []Rewrite         `json:"rewrites"`
	Provenance  []Provenance      `json:"provenance"`
	Functions   []Function        `json:"functions"`
}
