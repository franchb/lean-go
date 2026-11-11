import LeanGo.Feasibility.ProbeSupport

namespace BadClaim

axiom untrusted : False

theorem advertised : False := untrusted

end BadClaim

open Lean Elab Command

run_cmd do
  liftCoreM <| Probe.auditClaim ``BadClaim.advertised
