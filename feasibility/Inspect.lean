import LeanGo.Feasibility.ProbeSupport
import LeanGo.Feasibility.Model

-- PROBE ACTIONS
set_option trace.Compiler.saveBase false
set_option trace.Compiler.saveMono false

open Lean Elab Command
open Lean.Compiler.LCNF

run_cmd do
  liftCoreM do
    Probe.auditClaim ``LeanGo.Feasibility.normalize_preserves_eval
    Probe.auditClaim ``LeanGo.Feasibility.normalize_is_normal
    Probe.sourceClosure [``LeanGo.Feasibility.normalize]
    Probe.captureRoot ``LeanGo.Feasibility.normalize

run_cmd do
  for name in [``LeanGo.Feasibility.Tri, ``LeanGo.Feasibility.Expr] do
    let info ← getConstInfoInduct name
    logInfo m!"TYPE {info.name} parameters={info.numParams}"
    for ctorName in info.ctors do
      let ctor ← getConstInfoCtor ctorName
      logInfo m!"CTOR {ctor.name} tag={ctor.cidx} parameters={ctor.numParams} fields={ctor.numFields}"
      logInfo m!"CTOR_TYPE {ctor.type}"
