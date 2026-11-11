import LeanGo.Pilot.Export
run_cmd Lean.Elab.Command.liftTermElabM do
  let result ← Pilot.exportProgram
  IO.FS.writeFile "ir.json" result.compress
