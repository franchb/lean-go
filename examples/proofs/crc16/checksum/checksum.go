// Package checksum computes CRC-16/XMODEM using the extracted Lean chunk update.
package checksum

import crc16 "example.com/lean-go-crc16-demo/crc16"

// Update continues a checksum over chunk. Start with zero for a new message.
// An empty chunk preserves state. An extraction error returns the original state.
func Update(state uint16, chunk []byte) (uint16, error) {
	current := state
	limits := crc16.DefaultLimits()
	// Each extracted call has its own logical execution and input budgets.
	// Keep arbitrary caller chunk and frame sizes independent of those budgets.
	const blockBytes = 256
	for len(chunk) != 0 {
		n := min(len(chunk), blockBytes)
		next, err := crc16.RunUpdate(current, chunk[:n], limits)
		if err != nil {
			return state, err
		}
		current, chunk = next, chunk[n:]
	}
	return current, nil
}
