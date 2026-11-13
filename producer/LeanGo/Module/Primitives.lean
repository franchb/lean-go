module
public meta import Lean
public meta import Lean.Compiler.LCNF
public meta import Lean.Compiler.CSimpAttr
public meta import Lean.Compiler.ExternAttr
public meta import Lean.Compiler.ImplementedByAttr
public meta import Lean.Util.CollectAxioms
meta import all Lean
public meta section
open Lean Lean.Compiler
namespace LeanGo.Module

structure NumericPrimitive where
  name : Name
  result : Name
  externalSymbol : String
  operand : Name := ``UInt32
  arity : Nat := 2
  axioms : Array Name := #[]
  deriving Inhabited

-- Runtime admission is this exact table, never all extern declarations.
def numericPrimitives : Array NumericPrimitive := #[
  { name := ``UInt32.toUInt64, operand := ``UInt32, result := ``UInt64,
    externalSymbol := "lean_uint32_to_uint64", arity := 1 },
  { name := ``UInt64.toUInt32, operand := ``UInt64, result := ``UInt32,
    externalSymbol := "lean_uint64_to_uint32", arity := 1 },
  { name := ``UInt32.land, operand := ``UInt32, result := ``UInt32,
    externalSymbol := "lean_uint32_land", axioms := #[``propext, ``Quot.sound] },
  { name := ``UInt32.lor, operand := ``UInt32, result := ``UInt32,
    externalSymbol := "lean_uint32_lor", axioms := #[``propext, ``Quot.sound] },
  { name := ``UInt32.xor, operand := ``UInt32, result := ``UInt32,
    externalSymbol := "lean_uint32_xor", axioms := #[``propext, ``Quot.sound] },
  { name := ``UInt32.complement, operand := ``UInt32, result := ``UInt32,
    externalSymbol := "lean_uint32_complement", axioms := #[``propext, ``Quot.sound], arity := 1 },
  { name := ``UInt32.shiftRight, operand := ``UInt32, result := ``UInt32,
    externalSymbol := "lean_uint32_shift_right", axioms := #[``propext] },
  { name := ``UInt64.land, operand := ``UInt64, result := ``UInt64,
    externalSymbol := "lean_uint64_land", axioms := #[``propext, ``Quot.sound] },
  { name := ``UInt64.lor, operand := ``UInt64, result := ``UInt64,
    externalSymbol := "lean_uint64_lor", axioms := #[``propext, ``Quot.sound] },
  { name := ``UInt64.xor, operand := ``UInt64, result := ``UInt64,
    externalSymbol := "lean_uint64_xor", axioms := #[``propext, ``Quot.sound] },
  { name := ``UInt64.complement, operand := ``UInt64, result := ``UInt64,
    externalSymbol := "lean_uint64_complement", axioms := #[``propext, ``Quot.sound], arity := 1 },
  { name := ``UInt64.shiftRight, operand := ``UInt64, result := ``UInt64,
    externalSymbol := "lean_uint64_shift_right", axioms := #[``propext] },
  { name := ``UInt32.add, result := ``UInt32, externalSymbol := "lean_uint32_add" },
  { name := ``UInt32.sub, result := ``UInt32, externalSymbol := "lean_uint32_sub" },
  { name := ``UInt32.mul, result := ``UInt32, externalSymbol := "lean_uint32_mul" },
  { name := ``UInt32.decEq, result := ``Bool, externalSymbol := "lean_uint32_dec_eq" },
  { name := ``UInt32.decLt, result := ``Bool, externalSymbol := "lean_uint32_dec_lt" },
  { name := ``UInt32.decLe, result := ``Bool, externalSymbol := "lean_uint32_dec_le" },
  -- The pinned compiler also introduces this when multiplying by a power of two.
  { name := ``UInt32.shiftLeft, result := ``UInt32, externalSymbol := "lean_uint32_shift_left",
    axioms := #[``propext] },
  { name := ``UInt64.add, operand := ``UInt64, result := ``UInt64, externalSymbol := "lean_uint64_add" },
  { name := ``UInt64.sub, operand := ``UInt64, result := ``UInt64, externalSymbol := "lean_uint64_sub" },
  { name := ``UInt64.mul, operand := ``UInt64, result := ``UInt64, externalSymbol := "lean_uint64_mul" },
  { name := ``UInt64.decEq, operand := ``UInt64, result := ``Bool, externalSymbol := "lean_uint64_dec_eq" },
  { name := ``UInt64.decLt, operand := ``UInt64, result := ``Bool, externalSymbol := "lean_uint64_dec_lt" },
  { name := ``UInt64.decLe, operand := ``UInt64, result := ``Bool, externalSymbol := "lean_uint64_dec_le" },
  { name := ``UInt64.shiftLeft, operand := ``UInt64, result := ``UInt64,
    externalSymbol := "lean_uint64_shift_left", axioms := #[``propext] }]

def numericPrimitive? (n : Name) : Option NumericPrimitive :=
  numericPrimitives.find? (·.name == n)

def numericOrderPackage : Name :=
  ``instLinearOrderPackageUInt32

def numericWideOrderPackage : Name :=
  ``instLinearOrderPackageUInt64

-- Source-only wrappers support ordinary notation and erased decision proofs.
-- None of these names authorizes an executable call in exported IR.
def numericSourceSupport : Array Name := #[
  ``UInt32.ofNat, ``UInt64.ofNat, ``OfNat.ofNat, ``UInt32.instOfNat, ``UInt64.instOfNat,
  ``HAdd.hAdd, ``instHAdd, ``Add.add, ``instAddUInt32, ``instAddUInt64,
  ``HSub.hSub, ``instHSub, ``Sub.sub, ``instSubUInt32, ``instSubUInt64,
  ``HMul.hMul, ``instHMul, ``Mul.mul, ``instMulUInt32, ``instMulUInt64, ``outParam,
  ``HAnd.hAnd, ``instHAndOfAndOp, ``AndOp.and, ``instAndOpUInt32, ``instAndOpUInt64,
  ``HOr.hOr, ``instHOrOfOrOp, ``OrOp.or, ``instOrOpUInt32, ``instOrOpUInt64,
  ``HXor.hXor, ``instHXorOfXorOp, ``XorOp.xor, ``instXorOpUInt32, ``instXorOpUInt64,
  ``Complement.complement, ``instComplementUInt32, ``instComplementUInt64,
  ``HShiftRight.hShiftRight, ``instHShiftRightOfShiftRight, ``ShiftRight.shiftRight,
  ``instShiftRightUInt32, ``instShiftRightUInt64,
  ``BEq.beq, ``Std.PreorderPackage.toBEq,
  ``Std.LinearPreorderPackage.toPreorderPackage,
  ``Std.LinearOrderPackage.toLinearPreorderPackage, numericOrderPackage, numericWideOrderPackage,
  ``Decidable.decide, ``instDecidableEqUInt32, ``instDecidableEqUInt64,
  ``instDecidableEqBool, ``instBEqOfDecidableEq,
  ``LT.lt, ``instLTUInt32, ``instLTUInt64, ``LE.le, ``instLEUInt32, ``instLEUInt64,
  ``ite, ``dite, ``Not, ``Bool.Reflects, ``Bool.Reflects.match_1]

def isNumericSourceBoundary (n : Name) : Bool :=
  (numericPrimitive? n).isSome || numericSourceSupport.contains n

structure TrustedObservation where
  name : Name
  sourceModule : Name
  external? : Option ExternAttrData
  implementation? : Option Name
  rewrite? : Option (Name × Name)
  initialized : Bool
  deriving Inhabited

structure TrustedBoundary where
  name : Name
  observations : Array TrustedObservation
  deriving Inhabited

initialize numericTrustExt : SimplePersistentEnvExtension TrustedBoundary (NameMap TrustedBoundary) ←
  registerSimplePersistentEnvExtension {
    addImportedFn := mkStateFromImportedEntries (fun s r => s.insert r.name r) {}
    addEntryFn := fun s r => s.insert r.name r
    asyncMode := .sync
  }

def observedRewrite (env : Environment) (n : Name) : Option (Name × Name) :=
  ((CSimp.ext.getState env).map.find? n).map fun e => (e.toDeclName, e.thmName)

def sourceModule? (env : Environment) (n : Name) : Option Name := do
  let idx ← env.getModuleIdxFor? n
  return env.allImportedModuleNames[idx]!

-- Build this evidence in the trusted producer module, before importing any user
-- source. The full wrapper closure matters: stopping at the order package alone
-- would miss a scoped replacement of one of its computational dependencies.
def captureNumericBoundary (n : Name) : CoreM TrustedBoundary := do
  let env := (← getEnv).setExporting false
  let mut pending := #[n]
  let mut seen : NameSet := {}
  let mut observations := #[]
  let mut fuel := 20000
  while !pending.isEmpty do
    if fuel == 0 then throwError "Trusted numeric source closure limit: {n}"
    fuel := fuel - 1
    let current := pending.back!
    pending := pending.pop
    unless seen.contains current do
      seen := seen.insert current
      let some info := env.find? current | throwError "Missing trusted numeric source: {current}"
      let some owner := sourceModule? env current | throwError "Numeric support is not imported: {current}"
      observations := observations.push {
        name := current, sourceModule := owner,
        external? := getExternAttrData? env current,
        implementation? := getImplementedBy? env current,
        rewrite? := observedRewrite env current,
        initialized := hasInitAttr env current }
      -- Follow the pristine compiler's replacement edges as well as logical
      -- bodies; a replacement can introduce additional computational helpers.
      if let some target := getImplementedBy? env current then
        pending := pending.push target
      if let some (target, theoremName) := observedRewrite env current then
        pending := pending.push target |>.push theoremName
      -- Proof bodies are erased; their logical assumptions are checked below.
      match info with
      | .defnInfo d => pending := pending ++ d.value.getUsedConstants
      | .opaqueInfo d => pending := pending ++ d.value.getUsedConstants
      | _ => pure ()
  return { name := n, observations }

def expectedNumericAxioms (n : Name) : Array Name :=
  if n == numericOrderPackage || n == numericWideOrderPackage then
    #[``propext, ``Classical.choice, ``Quot.sound]
  else if n == ``Std.LinearPreorderPackage.toPreorderPackage ||
      n == ``Std.LinearOrderPackage.toLinearPreorderPackage then #[``propext]
  else if #[``instAndOpUInt32, ``instAndOpUInt64, ``instOrOpUInt32, ``instOrOpUInt64,
      ``instXorOpUInt32, ``instXorOpUInt64, ``instComplementUInt32,
      ``instComplementUInt64].contains n then #[``propext, ``Quot.sound]
  else if n == ``instShiftRightUInt32 || n == ``instShiftRightUInt64 then #[``propext]
  else (numericPrimitive? n).map (·.axioms) |>.getD #[]

def sameNames (a b : Array Name) : Bool :=
  a.size == b.size && a.all b.contains && b.all a.contains

-- These checks also run when producing the baseline, so a changed toolchain
-- cannot silently enlarge the named primitive or axiom boundary.
def validateNumericDefinition (n : Name) : CoreM Unit := do
  let env ← getEnv
  let some (.defnInfo d) := (env.setExporting false).find? n
    | throwError "Trusted numeric boundary is not a definition: {n}"
  unless d.safety == .safe do throwError "Unsafe numeric boundary: {n}"
  unless sameNames (← collectAxioms n) (expectedNumericAxioms n) do
    throwError "Unexpected numeric boundary axioms: {n}"
  if let some primitive := numericPrimitive? n then
    let expected : ExternAttrData := { entries := [.standard `all primitive.externalSymbol] }
    unless getExternAttrData? env n == some expected do
      throwError "Unexpected numeric primitive extern: {n}"
    unless (getImplementedBy? env n).isNone && (observedRewrite env n).isNone && !hasInitAttr env n do
      throwError "Unexpected numeric primitive replacement: {n}"
  else if n == ``UInt32.ofNat || n == ``UInt64.ofNat then
    let symbol := if n == ``UInt32.ofNat then "lean_uint32_of_nat" else "lean_uint64_of_nat"
    let expected : ExternAttrData := { entries := [.standard `all symbol] }
    unless getExternAttrData? env n == some expected do
      throwError "Unexpected numeric literal extern: {n}"
  else unless (getExternAttrData? env n).isNone do
    throwError "Unexpected numeric source support extern: {n}"

def numericBoundaryIssues (env : Environment) (n : Name) : Array String := Id.run do
  let some boundary := (numericTrustExt.getState env).find? n
    | return #[s!"Missing trusted numeric source baseline: {n}"]
  let mut issues := #[]
  for observed in boundary.observations do
    let dep := observed.name
    unless sourceModule? env dep == some observed.sourceModule do
      issues := issues.push s!"Numeric source origin changed: {dep}"
    unless getExternAttrData? env dep == observed.external? do
      issues := issues.push s!"Numeric source extern changed: {dep}"
    unless getImplementedBy? env dep == observed.implementation? do
      issues := issues.push s!"Numeric source implemented_by changed: {dep}"
    unless observedRewrite env dep == observed.rewrite? do
      issues := issues.push s!"Numeric source compiler rewrite changed: {dep}"
    unless hasInitAttr env dep == observed.initialized do
      issues := issues.push s!"Numeric source initialization changed: {dep}"
  return issues

-- Decidable is a trivial structure erased to Bool in pure-mono. Check the actual
-- mono declaration instead of trying to treat its dependent kernel type as Bool.
def validateNumericPrimitive (primitive : NumericPrimitive) : CoreM Unit := do
  validateNumericDefinition primitive.name
  let issues := numericBoundaryIssues (← getEnv) primitive.name
  unless issues.isEmpty do throwError "Numeric primitive audit rejected: {issues}"
  let some decl ← LCNF.getMonoDecl? primitive.name
    | throwError "Missing numeric pure-mono declaration: {primitive.name}"
  let mut resultType := decl.type
  for _ in [:primitive.arity] do
    let .forallE _ operand body _ := resultType
      | throwError "Invalid numeric primitive signature: {primitive.name}"
    unless operand == .const primitive.operand [] do
      throwError "Invalid numeric primitive operand: {primitive.name}"
    resultType := body
  unless resultType == .const primitive.result [] &&
      decl.safe && decl.levelParams.isEmpty && decl.params.size == primitive.arity &&
      decl.params.all (·.type == .const primitive.operand []) do
    throwError "Numeric primitive signature/safety mismatch: {primitive.name}"
  let .extern data := decl.value | throwError "Numeric primitive is not external: {primitive.name}"
  unless data == { entries := [.standard `all primitive.externalSymbol] } do
    throwError "Numeric mono extern mismatch: {primitive.name}"

end LeanGo.Module
