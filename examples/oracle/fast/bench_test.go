package fast

import (
	"hash/adler32"
	"math/rand/v2"
	"testing"

	adlermodel "example.com/lean-go-oracle/model/adler32"
	crcmodel "example.com/lean-go-oracle/model/crc16"
)

func benchInput() []byte {
	r := rand.New(rand.NewPCG(64, 1024))
	p := make([]byte, 64<<10)
	for i := range p {
		p[i] = byte(r.Uint32())
	}
	return p
}

var sinkAdler uint32
var sinkCRC uint16

func BenchmarkAdler32(b *testing.B) {
	p := benchInput()
	b.Run("fast", func(b *testing.B) {
		b.SetBytes(int64(len(p)))
		for b.Loop() {
			sinkAdler = UpdateAdler32(AdlerInit, p)
		}
	})
	b.Run("model", func(b *testing.B) {
		b.SetBytes(int64(len(p)))
		for b.Loop() {
			sinkAdler = adlermodel.Checksum(adlermodel.Update(adlermodel.Initial(), p))
		}
	})
	b.Run("stdlib", func(b *testing.B) {
		b.SetBytes(int64(len(p)))
		for b.Loop() {
			sinkAdler = adler32.Checksum(p)
		}
	})
}

func BenchmarkCRC16(b *testing.B) {
	p := benchInput()
	b.Run("fast", func(b *testing.B) {
		b.SetBytes(int64(len(p)))
		for b.Loop() {
			sinkCRC = UpdateCRC16(CRC16Init, p)
		}
	})
	b.Run("model", func(b *testing.B) {
		b.SetBytes(int64(len(p)))
		for b.Loop() {
			sinkCRC = crcmodel.Update(modelCRCInit, p)
		}
	})
}
