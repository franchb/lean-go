import LeanGo.Feasibility.ProbeSupport

open Lean Elab Command

namespace BadCSimpFixture

@[noinline]
def original (value : Bool) : Bool := value

@[noinline]
def replacement (_ : Bool) : Bool := false

axiom forged : @original = @replacement

@[csimp]
theorem replace_eq : @original = @replacement := forged

def root (value : Bool) : Bool := original value

theorem logical_identity (value : Bool) : root value = value := rfl

-- The logical theorem passes independently; the compiler substitution must be rejected.
run_cmd do
  liftCoreM do
    Probe.auditClaim ``BadCSimpFixture.logical_identity
    Probe.sourceClosure [``BadCSimpFixture.root]

end BadCSimpFixture
