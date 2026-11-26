import LeanGo.Module.Export
import LeanGo.Validate
import LeanGo.Validate.Command
import Examples.Producer.Local

/-! `#lean_go_validate` inside the build, so that the axiom gate
(`Examples.AxiomCheck`) also covers everything the command adds: derived
encodings, the program term, the specification list, every obligation, the
`PreFixedOn` facts and the whole-program theorems.

`Examples.Producer.Local` is `examples/modules/Local.lean` with the two lines
the export workspace injects (`import LeanGo.Module.Audit`,
`set_option compiler.postponeCompile false`), so the producer's compiler
evidence (the lifted helper `Local.run._lam_0` with its capture `config`) is
recorded exactly as in a real export. The empty paths skip the IR-file check
and the report file; `strict` turns any unproved obligation into a build
error. -/

#lean_go_validate "" [Local.run] "Local" report "" heartbeats 200000 timeout_ms 600000 strict
  namespace Examples.Command.Local

/-- The headline statement, restated by hand: the IR returns only encodings of
`Local.run`'s results. -/
example (n : Nat) (config : Local.Config) (x y : Local.Count) (v : LeanGo.Validate.Value)
    (h : Examples.Command.Local.prog.run n "Local.run"
      [LeanGo.Validate.Encode.encode config, LeanGo.Validate.Encode.encode x,
       LeanGo.Validate.Encode.encode y] = some v) :
    v = LeanGo.Validate.Encode.encode (Local.run config x y) :=
  Examples.Command.Local.explicit_0 n config x y v h
