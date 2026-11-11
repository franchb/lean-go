import LeanGo.Feasibility.ProbeSupport

open Lean Elab Command
open Lean.Compiler.LCNF

namespace CSimpFixture

@[noinline]
def original (value : Bool) : Bool := value

@[noinline]
def replacement (value : Bool) : Bool := value

@[csimp]
theorem replace_eq : @original = @replacement := rfl

def root (value : Bool) : Bool := original value

-- Inspect in the same scope as compilation; this does not audit arbitrary scoped history.
run_cmd do
  liftCoreM do
    Probe.sourceClosure [``CSimpFixture.root]
    Probe.reportClosure "base" getBaseDecl? [``CSimpFixture.root]
    Probe.reportClosure "mono" getMonoDecl? [``CSimpFixture.root]

end CSimpFixture
