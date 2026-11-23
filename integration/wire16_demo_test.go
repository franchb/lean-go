package integration_test

import "testing"

func TestStandaloneWire16Demo(t *testing.T) {
	testStandaloneDemo(t, "wire16", "0xabcd -> [ab cd] -> 0xabcd\n"+
		"[01 03 12 34] -> flags=3 length=4660 -> [01 03 12 34]\n"+
		"[02 80 00 00] -> unsupported version\n"+
		"[01 04 00 00] -> reserved flags\n")
}
