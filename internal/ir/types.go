// Package ir admits the closed audited-normalizer/v1 extraction profile.
package ir

const (
	Schema       = "lean-go-ir/v1"
	Profile      = "audited-normalizer/v1"
	Stage        = "pure-mono"
	LeanVersion  = "4.35.0"
	LeanCommit   = "eb6cafb7290e8bd5f2937522d8d804f58f02bafc"
	ModelSHA256  = "c67a835949d1786638d2809c970ef69861a074a23cf6fdbcae3f8cad1043771f"
	Root         = "LeanGo.Feasibility.normalize"
	MaxBytes     = 4 << 20
	MaxNodes     = 10000
	MaxCodeDepth = 256
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
type Code struct {
	Op     string `json:"op"`
	Bind   *Param `json:"bind,omitempty"`
	Value  *Value `json:"value,omitempty"`
	Next   *Code  `json:"next,omitempty"`
	Var    int    `json:"var,omitempty"`
	Type   string `json:"type,omitempty"`
	Result string `json:"result,omitempty"`
	Alts   []Alt  `json:"alts,omitempty"`
}
type Function struct {
	Name   string  `json:"name"`
	Params []Param `json:"params"`
	Result string  `json:"result"`
	Body   *Code   `json:"body"`
}
type Program struct {
	Schema      string            `json:"schema"`
	Profile     string            `json:"profile"`
	Stage       string            `json:"stage"`
	LeanVersion string            `json:"lean_version"`
	LeanCommit  string            `json:"lean_commit"`
	ModelSHA256 string            `json:"model_sha256"`
	Root        string            `json:"root"`
	Inputs      map[string]string `json:"inputs"`
	Types       []TypeDecl        `json:"types"`
	Claims      []Claim           `json:"claims"`
	Rewrites    []Rewrite         `json:"rewrites"`
	Functions   []Function        `json:"functions"`
}
