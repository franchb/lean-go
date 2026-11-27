package fast

// CRC16Poly is the CRC-16/XMODEM polynomial x^16 + x^12 + x^5 + 1 without its
// leading term. The register is not reflected and has no final XOR.
const CRC16Poly = 0x1021

// CRC16Init is the CRC-16/XMODEM initial register.
const CRC16Init uint16 = 0

// CRC16Table holds the lookup tables for a non-reflected 16-bit CRC.
// Entry i of the first table is the register after shifting i<<8 through
// eight polynomial steps; entry i of the second is the register after
// sixteen steps. With both, two bytes cost two independent lookups.
type CRC16Table [2][256]uint16

// MakeCRC16Table returns the tables for poly.
func MakeCRC16Table(poly uint16) *CRC16Table {
	t := new(CRC16Table)
	for i := range 256 {
		r := uint16(i) << 8
		for range 8 {
			if r&0x8000 != 0 {
				r = r<<1 ^ poly
			} else {
				r <<= 1
			}
		}
		t[0][i] = r
	}
	for i := range 256 {
		// Eight more steps on t[0][i], whose low byte is now the top byte
		// of the register shifted by eight.
		r := t[0][i]
		t[1][i] = r<<8 ^ t[0][r>>8]
	}
	return t
}

var xmodemTable = MakeCRC16Table(CRC16Poly)

// UpdateCRC16 continues a CRC-16/XMODEM computation over p. Pass the result
// of one call to the next; start with CRC16Init. The last result is the CRC
// of the concatenated chunks.
func UpdateCRC16(crc uint16, p []byte) uint16 {
	return xmodemTable.Update(crc, p)
}

// Update continues a non-reflected CRC over p using the tables in t.
func (t *CRC16Table) Update(crc uint16, p []byte) uint16 {
	// Two bytes at a time: XOR them into the register, then the high byte
	// needs sixteen steps and the low byte eight.
	// The result depends on the register and the two bytes only through x.
	for len(p) >= 2 {
		x := crc ^ uint16(p[0])<<8 ^ uint16(p[1])
		crc = t[1][x>>8] ^ t[0][x&0xff]
		p = p[2:]
	}
	if len(p) == 1 {
		crc = crc<<8 ^ t[0][byte(crc>>8)^p[0]]
	}
	return crc
}
