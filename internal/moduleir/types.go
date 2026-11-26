// Package moduleir validates the bounded pure-module extraction profile.
package moduleir

import "github.com/franchb/lean-go/internal/leanversion"

// Schema and Profile are the only accepted module IR identities. Capabilities
// are declared by Program.Features rather than by schema or profile versions.
const (
	Schema          = "lean-go-ir/v24"
	ManifestSchema  = "lean-go-manifest/v24"
	Profile         = "pure-module/v22"
	Stage           = "pure-mono"
	LeanVersion     = leanversion.Version
	LeanCommit      = leanversion.Commit
	MaxBytes        = 4 << 20
	MaxNodes        = 10000
	MaxCodeDepth    = 256
	MaxTypes        = 256
	MaxFunctions    = 256
	MaxRoots        = 64
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

// APIMetadata supplies presentation names without changing runtime type shapes.
type APIMetadata struct {
	Records map[string][]string `json:"records"`
}

type Program struct {
	Schema      string            `json:"schema"`
	Profile     string            `json:"profile"`
	Stage       string            `json:"stage"`
	LeanVersion string            `json:"lean_version"`
	LeanCommit  string            `json:"lean_commit"`
	Module      string            `json:"module"`
	Roots       []string          `json:"roots"`
	Features    []string          `json:"features"`
	Inputs      map[string]string `json:"inputs"`
	Types       []TypeDecl        `json:"types"`
	Claims      []Claim           `json:"claims"`
	Rewrites    []Rewrite         `json:"rewrites"`
	Provenance  []Provenance      `json:"provenance"`
	Functions   []Function        `json:"functions"`
	API         *APIMetadata      `json:"api"`
}
