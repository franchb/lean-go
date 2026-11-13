import Lean
open Lean.Compiler.LCNF
@[cpass] meta def noOpPass : PassInstaller where
  phase := .base
  install passes := pure passes
def run (x : UInt32) : UInt32 := x + 1
