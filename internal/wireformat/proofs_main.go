package wireformat

import (
	"fmt"
	"sort"
	"strings"
)

func toNatLemma(w *Word) string {
	switch w.Size {
	case 2:
		return "be16w_toNat"
	case 4:
		return "be32w_toNat"
	case 8:
		return "be64w_toNat"
	}
	return ""
}

func boundLemma(bits int) string { return fmt.Sprintf("u%d_lt", bits) }

// byteFacts states the range of every byte of a word for omega.
func byteFacts(w *Word, indent string) string {
	var b strings.Builder
	for _, i := range w.Order {
		fmt.Fprintf(&b, "%shave := u8_lt w.b%d\n", indent, i)
	}
	return b.String()
}

func (pw *proofWriter) masks() {
	seen := map[int]bool{}
	var widths []int
	for _, s := range pw.f.Subs {
		if !s.Whole() && !seen[s.Width] {
			seen[s.Width] = true
			widths = append(widths, s.Width)
		}
	}
	sort.Ints(widths)
	for _, wd := range widths {
		pw.p("theorem and_%d (n : Nat) : n &&& %d = n %% %d := by simpa using mask_and n %d\n",
			pow2(wd)-1, pow2(wd)-1, pow2(wd), wd)
	}
}

func maskLemma(s *Sub) string {
	if s.Whole() {
		return ""
	}
	return fmt.Sprintf(", and_%d", s.Modulus()-1)
}

// memberLemmas links runtime readers and conditions to the formulas.
func (pw *proofWriter) memberLemmas() {
	f := pw.f
	pw.p("\n/-! Readers: the wide value of every member as a function of the bytes. -/\n\n")
	for _, s := range f.Subs {
		simps := toNatLemma(s.Word)
		if simps == "" {
			simps = "UInt8.toNat_toUInt32"
		}
		if !s.Whole() {
			simps += ", " + getterName(s)
		}
		if s.Shift != 0 {
			simps += ", Nat.shiftRight_eq_div_pow"
		}
		simps += maskLemma(s)
		pw.p("theorem rd_%s (w : Wire) : (%s).toNat = %s := by\n%s  simp [%s] <;> omega\n",
			s.Name, readExpr(s), wireMember(s, "w"), byteFacts(s.Word, "  "), simps)
	}
	pw.p("\n/-! Header values stored by parse. -/\n\n")
	for _, s := range f.Fields {
		t := leanType(s.Type)
		pw.p("theorem hd_%s (w : Wire) (x : %s) : %s = x ↔ x.toNat = %s := by\n", s.Name, t, headerValue(s), wireMember(s, "w"))
		pw.p("  have e : (%s).toNat = %s := by\n", headerValue(s), wireMember(s, "w"))
		switch {
		case s.Type == 8:
			pw.p("%s    rw [UInt32.toNat_toUInt8, rd_%s]; omega\n", byteFacts(s.Word, "    "), s.Name)
		case s.Type == 16 && s.Whole():
			pw.p("    exact u16_toNat _ _\n")
		case s.Type == 16:
			pw.p("    rw [to16_toNat _ (by rw [rd_%s]; omega), rd_%s]\n", s.Name, s.Name)
		default:
			pw.p("    exact rd_%s w\n", s.Name)
		}
		pw.p("  rw [← %s.toNat_inj, e, eq_comm]\n", t)
	}
	for _, s := range f.Subs {
		if s.Enum == nil {
			continue
		}
		d := s.Word.Wide()
		var alts []string
		for _, v := range s.Enum {
			alts = append(alts, fmt.Sprintf("v.toNat = %d", v))
		}
		pw.p("\ntheorem %s_iff (v : %s) : %s v = true ↔ %s := by\n  unfold %s\n", allowedName(s), d, allowedName(s),
			strings.Join(alts, " ∨ "), allowedName(s))
		var hs []string
		for i, v := range s.Enum {
			pw.p("  by_cases e%d : v.toNat = %d\n", i, v)
			pw.p("  · rw [ite_eq_left (%s.toNat_inj.mp e%d)]; simp [e%d]\n", d, i, i)
			pw.p("  rw [ite_eq_right (fun e => e%d (%s.toNat_inj.mpr e))]\n", i, d)
			hs = append(hs, fmt.Sprintf("e%d", i))
		}
		pw.p("  simp [%s]\n", strings.Join(hs, ", "))
	}
	pw.p("\n/-! Runtime conditions. -/\n\n")
	for i, c := range f.Parse {
		s, d := c.Sub, c.Sub.Word.Wide()
		pw.p("theorem cond_%d (w : Wire) : %s ↔ %s := by\n", i, parseRuntimeCond(c), parseCond(c, "w"))
		switch c.Kind {
		case CheckConst, CheckReserved:
			pw.p("  rw [← %s.toNat_inj, rd_%s] <;> rfl\n", d, s.Name)
		case CheckEnum:
			pw.p("  rw [%s_iff, rd_%s]\n", allowedName(s), s.Name)
		default:
			pw.p("  rw [%s.le_iff_toNat_le, rd_%s] <;> rfl\n", d, s.Name)
		}
	}
	for j, c := range f.Valid {
		s, d := c.Sub, c.Sub.Word.Wide()
		widen := ""
		if s.Type < 32 {
			widen = fmt.Sprintf(", %s.toNat_toUInt32", leanType(s.Type))
		}
		pw.p("theorem vcond_%d (h : Header) : %s ↔ %s := by\n", j, validRuntimeCond(c), validCond(c, "h"))
		switch c.Kind {
		case CheckWidth:
			pw.p("  rw [%s.lt_iff_toNat_lt%s] <;> rfl\n", d, widen)
		case CheckEnum:
			pw.p("  rw [%s_iff%s]\n", allowedName(s), widen)
		default:
			pw.p("  rw [%s.le_iff_toNat_le%s] <;> rfl\n", d, widen)
		}
	}
}

// serValue is the natural-number value member j of a word contributes when
// serializing h: the field, the constant, or zero.
func serValue(s *Sub) string {
	switch s.Role {
	case RoleConst:
		return fmt.Sprint(s.Const)
	case RoleReserved:
		return "0"
	}
	return fieldNat(s, "h")
}

func serValues(w *Word) []string {
	out := make([]string, len(w.Subs))
	for j, s := range w.Subs {
		out[j] = serValue(s)
	}
	return out
}

func narrowFields(w *Word) []*Sub {
	var out []*Sub
	for _, s := range w.Subs {
		if s.Narrow() {
			out = append(out, s)
		}
	}
	return out
}

func (pw *proofWriter) serLemmas() {
	f := pw.f
	pw.p("\n/-! Serialization, byte by byte. -/\n\n")
	for _, w := range f.Words {
		if !w.Group {
			continue
		}
		var hyps, haves []string
		for _, s := range narrowFields(w) {
			hyps = append(hyps, fmt.Sprintf("(r_%s : %s < %d)", s.Name, fieldNat(s, "h"), s.Modulus()))
		}
		for _, s := range w.Subs {
			if s.Role == RoleField {
				haves = append(haves, fmt.Sprintf("have := %s h.%s", boundLemma(s.Type), s.Name))
			}
		}
		pw.p("theorem sw%d_toNat (h : Header) %s :\n    (%s).toNat = %s := by\n", w.Index, strings.Join(hyps, " "),
			wordCall(w), packed(w, serValues(w)))
		for _, hv := range haves {
			pw.p("  %s\n", hv)
		}
		pw.p("  simp [%s] <;> omega\n", wordName(w))
	}
	for _, w := range f.Words {
		pk := packed(w, serValues(w))
		var hyps, args []string
		if w.Group {
			for _, s := range narrowFields(w) {
				hyps = append(hyps, fmt.Sprintf("(r_%s : %s < %d)", s.Name, fieldNat(s, "h"), s.Modulus()))
				args = append(args, "r_"+s.Name)
			}
		}
		for p, i := range w.Order {
			pw.p("theorem ser_b%d (h : Header) %s :\n    (serialize h).b%d.toNat = %s := by\n", i, strings.Join(hyps, " "),
				i, byteOf(pk, false, w.Size, p))
			if w.Group {
				shift := ""
				if w.Size-1-p != 0 {
					shift = ", UInt32.toNat_shiftRight"
				}
				pw.p("  simp only [serialize]\n  rw [UInt32.toNat_toUInt8%s, sw%d_toNat h %s] <;> simp [Nat.shiftRight_eq_div_pow]\n",
					shift, w.Index, strings.Join(args, " "))
				continue
			}
			s := w.Subs[0]
			if s.Role == RoleField {
				pw.p("  have := %s h.%s\n", boundLemma(s.Type), s.Name)
			}
			pw.p("  simp [serialize, Nat.shiftRight_eq_div_pow] <;> omega\n")
		}
	}
}

// chain writes the linear case analysis over the runtime checks of parse or
// validate. leaf(k) closes the goal when check k fails; pos closes it when
// all pass.
func (pw *proofWriter) chain(fn, arg, cond string, props []string, leaf func(k int) string, pos []string) {
	pw.p("  unfold %s\n", fn)
	for k, prop := range props {
		pw.p("  by_cases c%d : %s\n", k, prop)
		pw.p("  case neg =>\n    rw [ite_eq_right (fun e => c%d ((%s_%d %s).mp e))]\n    %s\n", k, cond, k, arg, leaf(k))
		pw.p("  rw [ite_eq_left ((%s_%d %s).mpr c%d)]\n", cond, k, arg, k)
	}
	for _, l := range pos {
		pw.p("  %s\n", l)
	}
}

func cs(n int) []string { return vars("c", n) }

func (pw *proofWriter) theorem(c Claim, body func()) {
	pw.p("\ntheorem %s %s", c.Name, c.Binders)
	if c.Premise != "" {
		pw.p(" (%s : %s)", c.Hyp, c.Premise)
	}
	pw.p(" :\n    %s := by\n", c.Body(""))
	body()
}

func (pw *proofWriter) claims() {
	f := pw.f
	claims := map[string]Claim{}
	for _, c := range f.Claims() {
		claims[c.Name] = c
	}
	np, nv := len(f.Parse), len(f.Valid)
	pconds := parseConds(f, "w")
	vconds := validConds(f, "h")
	var hds []string
	for _, s := range f.Fields {
		hds = append(hds, "hd_"+s.Name)
	}

	// parse_ok_iff
	pw.theorem(claims["parse_ok_iff"], func() {
		pos := []string{"cases h", fmt.Sprintf("rw [ParseResult.ok.injEq, Header.mk.injEq, %s]", strings.Join(hds, ", "))}
		if np == 0 {
			pos[1] += " <;> exact Iff.rfl"
		} else {
			pos = append(pos, fmt.Sprintf("exact ⟨fun hd => ⟨%s, hd⟩, fun hh => hh.2⟩", tuple(cs(np))))
		}
		pw.chain("parse", "w", "cond", pconds, func(k int) string {
			return fmt.Sprintf("exact ⟨fun e => ParseResult.noConfusion e, fun hh => absurd %s c%d⟩", proj("hh.1", k, np), k)
		}, pos)
	})
	for i, c := range f.Parse {
		pw.theorem(claims["parse_"+c.Reason+"_iff"], func() {
			other := fmt.Sprintf("exact ⟨fun e => ParseResult.noConfusion e, fun hh => absurd c%d %s⟩", i, proj("hh", i, i+1))
			pw.chain("parse", "w", "cond", pconds, func(k int) string {
				switch {
				case k < i:
					return fmt.Sprintf("exact ⟨fun e => ParseResult.noConfusion e, fun hh => absurd %s c%d⟩", proj("hh", k, i+1), k)
				case k == i:
					return fmt.Sprintf("exact ⟨fun _ => %s, fun _ => rfl⟩", tuple(cs(i+1)))
				}
				return other
			}, []string{other})
		})
	}
	if np == 0 {
		pw.theorem(claims["accepts"], func() { pw.p("  exact ⟨_, rfl⟩\n") })
	} else {
		pw.theorem(claims["accepts_iff"], func() {
			pw.p("  constructor\n  · rintro ⟨h, hp⟩\n    exact ((parse_ok_iff w h).mp hp).1\n  · intro hc\n")
			pw.p("    cases hp : parse w with\n    | ok h => exact ⟨h, rfl⟩\n")
			for i, c := range f.Parse {
				pw.p("    | %s => exact absurd %s %s\n", c.Reason, proj("hc", i, np),
					strings.TrimSuffix(fmt.Sprintf("((parse_%s_iff w).mp hp)%s", c.Reason, strings.Repeat(".2", i)), ""))
			}
		})
	}
	if nv == 0 {
		pw.theorem(claims["validate_valid"], func() { pw.p("  rfl\n") })
	} else {
		pw.theorem(claims["validate_valid_iff"], func() {
			pw.chain("validate", "h", "vcond", vconds, func(k int) string {
				return fmt.Sprintf("exact ⟨fun e => Validity.noConfusion e, fun hh => absurd %s c%d⟩", proj("hh", k, nv), k)
			}, []string{fmt.Sprintf("exact ⟨fun _ => %s, fun _ => rfl⟩", tuple(cs(nv)))})
		})
	}
	for j, c := range f.Valid {
		pw.theorem(claims["validate_"+c.Reason+"_iff"], func() {
			other := fmt.Sprintf("exact ⟨fun e => Validity.noConfusion e, fun hh => absurd c%d %s⟩", j, proj("hh", j, j+1))
			pw.chain("validate", "h", "vcond", vconds, func(k int) string {
				switch {
				case k < j:
					return fmt.Sprintf("exact ⟨fun e => Validity.noConfusion e, fun hh => absurd %s c%d⟩", proj("hh", k, j+1), k)
				case k == j:
					return fmt.Sprintf("exact ⟨fun _ => %s, fun _ => rfl⟩", tuple(cs(j+1)))
				}
				return other
			}, []string{other})
		})
	}
	pw.roundTrips(claims)
	if f.Payload != nil {
		pw.payload(claims)
	}
}

// fieldIndex returns the position of a member among Header fields.
func (f *Format) fieldIndex(s *Sub) int {
	for i, o := range f.Fields {
		if o == s {
			return i
		}
	}
	return -1
}

func checkIndex(list []*Check, s *Sub, k CheckKind) int {
	for i, c := range list {
		if c.Sub == s && c.Kind == k {
			return i
		}
	}
	return -1
}

func memberIndex(s *Sub) int {
	for j, o := range s.Word.Subs {
		if o == s {
			return j
		}
	}
	return -1
}

func (pw *proofWriter) roundTrips(claims map[string]Claim) {
	f := pw.f
	np, nv, nf := len(f.Parse), len(f.Valid), len(f.Fields)
	pw.p("\ntheorem wire_ext (a b : Wire)")
	for i := range f.Size {
		pw.p(" (e%d : a.b%d.toNat = b.b%d.toNat)", i, i, i)
	}
	pw.p(" : a = b := by\n  cases a; cases b\n  simp only [Wire.mk.injEq]\n")
	var injs []string
	for i := range f.Size {
		injs = append(injs, fmt.Sprintf("UInt8.toNat_inj.mp e%d", i))
	}
	pw.p("  exact %s\n", tuple(injs))

	// parse_serialize
	pw.theorem(claims["parse_serialize"], func() {
		if nv > 0 {
			pw.p("  %s := hv\n", destruct(tuple(vars("v", nv))))
		}
		for _, s := range f.Fields {
			pw.p("  have q_%s := %s h.%s\n", s.Name, boundLemma(s.Type), s.Name)
			if s.Narrow() {
				pw.p("  have r_%s : %s < %d := by omega\n", s.Name, fieldNat(s, "h"), s.Modulus())
			}
		}
		pw.p("  apply (parse_ok_iff (serialize h) h).mpr\n")
		var rws []string
		for _, w := range f.Words {
			var args []string
			if w.Group {
				for _, s := range narrowFields(w) {
					args = append(args, "r_"+s.Name)
				}
			}
			for _, i := range w.Order {
				rws = append(rws, strings.TrimSpace(fmt.Sprintf("ser_b%d h %s", i, strings.Join(args, " "))))
			}
		}
		pw.p("  rw [%s]\n", strings.Join(rws, ", "))
		for _, w := range f.Words {
			var rangeArgs []string
			for _, s := range w.Subs {
				switch {
				case s.Role != RoleField:
					rangeArgs = append(rangeArgs, "(by decide)")
				case s.Narrow():
					rangeArgs = append(rangeArgs, "r_"+s.Name)
				default:
					rangeArgs = append(rangeArgs, "q_"+s.Name)
				}
			}
			vals := serValues(w)
			for j := range vals {
				if strings.Contains(vals[j], " ") {
					vals[j] = "(" + vals[j] + ")"
				}
			}
			pw.p("  have t%d := w%d_rt %s %s\n", w.Index, w.Index, strings.Join(vals, " "), strings.Join(rangeArgs, " "))
		}
		tp := func(s *Sub) string {
			return proj(fmt.Sprintf("t%d", s.Word.Index), memberIndex(s), len(s.Word.Subs))
		}
		var cproofs, dproofs []string
		for _, c := range f.Parse {
			switch c.Kind {
			case CheckConst, CheckReserved:
				cproofs = append(cproofs, tp(c.Sub))
			default:
				cproofs = append(cproofs, fmt.Sprintf("(by rw [%s]; exact v%d)", tp(c.Sub), checkIndex(f.Valid, c.Sub, c.Kind)))
			}
		}
		for _, s := range f.Fields {
			dproofs = append(dproofs, fmt.Sprintf("(%s).symm", tp(s)))
		}
		if np == 0 {
			pw.p("  exact %s\n", tuple(dproofs))
		} else {
			pw.p("  exact ⟨%s, %s⟩\n", tuple(cproofs), tuple(dproofs))
		}
	})

	// parse_inj
	pattern := func(c, d string) string {
		if np == 0 {
			return tuple(vars(d, nf))
		}
		return fmt.Sprintf("⟨%s, %s⟩", tuple(vars(c, np)), tuple(vars(d, nf)))
	}
	pw.p("\ntheorem parse_inj (a b : Wire) (h : Header) (ha : parse a = .ok h) (hb : parse b = .ok h) : a = b := by\n")
	pw.p("  %s := (parse_ok_iff a h).mp ha\n", destruct(pattern("ca", "da")))
	pw.p("  %s := (parse_ok_iff b h).mp hb\n", destruct(pattern("cb", "db")))
	for _, w := range f.Words {
		var as, bs, lts, fs []string
		for _, i := range w.Order {
			as = append(as, fmt.Sprintf("a.b%d.toNat", i))
			bs = append(bs, fmt.Sprintf("b.b%d.toNat", i))
			lts = append(lts, "(u8_lt _)")
		}
		for _, s := range w.Subs {
			if s.Role == RoleField {
				i := f.fieldIndex(s)
				fs = append(fs, fmt.Sprintf("(da%d.symm.trans db%d)", i, i))
			} else {
				kind := CheckConst
				if s.Role == RoleReserved {
					kind = CheckReserved
				}
				i := checkIndex(f.Parse, s, kind)
				fs = append(fs, fmt.Sprintf("(ca%d.trans cb%d.symm)", i, i))
			}
		}
		pw.p("  have k%d := w%d_inj %s %s %s %s\n", w.Index, w.Index, strings.Join(as, " "), strings.Join(bs, " "),
			strings.Join(append(lts, lts...), " "), strings.Join(fs, " "))
	}
	var es []string
	for i := range f.Size {
		for _, w := range f.Words {
			for p, o := range w.Order {
				if o == i {
					es = append(es, proj(fmt.Sprintf("k%d", w.Index), p, w.Size))
				}
			}
		}
	}
	pw.p("  exact wire_ext a b %s\n", strings.Join(es, " "))

	if nv > 0 {
		vc := validConds(f, "h")
		pw.p("\ntheorem parsed_valid (w : Wire) (h : Header) (hp : parse w = .ok h) :\n    %s := by\n", lines(vc, "    "))
		pw.p("  %s := (parse_ok_iff w h).mp hp\n", destruct(pattern("c", "d")))
		var parts []string
		for _, c := range f.Valid {
			d := fmt.Sprintf("d%d", f.fieldIndex(c.Sub))
			if c.Kind == CheckWidth {
				parts = append(parts, fmt.Sprintf("(by rw [%s]; exact Nat.mod_lt _ (by decide))", d))
			} else {
				parts = append(parts, fmt.Sprintf("(by rw [%s]; exact c%d)", d, checkIndex(f.Parse, c.Sub, c.Kind)))
			}
		}
		pw.p("  exact %s\n", tuple(parts))
	}
	valid := "(parsed_valid w h hp)"
	if nv == 0 {
		valid = ""
	}
	pw.theorem(claims["serialize_parse"], func() {
		pw.p("  exact parse_inj (serialize h) w h (parse_serialize h %s) hp\n", valid)
	})
	pw.theorem(claims["parse_validate"], func() {
		if nv == 0 {
			pw.p("  exact validate_valid h\n")
		} else {
			pw.p("  exact (validate_valid_iff h).mpr %s\n", valid)
		}
	})
}

func (pw *proofWriter) payload(claims map[string]Claim) {
	f := pw.f
	s := f.Payload
	l := payloadNat(f, "h")
	total := widened(s, "h")
	widen := ""
	if s.Type < 32 {
		widen = fmt.Sprintf("%s.toNat_toUInt32", leanType(s.Type))
	}
	pw.p("\ntheorem payloadLength_toNat (h : Header) : (payloadLength h.%s).toNat = %s := by\n  unfold payloadLength\n", s.Name, l)
	if f.HeaderBytes == 0 {
		if widen != "" {
			pw.p("  rw [%s]\n", widen)
		} else {
			pw.p("  rfl\n")
		}
	} else {
		n := f.HeaderBytes
		pw.p("  by_cases hl : %s < %d\n", total, n)
		pw.p("  · rw [ite_eq_left hl]\n    have hn : (%s).toNat < %d := hl\n", total, n)
		if widen != "" {
			pw.p("    rw [%s] at hn\n", widen)
		}
		pw.p("    simp only [UInt32.reduceToNat]; omega\n")
		pw.p("  · rw [ite_eq_right hl]\n    have hn : ¬ (%s).toNat < %d := hl\n", total, n)
		pw.p("    have le : (%d : UInt32) ≤ %s := by\n      rw [UInt32.le_iff_toNat_le]; exact Nat.le_of_not_gt hn\n", n, total)
		rewrites := "UInt32.toNat_sub_of_le _ _ le"
		if widen != "" {
			rewrites += ", " + widen
		}
		pw.p("    rw [%s] <;> rfl\n", rewrites)
	}
	pw.theorem(claims["payload_need_more_iff"], func() {
		pw.p("  have hl := payloadLength_toNat h\n  unfold classify\n")
		pw.p("  by_cases short : available < payloadLength h.%s\n", s.Name)
		pw.p("  · have lt : available.toNat < (payloadLength h.%s).toNat := short\n", s.Name)
		pw.p("    rw [ite_eq_left short, Availability.needMore.injEq, ← UInt32.toNat_inj,\n")
		pw.p("      UInt32.toNat_sub_of_le _ _ (UInt32.le_of_lt short)]\n")
		pw.p("    rw [hl] at lt ⊢\n    constructor\n    · intro e; exact ⟨lt, e.symm⟩\n    · intro e; exact e.2.symm\n")
		pw.p("  · have ge : ¬ available.toNat < (payloadLength h.%s).toNat := short\n", s.Name)
		pw.p("    rw [ite_eq_right short]\n    rw [hl] at ge\n")
		pw.p("    exact ⟨fun e => Availability.noConfusion e, fun hh => absurd hh.1 ge⟩\n")
	})
	pw.theorem(claims["payload_complete_iff"], func() {
		pw.p("  have hl := payloadLength_toNat h\n  unfold classify\n")
		pw.p("  by_cases short : available < payloadLength h.%s\n", s.Name)
		pw.p("  · have lt : available.toNat < (payloadLength h.%s).toNat := short\n", s.Name)
		pw.p("    rw [ite_eq_left short]\n    rw [hl] at lt\n")
		pw.p("    exact ⟨fun e => Availability.noConfusion e, fun hh => absurd hh.1 (Nat.not_le_of_lt lt)⟩\n")
		pw.p("  · have ge : ¬ available.toNat < (payloadLength h.%s).toNat := short\n", s.Name)
		pw.p("    rw [ite_eq_right short, Availability.complete.injEq, ← UInt32.toNat_inj]\n")
		pw.p("    rw [hl] at ge ⊢\n    constructor\n    · intro e; exact ⟨Nat.le_of_not_gt ge, e.symm⟩\n    · intro e; exact e.2.symm\n")
	})
}

// ProofsLean renders proof/<Module>Proofs.lean.
func (f *Format) ProofsLean() string {
	pw := &proofWriter{f: f}
	pw.p("-- Code generated by lean-go-format from %s.json; DO NOT EDIT.\n", f.Name)
	pw.p("-- Proofs of %sContract. Every step below is produced by the generator's\n", f.Module)
	pw.p("-- fixed scheme; nothing is tuned per format.\n")
	pw.p("import %s\nimport Init.Data.UInt.Bitwise\n\nset_option linter.unusedVariables false\nset_option linter.unusedSimpArgs false\nset_option linter.unnecessarySimpa false\n\n", f.Module)
	pw.p("namespace %sProofs\nopen %s\n\n", f.Module, f.Module)
	pw.p("%s", proofLibrary)
	pw.masks()
	for _, w := range f.Words {
		pw.wordLemmas(w)
	}
	pw.memberLemmas()
	pw.serLemmas()
	pw.claims()
	pw.p("\nend %sProofs\n", f.Module)
	return pw.b.String()
}

// destruct binds a pattern: obtain for tuples, have for a single name.
func destruct(pattern string) string {
	if strings.HasPrefix(pattern, "⟨") {
		return "obtain " + pattern
	}
	return "have " + pattern
}
