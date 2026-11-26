package integration_test

import "testing"

func TestStandaloneAdler32Demo(t *testing.T) {
	testStandaloneDemo(t, "adler32", "Adler-32(\"Wikipedia\") = 0x11E60398\n")
}
