package pipeline

import (
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strings"

	"github.com/franchb/lean-go/internal/leanversion"
)

// Compiler is one exactly qualified Lean release.
type Compiler struct {
	Version string
	Commit  string
}

// DefaultCompiler is the production pin.
var DefaultCompiler = Compiler{leanversion.Version, leanversion.Commit}

// SupportedCompilers lists every release the producer and validators admit.
var SupportedCompilers = []Compiler{DefaultCompiler, {"4.36.0-rc2", "b35cb10cf2ced51a2fd1880cfdbf75c71ab7a35a"}}

// Toolchain is the elan toolchain name.
func (c Compiler) Toolchain() string { return "leanprover/lean4:v" + c.Version }

// PinFile is the lean-toolchain file staged for Lake.
func (c Compiler) PinFile() []byte { return []byte(c.Toolchain() + "\n") }

// CheckVersion requires the exact version and commit in `lean --version` output.
func (c Compiler) CheckVersion(text string) error {
	pattern := `^Lean \(version ` + regexp.QuoteMeta(c.Version) + `, [^,\r\n]+, commit ` + regexp.QuoteMeta(c.Commit) + `, Release\)$`
	if !regexp.MustCompile(pattern).MatchString(strings.TrimSpace(text)) {
		return exportErrorf("unexpected Lean version or commit: %s", strings.TrimSpace(text))
	}
	return nil
}

// SelectedCompiler honours LEAN_GO_TOOLCHAIN, defaulting to the production pin.
func SelectedCompiler() (Compiler, error) {
	version, ok := os.LookupEnv("LEAN_GO_TOOLCHAIN")
	if !ok {
		version = DefaultCompiler.Version
	}
	for _, c := range SupportedCompilers {
		if c.Version == version {
			return c, nil
		}
	}
	names := make([]string, len(SupportedCompilers))
	for i, c := range SupportedCompilers {
		names[i] = c.Version
	}
	return Compiler{}, exportErrorf("unsupported LEAN_GO_TOOLCHAIN %s; expected one of %s", pythonRepr(version), strings.Join(names, ", "))
}

// RequiredGo is the Go release whose go/format output the tracked artifacts use.
const RequiredGo = "go1.27.4"

// CheckBuildToolchain requires that this binary was built with RequiredGo.
func CheckBuildToolchain() error { return checkGoVersion(runtime.Version()) }

func checkGoVersion(version string) error {
	if version != RequiredGo && !strings.HasPrefix(version, RequiredGo+" ") {
		return fmt.Errorf("expected Go 1.27.4, got %s; build lean-go with the pinned Go toolchain", version)
	}
	return nil
}
