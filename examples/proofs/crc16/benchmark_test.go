package main

import (
	"testing"

	"example.com/lean-go-crc16-demo/checksum"
)

var result uint16

func BenchmarkCRC1KiB(b *testing.B) {
	data := make([]byte, 1024)
	for i := range data {
		data[i] = byte(i)
	}
	want := reference(0, data)
	if got, err := checksum.Update(0, data); err != nil || got != want {
		b.Fatalf("checksum mismatch: %04x, %v (want %04x)", got, err, want)
	}
	b.Run("Generated", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		for b.Loop() {
			got, err := checksum.Update(0, data)
			if err != nil {
				b.Fatal(err)
			}
			result = got
		}
	})
	b.Run("BitReference", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		for b.Loop() {
			result = reference(0, data)
		}
	})
}
