package integration_test

import "testing"

func TestStandaloneWordsDemo(t *testing.T) {
	testStandaloneDemo(t, "words", "widen(0x80000001) = 0x0000000080000001\n"+
		"pack(0x80000001, 0xffffffff) = 0x80000001ffffffff\n"+
		"high(0x80000001ffffffff) = 0x80000001\n"+
		"low(0x80000001ffffffff) = 0xffffffff\n"+
		"narrow(0x80000001ffffffff) = 0xffffffff\n")
}
