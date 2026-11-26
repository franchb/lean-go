package wireformat

import (
	"fmt"
	"sort"
	"strings"
)

// The proofs follow one scheme for every spec:
//
//  1. A fixed library of natural-number lemmas (`extract`, `extract0`) and
//     toNat lemmas for the runtime byte helpers.
//  2. Per word, lemmas over abstract natural numbers: its bytes reassemble
//     (`w<k>_bytes`), each member is extracted from a packed value
//     (`w<k>_ext_<j>`), members reassemble (`w<k>_reasm`), and their
//     consequences `w<k>_rt` (serialize then read) and `w<k>_inj` (equal
//     members force equal bytes). Each is proved by rewriting to the shape
//     of `extract` with omega solving only linear side goals, or by a chain
//     of `Nat.mod_mul` facts over generalized atoms.
//  3. Per member, toNat lemmas linking the runtime readers, conditions and
//     serializer to those formulas.
//  4. The claims, by a linear case chain over the checks in runtime order.

const proofLibrary = `theorem extract (lo v hi S M : Nat) (hS : 0 < S) (hlo : lo < S) (hv : v < M) :
    (lo + v * S + hi * (S * M)) / S % M = v := by
  have e : lo + v * S + hi * (S * M) = lo + S * (v + hi * M) := by
    rw [Nat.mul_add, Nat.add_assoc, Nat.mul_comm v S, Nat.mul_left_comm hi S M]
  rw [e, Nat.add_mul_div_left _ _ hS, Nat.div_eq_of_lt hlo, Nat.zero_add,
    Nat.add_mul_mod_self_right, Nat.mod_eq_of_lt hv]

theorem extract0 (v hi M : Nat) (hv : v < M) : (v + hi * M) % M = v := by
  rw [Nat.add_mul_mod_self_right, Nat.mod_eq_of_lt hv]

theorem u8_lt (x : UInt8) : x.toNat < 256 := x.toNat_lt
theorem u16_lt (x : UInt16) : x.toNat < 65536 := x.toNat_lt
theorem u32_lt (x : UInt32) : x.toNat < 4294967296 := x.toNat_lt
theorem u64_lt (x : UInt64) : x.toNat < 18446744073709551616 := x.toNat_lt

theorem be16w_toNat (a b : UInt8) : (be16w a b).toNat = a.toNat * 256 + b.toNat := by
  have := a.toNat_lt; have := b.toNat_lt
  simp [be16w]; omega

theorem be32w_toNat (a b c d : UInt8) :
    (be32w a b c d).toNat = a.toNat * 16777216 + b.toNat * 65536 + c.toNat * 256 + d.toNat := by
  have := a.toNat_lt; have := b.toNat_lt; have := c.toNat_lt; have := d.toNat_lt
  simp [be32w]; omega

theorem be64w_toNat (a b c d e f g h : UInt8) :
    (be64w a b c d e f g h).toNat = a.toNat * 72057594037927936 + b.toNat * 281474976710656 +
      c.toNat * 1099511627776 + d.toNat * 4294967296 + e.toNat * 16777216 + f.toNat * 65536 +
      g.toNat * 256 + h.toNat := by
  have := a.toNat_lt; have := b.toNat_lt; have := c.toNat_lt; have := d.toNat_lt
  have := e.toNat_lt; have := f.toNat_lt; have := g.toNat_lt; have := h.toNat_lt
  simp only [be64w, UInt64.toNat_add, UInt64.toNat_mul, UInt32.toNat_toUInt64, be32w_toNat]
  simp
  omega

theorem u16_toNat (a b : UInt8) : (u16 a b).toNat = a.toNat * 256 + b.toNat := by
  have hlo := b.toNat_lt
  simp only [u16, UInt16.toNat_or, UInt16.toNat_shiftLeft, UInt8.toNat_toUInt16,
    UInt16.reduceToNat, Nat.reduceMod, Nat.shiftLeft_eq, Nat.reducePow]
  have ha := a.toNat_lt
  rw [Nat.mod_eq_of_lt (by omega), Nat.mul_comm, ← Nat.two_pow_add_eq_or_of_lt (i := 8) hlo]

theorem to16_toNat (v : UInt32) (hv : v.toNat < 65536) : (to16 v).toNat = v.toNat := by
  simp only [to16, UInt16.toNat_or, UInt16.toNat_shiftLeft, UInt8.toNat_toUInt16,
    UInt32.toNat_toUInt8, UInt32.toNat_shiftRight, UInt16.reduceToNat, UInt32.reduceToNat,
    Nat.reduceMod, Nat.shiftLeft_eq, Nat.reducePow, Nat.shiftRight_eq_div_pow]
  have h2 : v.toNat / 256 % 256 * 256 % 65536 = 2 ^ 8 * (v.toNat / 256) := by omega
  rw [h2, ← Nat.two_pow_add_eq_or_of_lt (by omega)]
  omega

theorem mask_and (n k : Nat) : n &&& (2 ^ k - 1) = n % 2 ^ k := Nat.and_two_pow_sub_one_eq_mod n k
`

type proofWriter struct {
	f *Format
	b strings.Builder
}

func (pw *proofWriter) p(format string, args ...any) { fmt.Fprintf(&pw.b, format, args...) }

func vars(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%s%d", prefix, i)
	}
	return out
}

func bindersOf(names []string, typ string) string {
	return "(" + strings.Join(names, " ") + " : " + typ + ")"
}

func ranges(names []string, bounds []string, prefix string) string {
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = fmt.Sprintf("(%s%d : %s < %s)", prefix, i, n, bounds[i])
	}
	return strings.Join(parts, " ")
}

func memberBounds(w *Word) []string {
	out := make([]string, len(w.Subs))
	for j, s := range w.Subs {
		out[j] = pow2Str(s.Width)
	}
	return out
}

func byteBounds(k int) []string {
	out := make([]string, k)
	for i := range out {
		out[i] = "256"
	}
	return out
}

// wordLemmas writes the abstract natural-number lemmas for one word.
func (pw *proofWriter) wordLemmas(w *Word) {
	k, n, bits := w.Size, len(w.Subs), w.Bits()
	name := fmt.Sprintf("w%d", w.Index)
	top := pow2Str(bits)
	pw.p("\n/-! Word %d: bytes %v, %d bits. -/\n\n", w.Index, w.Order, bits)

	// Bytes reassemble.
	bytesW := make([]string, k)
	for p := range k {
		bytesW[p] = byteOf("W", true, k, p)
	}
	xw, _ := wordValue(bytesW)
	pw.p("theorem %s_bytes (W : Nat) (hW : W < %s) : %s = W := ", name, top, xw)
	if k == 1 {
		pw.p("Nat.mod_eq_of_lt hW\n")
	} else {
		pw.p("by\n")
		for j := 1; j < k; j++ {
			a := bytePow(j)
			pw.p("  have e%d : W %% %s = W %% %d + %d * (W / %d %% 256) := Nat.mod_mul (x := W) (a := %d) (b := 256)\n",
				j, pow2Str(8*(j+1)), a, a, a, a)
		}
		pw.p("  have e%d : W %% %s = W := Nat.mod_eq_of_lt hW\n", k, top)
		for p := range k {
			pw.p("  generalize %s = f%d at *\n", bytesW[p], p)
		}
		for j := 2; j <= k; j++ {
			pw.p("  generalize W %% %s = m%d at *\n", pow2Str(8*j), j)
		}
		pw.p("  omega\n")
	}

	vs := vars("v", n)
	vb := memberBounds(w)
	vbind := bindersOf(vs, "Nat") + " " + ranges(vs, vb, "r")
	rs := strings.Join(vars("r", n), " ")
	packedV := packed(w, vs)
	if w.Group {
		// Members are extracted from a packed value.
		for j, s := range w.Subs {
			var lo, hi []string
			for i, o := range w.Subs {
				switch {
				case o.Shift < s.Shift:
					lo = append(lo, fmt.Sprintf("%s * %d", vs[i], o.Scale()))
				case o.Shift > s.Shift:
					hi = append(hi, fmt.Sprintf("%s * %d", vs[i], o.Scale()/(s.Scale()*s.Modulus())))
				}
			}
			sum := func(parts []string) string {
				if len(parts) == 0 {
					return "0"
				}
				return "(" + strings.Join(parts, " + ") + ")"
			}
			pw.p("theorem %s_ext_%d %s :\n    %s = %s := by\n", name, j, vbind, memberValue(s, packedV, false), vs[j])
			if s.Shift == 0 {
				pw.p("  rw [show %s = %s + %s * %d by omega]\n", packedV, vs[j], sum(hi), s.Modulus())
				pw.p("  exact extract0 %s _ %d r%d\n", vs[j], s.Modulus(), j)
			} else {
				pw.p("  rw [show %s = %s + %s * %d + %s * (%d * %d) by omega]\n",
					packedV, sum(lo), vs[j], s.Scale(), sum(hi), s.Scale(), s.Modulus())
				pw.p("  exact extract _ %s _ %d %d (by decide) (by omega) r%d\n", vs[j], s.Scale(), s.Modulus(), j)
			}
		}
		// Members reassemble.
		fw := make([]string, n)
		for j, s := range w.Subs {
			fw[j] = memberValue(s, "W", true)
		}
		pw.p("theorem %s_reasm (W : Nat) (hW : W < %s) :\n    %s = W := by\n", name, top, packed(w, fw))
		asc := append([]*Sub(nil), w.Subs...)
		sort.Slice(asc, func(i, j int) bool { return asc[i].Shift < asc[j].Shift })
		for i, s := range asc {
			if s.Shift == 0 {
				continue
			}
			pw.p("  have e%d : W %% %d = W %% %d + %d * (%s) := Nat.mod_mul (x := W) (a := %d) (b := %d)\n",
				i, s.Scale()*s.Modulus(), s.Scale(), s.Scale(), memberValue(s, "W", true), s.Scale(), s.Modulus())
		}
		pw.p("  have eT : W %% %s = W := Nat.mod_eq_of_lt hW\n", top)
		seen := map[string]bool{}
		for j := range w.Subs {
			if !seen[fw[j]] {
				seen[fw[j]] = true
				pw.p("  generalize %s = f%d at *\n", fw[j], j)
			}
		}
		for _, s := range asc {
			if s.Shift == 0 {
				continue
			}
			for _, t := range []string{fmt.Sprintf("W %% %d", s.Scale()), fmt.Sprintf("W %% %d", s.Scale()*s.Modulus())} {
				if !seen[t] {
					seen[t] = true
					pw.p("  generalize %s = m%d at *\n", t, len(seen))
				}
			}
		}
		pw.p("  omega\n")
	}

	// Serialize then read.
	readBack := make([]string, k)
	for p := range k {
		readBack[p] = byteOf(packedV, false, k, p)
	}
	xr, xrAtomic := wordValue(readBack)
	var rt []string
	for j, s := range w.Subs {
		rt = append(rt, fmt.Sprintf("%s = %s", memberValue(s, xr, xrAtomic), vs[j]))
	}
	pw.p("theorem %s_rt %s :\n    %s := by\n", name, vbind, lines(rt, "    "))
	pw.p("  rw [%s_bytes (%s) (by omega)]\n", name, packedV)
	if w.Group {
		var parts []string
		for j := range w.Subs {
			parts = append(parts, fmt.Sprintf("%s_ext_%d %s %s", name, j, strings.Join(vs, " "), rs))
		}
		pw.p("  exact %s\n", tuple(parts))
	} else {
		pw.p("  omega\n")
	}

	// Byte extraction.
	as, bs := vars("a", k), vars("b", k)
	xa, xaAtomic := wordValue(as)
	xb, xbAtomic := wordValue(bs)
	if k > 1 {
		for p := range k {
			q := bytePow(k - 1 - p)
			var lo, hi []string
			for i := range k {
				qi := bytePow(k - 1 - i)
				switch {
				case i > p:
					if qi == 1 {
						lo = append(lo, as[i])
					} else {
						lo = append(lo, fmt.Sprintf("%s * %d", as[i], qi))
					}
				case i < p:
					hi = append(hi, fmt.Sprintf("%s * %d", as[i], qi/(q*256)))
				}
			}
			sum := func(parts []string) string {
				if len(parts) == 0 {
					return "0"
				}
				return "(" + strings.Join(parts, " + ") + ")"
			}
			pw.p("theorem %s_bext_%d %s %s :\n    %s = %s := by\n", name, p, bindersOf(as, "Nat"),
				ranges(as, byteBounds(k), "r"), byteOf(xa, false, k, p), as[p])
			if q == 1 {
				pw.p("  rw [show %s = %s + %s * 256 by omega]\n", xa, as[p], sum(hi))
				pw.p("  exact extract0 %s _ 256 r%d\n", as[p], p)
			} else {
				pw.p("  rw [show %s = %s + %s * %d + %s * (%d * 256) by omega]\n", xa, sum(lo), as[p], q, sum(hi), q)
				pw.p("  exact extract _ %s _ %d 256 (by decide) (by omega) r%d\n", as[p], q, p)
			}
		}
	}

	// The bytes bound the word; stated alone so omega sees only the ranges.
	pw.p("theorem %s_xbound %s %s : %s < %s := by omega\n", name, bindersOf(as, "Nat"),
		ranges(as, byteBounds(k), "r"), xa, top)

	// Equal members force equal bytes.
	var fs []string
	for j, s := range w.Subs {
		fs = append(fs, fmt.Sprintf("(f%d : %s = %s)", j, memberValue(s, xa, xaAtomic), memberValue(s, xb, xbAtomic)))
	}
	var eqs []string
	for p := range k {
		eqs = append(eqs, fmt.Sprintf("%s = %s", as[p], bs[p]))
	}
	pw.p("theorem %s_inj %s %s %s\n    %s :\n    %s := by\n", name,
		bindersOf(append(append([]string{}, as...), bs...), "Nat"),
		ranges(as, byteBounds(k), "ra"), ranges(bs, byteBounds(k), "rb"),
		strings.Join(fs, " "), conj(eqs))
	if w.Group {
		pw.p("  have e : %s = %s := by\n", xa, xb)
		pw.p("    rw [← %s_reasm (%s) (%s_xbound %s %s), ← %s_reasm (%s) (%s_xbound %s %s), %s]\n",
			name, xa, name, strings.Join(as, " "), strings.Join(vars("ra", k), " "),
			name, xb, name, strings.Join(bs, " "), strings.Join(vars("rb", k), " "),
			strings.Join(vars("f", n), ", "))
	} else {
		pw.p("  have e : %s = %s := f0\n", xa, xb)
	}
	if k == 1 {
		pw.p("  exact e\n")
	} else {
		pw.p("  refine %s\n", tuple(repeat("?_", k)))
		ra, rb := strings.Join(vars("ra", k), " "), strings.Join(vars("rb", k), " ")
		for p := range k {
			pw.p("  · rw [← %s_bext_%d %s %s, ← %s_bext_%d %s %s, e]\n",
				name, p, strings.Join(as, " "), ra, name, p, strings.Join(bs, " "), rb)
		}
	}
}

func repeat(s string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}
	return out
}
