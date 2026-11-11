import LeanGo.Feasibility.ProbeSupport

namespace ForeignFixture

@[extern "lean_go_missing_foreign"]
opaque foreign : UInt32 → UInt32

def root (value : UInt32) : UInt32 := foreign value

end ForeignFixture

open Lean Elab Command
open Lean.Compiler.LCNF

run_cmd do
  liftCoreM do
    Probe.sourceClosure [``ForeignFixture.root]
    Probe.reportClosure "base" getBaseDecl? [``ForeignFixture.root]
    Probe.reportClosure "mono" getMonoDecl? [``ForeignFixture.root]
