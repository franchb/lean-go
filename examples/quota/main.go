package main

import (
	"fmt"
	"log"

	"github.com/franchb/lean-go/quota"
)

func main() {
	state, err := quota.New(10)
	if err != nil {
		log.Fatal(err)
	}
	state, accepted, err := state.Reserve(7)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("reserve 7:", accepted)
	state, accepted, err = state.Reserve(4)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("reserve 4:", accepted)
	state, accepted, err = state.Release(2)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("release 2:", accepted)
	remaining, err := state.Remaining()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("remaining:", remaining)
}
