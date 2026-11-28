// Package proofdemos is the independent inventory of the standalone proof
// demos under examples/proofs: for each demo the module and roots it
// extracts, every source claim its proof contract must check, the consumer
// files deployed with Go alone and the exact output of its program.
//
// The inventory is written out by hand on purpose. A demo whose contract and
// evidence silently lost a claim, or whose program printed something else,
// would still agree with itself; it does not agree with this list. The
// integration tests check the tracked contracts, evidence and programs
// against it, and lean-go-dev installed-cli checks freshly generated ones.
package proofdemos

// Demo is one standalone proof demo, examples/proofs/<Name>.
type Demo struct {
	Name, Module string
	// Roots are the extracted roots, sorted.
	Roots []string
	// Claims are the (theorem, expected proposition) pairs, in no order.
	Claims [][2]string
	// ExtraFiles are consumer files deployed besides go.mod, main.go,
	// main_test.go and the generated package's Go files.
	ExtraFiles []string
	// Output is the exact output of `go run .`.
	Output string
}

// GeneratedFiles are the Go files of a generated metered package.
var GeneratedFiles = []string{"api.go", "control.go", "runtime.go", "schema.go"}

// ConsumerFiles lists the files a Go-only deployment of d copies: the
// consumer, its extra files and the generated package's Go files.
func (d Demo) ConsumerFiles() []string {
	files := append([]string{"go.mod", "main.go", "main_test.go"}, d.ExtraFiles...)
	for _, name := range GeneratedFiles {
		files = append(files, d.Name+"/"+name)
	}
	return files
}

func pairs(proofs, contract string, names ...string) [][2]string {
	out := make([][2]string, len(names))
	for i, name := range names {
		out[i] = [2]string{proofs + "." + name, contract + "." + name}
	}
	return out
}

// All lists the ten demos in the order the installed-CLI check runs them.
var All = []Demo{
	{Name: "clamp", Module: "Clamp", Roots: []string{"Clamp.clamp"},
		Claims: pairs("ClampProofs", "ClampContract", "bounded", "idempotent", "preserves"),
		Output: "clamp(7, 42) = 7\nclamp(100, 42) = 42\nclamp(18446744073709551615, 42) = 42\n"},
	{Name: "flags", Module: "Flags", Roots: []string{"Flags.clear", "Flags.contains", "Flags.set", "Flags.toggle"},
		Claims: pairs("FlagsProofs", "FlagsContract", "clear_idempotent", "clear_untouched", "cleared_bits_absent", "contains_set",
			"set_idempotent", "set_untouched", "toggle_involution", "toggle_untouched"),
		Output: "set(0x00000001, 0x80000002) = 0x80000003\ncontains(0x80000003, 0x80000002) = true\n" +
			"clear(0x80000003, 0x00000002) = 0x80000001\ntoggle(0x80000001, 0x80000000) = 0x00000001\n"},
	{Name: "words", Module: "Words", Roots: []string{"Words.high", "Words.low", "Words.narrow", "Words.pack", "Words.widen"},
		Claims: pairs("WordsProofs", "WordsContract", "widen_preserves", "narrow_truncates", "narrow_widen", "widen_narrow",
			"low_pack", "high_pack", "pack_unpack"),
		Output: "widen(0x80000001) = 0x0000000080000001\npack(0x80000001, 0xffffffff) = 0x80000001ffffffff\n" +
			"high(0x80000001ffffffff) = 0x80000001\nlow(0x80000001ffffffff) = 0xffffffff\nnarrow(0x80000001ffffffff) = 0xffffffff\n"},
	{Name: "bytes", Module: "ByteCodec", Roots: []string{"ByteCodec.decodeBE", "ByteCodec.decodeLE", "ByteCodec.encodeBE", "ByteCodec.encodeLE"},
		Claims: pairs("ByteCodecProofs", "ByteCodecContract", "decodeLE_encodeLE", "decodeBE_encodeBE", "encodeLE_decodeLE",
			"encodeBE_decodeBE", "encodeLE_b0", "encodeLE_b1", "encodeLE_b2", "encodeLE_b3", "encodeBE_b0", "encodeBE_b1",
			"encodeBE_b2", "encodeBE_b3"),
		Output: "LE(0x12345678) = [78 56 34 12] -> 0x12345678\nBE(0x12345678) = [12 34 56 78] -> 0x12345678\n"},
	{Name: "header", Module: "HeaderCodec", Roots: []string{"HeaderCodec.encode", "HeaderCodec.parse"},
		Claims: pairs("HeaderCodecProofs", "HeaderCodecContract", "parse_success_iff", "parse_unsupported_version_iff",
			"parse_reserved_flags_iff", "parsed_length_bound", "encode_success_iff", "encode_reserved_flags_iff",
			"encode_length_overflow_iff", "parse_encode_roundtrip", "encode_parse_roundtrip"),
		Output: "[01 03 12 34] -> flags=3 length=4660 -> [01 03 12 34]\n[02 80 00 00] -> unsupported version\n" +
			"[01 04 00 00] -> reserved flags\n"},
	{Name: "wire16", Module: "Wire16", Roots: []string{"Wire16.classify", "Wire16.decodeBE", "Wire16.encode", "Wire16.encodeBE", "Wire16.parse"},
		Claims: pairs("Wire16Proofs", "Wire16Contract", "encode_be_bytes", "decode_be_value", "decode_encode_be", "encode_decode_be",
			"parse_success_iff", "parse_unsupported_version_iff", "parse_reserved_flags_iff", "encode_success_iff",
			"encode_reserved_flags_iff", "parse_encode_roundtrip", "encode_parse_roundtrip", "need_more_iff", "complete_iff"),
		ExtraFiles: []string{"frame/frame.go", "frame/frame_test.go", "frame/append_test.go", "frame/roundtrip_test.go",
			"frame/stream_test.go", "frame/stream_write_test.go"},
		Output: "0xabcd -> [ab cd] -> 0xabcd\n[01 03 12 34] -> flags=3 length=4660 -> [01 03 12 34]\n" +
			"[02 80 00 00] -> unsupported version\n[01 04 00 00] -> reserved flags\n"},
	{Name: "crc16", Module: "Crc16", Roots: []string{"Crc16.update", "Crc16.updateByte"},
		Claims:     pairs("Crc16Proofs", "Crc16Contract", "update_byte_spec", "chunks_spec"),
		ExtraFiles: []string{"checksum/checksum.go"},
		Output:     "CRC-16/XMODEM(\"123456789\") = 0x31C3\n"},
	{Name: "adler32", Module: "Adler32", Roots: []string{"Adler32.checksum", "Adler32.initial", "Adler32.update"},
		Claims: pairs("Adler32Proofs", "Adler32Contract", "chunks_spec", "step_spec"),
		Output: "Adler-32(\"Wikipedia\") = 0x11E60398\n"},
	{Name: "normalizer", Module: "Normalizer", Roots: []string{"Normalizer.normalize"},
		Claims: pairs("NormalizerProofs", "NormalizerContract", "normalize_is_normal", "normalize_preserves_eval"),
		Output: "normalize(not (x4294967295 and U)) = (not x4294967295 or U)\nnormalize(not not F) = F\n" +
			"normalize(not (F or x0)) = (T and not x0)\n"},
	{Name: "clock", Module: "Clock", Roots: []string{"Clock.join", "Clock.split"},
		Claims: pairs("ClockProofs", "ClockContract", "minutesBelow60", "recombines", "secondsBelow60"),
		Output: "0 s = 0 h 0 min 0 s = 0 s\n3661 s = 1 h 1 min 1 s = 3661 s\n86399 s = 23 h 59 min 59 s = 86399 s\n" +
			"4294967295 s = 1193046 h 28 min 15 s = 4294967295 s\n"},
}

// Frame is the checked-frame composition over the wire16 and crc16 demos.
var Frame = struct {
	// Components are the bound component names, sorted.
	Components []string
	// Claims are the composition claims, in contract order.
	Claims [][2]string
	// Output is the exact output of `go run .` in examples/proofs/checkedframe.
	Output string
}{
	Components: []string{"crc16", "wire16"},
	Claims:     pairs("CheckedFrameProofs", "CheckedFrameContract", "accepts_reference_crc", "roundtrip"),
	Output:     "wire 010200093132333435363738395744\npayload 123456789\n",
}
