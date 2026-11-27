package integration_test

import "testing"

func TestStandaloneClockDemo(t *testing.T) {
	testStandaloneDemo(t, "clock", "0 s = 0 h 0 min 0 s = 0 s\n"+
		"3661 s = 1 h 1 min 1 s = 3661 s\n"+
		"86399 s = 23 h 59 min 59 s = 86399 s\n"+
		"4294967295 s = 1193046 h 28 min 15 s = 4294967295 s\n")
}
