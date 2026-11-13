package integration_test

import "testing"

func TestStandaloneFlagsDemo(t *testing.T) {
	testStandaloneDemo(t, "flags", "set(0x00000001, 0x80000002) = 0x80000003\n"+
		"contains(0x80000003, 0x80000002) = true\n"+
		"clear(0x80000003, 0x00000002) = 0x80000001\n"+
		"toggle(0x80000001, 0x80000000) = 0x00000001\n")
}
