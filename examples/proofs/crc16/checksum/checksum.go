// Package checksum computes CRC-16/XMODEM by calling the extracted Lean byte update.
package checksum

import crc16 "example.com/lean-go-crc16-demo/crc16"

// Update continues a checksum over chunk. Start with zero for a new message.
// An empty chunk preserves state. An extraction error returns the original state.
func Update(state uint16, chunk []byte) (uint16, error) {
	limits := crc16.DefaultLimits()
	current := state
	for _, b := range chunk {
		next, err := crc16.RunTyped(current, b, limits)
		if err != nil {
			return state, err
		}
		current = next
	}
	return current, nil
}
