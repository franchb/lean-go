package pipeline

import "testing"

// Lean-gated (LEAN_GO_MODULES=1) port of the retired scripts/test_module_admission.py:
// every rejected runtime boundary preserves existing output and is never an
// elaboration failure. TestAdmissionDiagnosticsMatchRecordedPythonReports checks the
// same programs' complete reports against those recorded from the retired
// Python pipeline.

func TestRejectedRuntimeBoundariesPreserveOutput(t *testing.T) {
	requireModules(t)
	cases := map[string][2]string{
		"custom_compiler_pass": {"open Lean.Compiler.LCNF\n@[cpass] meta def noOpPass : PassInstaller where\n  phase := .base\n  install passes := pure passes\ndef run (b : Bool) : Bool := b\n", "[Cc]ustom compiler passes"},
		"late_compiler_pass":   {"def run (b : Bool) : Bool := b\nopen Lean.Compiler.LCNF\n@[cpass] meta def noOpPass : PassInstaller where\n  phase := .base\n  install passes := pure passes\n", "[Cc]ustom compiler passes"},
		"extern":               {"@[extern \"lean_go_untrusted\"] opaque impl : Bool → Bool\ndef run (x : Bool) : Bool := impl x\n", "External function body|extern|opaque"},
		"late_extern":          {"def run (x : Bool) : Bool := x\nattribute [extern \"lean_go_untrusted\"] run\n", "extern"},
		"implemented_by":       {"def impl (_ : Bool) : Bool := false\n@[implemented_by impl] def original (_ : Bool) : Bool := true\ndef run (x : Bool) : Bool := original x\n", "implemented_by"},
		"late_implemented_by":  {"def replacement (_ : Bool) : Bool := false\ndef run (x : Bool) : Bool := x\nattribute [implemented_by replacement] run\n", "implemented_by"},
		"late_csimp":           {"def run (x : Bool) : Bool := x\ndef replacement (_ : Bool) : Bool := false\ntheorem forged : run = replacement := by sorry\nattribute [csimp] forged\n", "rewrite|axiom|sorry"},
		"unsafe":               {"unsafe def run (_ : Bool) : Bool := true\n", "safety mismatch|unsafe"},
		"partial":              {"partial def run (x : Bool) : Bool := run x\n", "safety mismatch|unsafe|partial|recursive implementation"},
		"recursive_override": {"@[noinline] def f (_ : Bool) : Bool := true\npartial def f._unsafe_rec (b : Bool) : Bool :=\n  match b with | true => false | false => f._unsafe_rec true\ndef run (b : Bool) : Bool := f b\n",
			"recursive implementation|repeated compilation"},
		"polymorphic": {"def run {α : Type} (x : α) : α := x\n", "polymorph|type|Type|universe"},
		// Unsigned division is admitted; division of natural numbers is not.
		"natural_division": {"def run (x : Nat) : Nat := x / 3\n", "primitive|Primitive|Unsupported|unsupported|extern source dependency|provenance"},
		"sorry_proof":      {"theorem forged : False := by sorry\ndef run (_ : Bool) : Bool := False.elim forged\n", "axiom|proof|Unsupported|unsupported|Missing pure|missing pure"},
		"local_csimp": {"def original (_ : Bool) : Bool := true\ndef replacement (_ : Bool) : Bool := false\ntheorem forged : original = replacement := by sorry\nsection\nattribute [local csimp] forged\n@[noinline] def run (x : Bool) : Bool := original x\nend\n",
			"axiom|sorry|rewrite|proof"},
		"scoped_csimp": {"def original (_ : Bool) : Bool := true\ndef replacement (_ : Bool) : Bool := false\ntheorem forged : original = replacement := by sorry\nnamespace RewriteScope\nattribute [scoped csimp] forged\nend RewriteScope\nopen scoped RewriteScope\n@[noinline] def run (x : Bool) : Bool := original x\n",
			"axiom|sorry|rewrite|proof"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newLeanFixture(t)
			f.write("Admission.lean", "namespace Admission\n"+c[0]+"\nend Admission\n")
			f.reject("Admission", "Admission.run", c[1])
		})
	}
}
