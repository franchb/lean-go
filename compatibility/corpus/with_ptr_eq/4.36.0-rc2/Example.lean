unsafe def run (a : Bool) : Bool :=
  withPtrEq a true (fun _ => a) (fun h => h)
