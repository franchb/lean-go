package wireformat

import (
	"fmt"
	"strings"
)

// Claim is one theorem of the proof contract.
type Claim struct {
	Name    string
	Binders string                     // theorem binders, e.g. "(w : Wire) (h : Header)"
	Forall  string                     // contract quantifier prefix, e.g. "∀ (w : Wire) (h : Header),"
	Body    func(prefix string) string // statement; prefix qualifies runtime functions
	Hyp     string                     // optional hypothesis name for an implication's premise
	Premise string                     // optional premise (contract: `premise →`; theorem: binder)
	Doc     string
}

func lines(items []string, indent string) string {
	return strings.Join(items, " ∧\n"+indent)
}

func group(items []string, indent string) string {
	if len(items) == 1 {
		return items[0]
	}
	return "(" + lines(items, indent+" ") + ")"
}

// Claims lists the contract in a fixed order.
func (f *Format) Claims() []Claim {
	var out []Claim
	cs, ds := parseConds(f, "w"), decodedFields(f, "w", "h")
	vs := validConds(f, "h")
	out = append(out, Claim{
		Name: "parse_ok_iff", Binders: "(w : Wire) (h : Header)", Forall: "∀ (w : Wire) (h : Header),",
		Doc: "A successful parse is exactly: every check holds and every field is the stated function of the bytes.",
		Body: func(p string) string {
			if len(cs) == 0 {
				return fmt.Sprintf("%sparse w = .ok h ↔\n      %s", p, lines(ds, "      "))
			}
			return fmt.Sprintf("%sparse w = .ok h ↔\n      %s ∧\n      %s", p, group(cs, "      "), group(ds, "      "))
		},
	})
	for i, c := range f.Parse {
		items := append(append([]string{}, cs[:i]...), "¬"+parseCond(c, "w"))
		out = append(out, Claim{
			Name: "parse_" + c.Reason + "_iff", Binders: "(w : Wire)", Forall: "∀ (w : Wire),",
			Doc: fmt.Sprintf("parse reports %s exactly when the earlier checks pass and this one fails.", c.Reason),
			Body: func(p string) string {
				return fmt.Sprintf("%sparse w = .%s ↔\n      %s", p, c.Reason, lines(items, "      "))
			},
		})
	}
	if len(cs) == 0 {
		out = append(out, Claim{Name: "accepts", Binders: "(w : Wire)", Forall: "∀ (w : Wire),",
			Doc:  "Every byte string of the header size is accepted.",
			Body: func(p string) string { return fmt.Sprintf("∃ h, %sparse w = .ok h", p) }})
	} else {
		out = append(out, Claim{Name: "accepts_iff", Binders: "(w : Wire)", Forall: "∀ (w : Wire),",
			Doc: "parse accepts exactly the byte strings on which every declared constraint holds.",
			Body: func(p string) string {
				return fmt.Sprintf("(∃ h, %sparse w = .ok h) ↔\n      %s", p, lines(cs, "      "))
			}})
	}
	if len(vs) == 0 {
		out = append(out, Claim{Name: "validate_valid", Binders: "(h : Header)", Forall: "∀ (h : Header),",
			Doc:  "Every header value is valid.",
			Body: func(p string) string { return fmt.Sprintf("%svalidate h = .valid", p) }})
	} else {
		out = append(out, Claim{Name: "validate_valid_iff", Binders: "(h : Header)", Forall: "∀ (h : Header),",
			Doc: "The validity predicate: every field fits its width and satisfies its declared constraints.",
			Body: func(p string) string {
				return fmt.Sprintf("%svalidate h = .valid ↔\n      %s", p, lines(vs, "      "))
			}})
	}
	for j, c := range f.Valid {
		items := append(append([]string{}, vs[:j]...), "¬"+validCond(c, "h"))
		out = append(out, Claim{
			Name: "validate_" + c.Reason + "_iff", Binders: "(h : Header)", Forall: "∀ (h : Header),",
			Doc: fmt.Sprintf("validate reports %s exactly when the earlier checks pass and this one fails.", c.Reason),
			Body: func(p string) string {
				return fmt.Sprintf("%svalidate h = .%s ↔\n      %s", p, c.Reason, lines(items, "      "))
			},
		})
	}
	ps := Claim{Name: "parse_serialize", Binders: "(h : Header)", Forall: "∀ (h : Header),",
		Doc:  "Serializing a valid header and parsing the bytes returns the header.",
		Body: func(p string) string { return fmt.Sprintf("%sparse (%sserialize h) = .ok h", p, p) }}
	if len(vs) != 0 {
		ps.Hyp, ps.Premise = "hv", lines(vs, "      ")
	}
	out = append(out, ps)
	out = append(out, Claim{Name: "serialize_parse", Binders: "(w : Wire) (h : Header)", Forall: "∀ (w : Wire) (h : Header),",
		Doc: "Accepted bytes are the serialization of the header they parse to: encodings are unique.",
		Hyp: "hp", Premise: "parse w = .ok h",
		Body: func(p string) string { return fmt.Sprintf("%sserialize h = w", p) }})
	out = append(out, Claim{Name: "parse_validate", Binders: "(w : Wire) (h : Header)", Forall: "∀ (w : Wire) (h : Header),",
		Doc: "Every parsed header is valid.", Hyp: "hp", Premise: "parse w = .ok h",
		Body: func(p string) string { return fmt.Sprintf("%svalidate h = .valid", p) }})
	if f.Payload != nil {
		l := payloadNat(f, "h")
		out = append(out, Claim{Name: "payload_need_more_iff",
			Binders: "(h : Header) (available missing : UInt32)", Forall: "∀ (h : Header) (available missing : UInt32),",
			Doc: "With fewer payload bytes than declared, classify reports the exact deficit.",
			Body: func(p string) string {
				return fmt.Sprintf("%sclassify h available = .needMore missing ↔\n      available.toNat < %s ∧ missing.toNat = %s - available.toNat", p, l, l)
			}})
		out = append(out, Claim{Name: "payload_complete_iff",
			Binders: "(h : Header) (available consumed : UInt32)", Forall: "∀ (h : Header) (available consumed : UInt32),",
			Doc: "With enough payload bytes, classify reports exactly the declared count to consume.",
			Body: func(p string) string {
				return fmt.Sprintf("%sclassify h available = .complete consumed ↔\n      %s ≤ available.toNat ∧ consumed.toNat = %s", p, l, l)
			}})
	}
	return out
}

// premise renders a claim's premise in the contract, qualified.
func (c Claim) contractPremise(prefix string) string {
	if c.Premise == "parse w = .ok h" {
		return prefix + c.Premise
	}
	return c.Premise
}

// ContractLean renders proof/<Module>Contract.lean.
func (f *Format) ContractLean() string {
	var b strings.Builder
	p := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	p("-- Code generated by lean-go-format from the %s spec; DO NOT EDIT.\n", f.Name)
	p("-- Expected propositions, stated from the format spec independently of the proofs.\n")
	p("-- Byte b<i> is wire byte i; multi-byte values are written out in base 256.\n")
	p("import %s\n\nnamespace %sContract\n", f.Module, f.Module)
	pre := f.Module + "."
	for _, c := range f.Claims() {
		p("\n/-- %s -/\ndef %s : Prop :=\n  %s\n", c.Doc, c.Name, c.Forall)
		if c.Premise != "" {
			p("    %s →\n", c.contractPremise(pre))
		}
		p("    %s\n", c.Body(pre))
	}
	p("\nend %sContract\n", f.Module)
	return b.String()
}

// ContractJSON renders proof-contract.json.
func (f *Format) ContractJSON() string {
	var b strings.Builder
	fmt.Fprintf(&b, "{\n  \"schema\": \"lean-go-proof-contract/v1\",\n  \"source_dir\": \"source\",\n  \"proof_dir\": \"proof\",\n")
	fmt.Fprintf(&b, "  \"module\": %q,\n  \"roots\": [\n", f.Module)
	roots := f.Roots()
	for i, r := range roots {
		fmt.Fprintf(&b, "    %q", r)
		if i < len(roots)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("  ],\n  \"claims\": [\n")
	claims := f.Claims()
	for i, c := range claims {
		fmt.Fprintf(&b, "    {\n      \"theorem\": \"%sProofs.%s\",\n      \"expected\": \"%sContract.%s\"\n    }", f.Module, c.Name, f.Module, c.Name)
		if i < len(claims)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("  ]\n}\n")
	return b.String()
}
