package main

import (
	"fmt"
	"log"

	"example.com/lean-go-crc16-demo/checksum"
)

func main() {
	const message = "123456789"
	result, err := checksum.Update(0, []byte(message))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("CRC-16/XMODEM(%q) = 0x%04X\n", message, result)
}
