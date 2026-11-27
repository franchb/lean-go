package nativegen

import (
	"slices"
	"testing"

	"github.com/franchb/lean-go/internal/moduleir"
)

// The native matrix, the validator's catalog and the producer admit the same
// 68 names, with the same signatures and semantics text.
func TestNativeMatrixMatchesValidatorCatalog(t *testing.T) {
	names := AllPrimitives()
	if len(names) != 68 {
		t.Fatal(len(names))
	}
	for _, name := range names {
		native, _ := LookupPrimitive(name)
		admitted, ok := moduleir.LookupPrimitive(name)
		if !ok || admitted.Name != native.Name || admitted.Result != native.Result ||
			!slices.Equal(admitted.Params, native.Params) || admitted.Semantics != native.Semantics {
			t.Fatalf("%s: native %+v, validator %+v", name, native, admitted)
		}
	}
}
