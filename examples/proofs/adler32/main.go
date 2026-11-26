package main

import (
	"fmt"
	"log"

	x "example.com/lean-go-adler32-demo/adler32"
)

// update carries the extracted state across bounded calls for arbitrary input sizes.
func update(state x.State, data []byte) (x.State, error) {
	limits := x.DefaultLimits()
	current := state
	for len(data) > 0 {
		n := min(256, len(data))
		next, err := x.RunUpdate(current, data[:n], limits)
		if err != nil {
			return state, err
		}
		current, data = next, data[n:]
	}
	return current, nil
}

func main() {
	limits := x.DefaultLimits()
	state, err := x.RunInitial(limits)
	if err != nil {
		log.Fatal(err)
	}
	const message = "Wikipedia"
	state, err = update(state, []byte(message))
	if err != nil {
		log.Fatal(err)
	}
	sum, err := x.RunChecksum(state, limits)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Adler-32(%q) = 0x%08X\n", message, sum)
}
