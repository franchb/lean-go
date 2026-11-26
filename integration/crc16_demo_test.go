package integration_test

import "testing"

func TestStandaloneCRC16Demo(t *testing.T) {
	testStandaloneDemo(t, "crc16", "CRC-16/XMODEM(\"123456789\") = 0x31C3\n", "checksum/checksum.go")
}
