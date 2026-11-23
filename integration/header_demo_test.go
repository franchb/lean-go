package integration_test

import "testing"

func TestStandaloneHeaderDemo(t *testing.T) {
	testStandaloneDemo(t, "header", "[01 03 12 34] -> flags=3 length=4660 -> [01 03 12 34]\n"+
		"[02 80 00 00] -> unsupported version\n"+
		"[01 04 00 00] -> reserved flags\n")
}
