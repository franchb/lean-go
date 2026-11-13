import Lean
import Init.Data.UInt.Package
open Lean in
run_cmd Lean.Elab.Command.liftCoreM do
  let env := (← getEnv).setExporting false
  for name in ["_private.Init.Data.UInt.Package.0.instLinearOrderPackageUInt32", "instLinearOrderPackageUInt32"] do
    let kind := match env.find? name.toName with
      | some (.defnInfo _) => "definition"
      | some _ => "other constant"
      | none => "missing"
    logInfo m!"{name}: {kind}"
