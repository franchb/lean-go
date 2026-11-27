package nativegen_test

import (
	"github.com/franchb/lean-go/internal/modulegen"
	"github.com/franchb/lean-go/internal/nativegen"
)

// The differential tests compare native output with metered output. The
// metered generator imports this package, so it is wired in from here.
func init() { nativegen.MeteredGenerate = modulegen.Generate }
