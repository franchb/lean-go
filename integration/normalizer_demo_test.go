package integration_test

import "testing"

func TestStandaloneNormalizerDemo(t *testing.T) {
	testStandaloneDemo(t, "normalizer", "normalize(not (x4294967295 and U)) = (not x4294967295 or U)\n"+
		"normalize(not not F) = F\n"+
		"normalize(not (F or x0)) = (T and not x0)\n")
}
