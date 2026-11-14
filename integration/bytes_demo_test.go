package integration_test

import "testing"

func TestStandaloneBytesDemo(t *testing.T) {
	testStandaloneDemo(t, "bytes", "LE(0x12345678) = [78 56 34 12] -> 0x12345678\n"+
		"BE(0x12345678) = [12 34 56 78] -> 0x12345678\n")
}
