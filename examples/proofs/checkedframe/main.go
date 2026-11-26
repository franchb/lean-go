package main

import (
	"bytes"
	"fmt"
	"log"

	"example.com/lean-go-checkedframe-demo/checkedframe"
)

func main() {
	wire, err := checkedframe.Encode(2, []byte("123456789"))
	if err != nil {
		log.Fatal(err)
	}
	got, err := checkedframe.ReadFrameFrom(bytes.NewReader(wire))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("wire %x\npayload %s\n", wire, got.Payload)
}
