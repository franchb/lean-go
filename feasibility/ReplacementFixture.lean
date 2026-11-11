import LeanGo.Feasibility.ProbeSupport

namespace ReplacementFixture

def replacement (_ : UInt32) : UInt32 := 0

@[implemented_by replacement]
def root (value : UInt32) : UInt32 := value

theorem logical_identity (value : UInt32) : root value = value := rfl

end ReplacementFixture

open Lean Elab Command
open Lean.Compiler.LCNF

run_cmd do
  liftCoreM do
    Probe.sourceClosure [``ReplacementFixture.root]
    Probe.reportClosure "base" getBaseDecl? [``ReplacementFixture.root]
    Probe.reportClosure "mono" getMonoDecl? [``ReplacementFixture.root]
