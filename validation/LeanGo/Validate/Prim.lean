module
public import LeanGo.Validate.Value
public meta import LeanGo.Validate.Attr

/-!
# Primitive semantics

The complete unsigned primitive matrix, defined directly with Lean's own
`UInt8/16/32/64` operations so that symbolic evaluation produces exactly the
terms the source elaborates to (`a + b`, `a <<< b`, `decide (a < b)`,
`a.toUInt64`, ...). Names follow `producer/LeanGo/Module/Primitives.lean`
(`UInt32.add`, `UInt8.toUInt16`, ...). The producer admits exactly this
matrix: all 68 primitives, including `div`, `mod` and the twelve conversions.

Shift counts are reduced modulo the width and division/remainder by zero
yield `0` and the dividend respectively, because that is what Lean's
operations do.
-/

@[expose] public section

namespace LeanGo.Validate

inductive Width where
  | w8 | w16 | w32 | w64
  deriving Repr, DecidableEq, Inhabited

def Width.typeName : Width → String
  | .w8 => "UInt8" | .w16 => "UInt16" | .w32 => "UInt32" | .w64 => "UInt64"

inductive BinOp where
  | add | sub | mul | div | mod | land | lor | xor | shiftLeft | shiftRight
  deriving Repr, DecidableEq, Inhabited

inductive CmpOp where
  | decEq | decLt | decLe
  deriving Repr, DecidableEq, Inhabited

/-- IR primitives. `conv a b` is `UIntA.toUIntB`; only `a ≠ b` is named in
the IR, `conv a a` is stuck. -/
inductive Prim where
  | bin (op : BinOp) (w : Width)
  | complement (w : Width)
  | cmp (op : CmpOp) (w : Width)
  | conv (src dst : Width)
  deriving Repr, DecidableEq, Inhabited

def BinOp.opName : BinOp → String
  | .add => "add" | .sub => "sub" | .mul => "mul" | .div => "div" | .mod => "mod"
  | .land => "land" | .lor => "lor" | .xor => "xor"
  | .shiftLeft => "shiftLeft" | .shiftRight => "shiftRight"

def CmpOp.opName : CmpOp → String
  | .decEq => "decEq" | .decLt => "decLt" | .decLe => "decLe"

/-- The primitive's IR name, as in `Primitives.lean`. -/
def Prim.name : Prim → String
  | .bin op w => w.typeName ++ "." ++ op.opName
  | .complement w => w.typeName ++ ".complement"
  | .cmp op w => w.typeName ++ "." ++ op.opName
  | .conv a b => a.typeName ++ ".to" ++ b.typeName

def Width.all : List Width := [.w8, .w16, .w32, .w64]
def BinOp.all : List BinOp :=
  [.add, .sub, .mul, .div, .mod, .land, .lor, .xor, .shiftLeft, .shiftRight]
def CmpOp.all : List CmpOp := [.decEq, .decLt, .decLe]

/-- Every primitive of the matrix: 4 × (10 + 1 + 3) operations and 12 conversions. -/
def Prim.all : List Prim :=
  (Width.all.flatMap fun w =>
    (BinOp.all.map (Prim.bin · w)) ++ [Prim.complement w] ++ (CmpOp.all.map (Prim.cmp · w))) ++
  (Width.all.flatMap fun a => (Width.all.filter (· ≠ a)).map (Prim.conv a))

/-- Resolve an IR primitive name. -/
def Prim.ofName? (s : String) : Option Prim :=
  Prim.all.find? (·.name == s)

theorem Prim.all_length : Prim.all.length = 68 := by decide

/-- Homogeneous binary operation on one Lean carrier. -/
def BinOp.eval {α : Type} [Add α] [Sub α] [Mul α] [Div α] [Mod α] [AndOp α] [OrOp α]
    [XorOp α] [ShiftLeft α] [ShiftRight α] : BinOp → α → α → α
  | .add, a, b => a + b
  | .sub, a, b => a - b
  | .mul, a, b => a * b
  | .div, a, b => a / b
  | .mod, a, b => a % b
  | .land, a, b => a &&& b
  | .lor, a, b => a ||| b
  | .xor, a, b => a ^^^ b
  | .shiftLeft, a, b => a <<< b
  | .shiftRight, a, b => a >>> b

/-- Decision procedures; the result is the IR `Bool`. -/
def CmpOp.eval {α : Type} [DecidableEq α] [LT α] [LE α] [DecidableRel (α := α) (· < ·)]
    [DecidableRel (α := α) (· ≤ ·)] : CmpOp → α → α → Bool
  | .decEq, a, b => decide (a = b)
  | .decLt, a, b => decide (a < b)
  | .decLe, a, b => decide (a ≤ b)

section
variable {α : Type} [Add α] [Sub α] [Mul α] [Div α] [Mod α] [AndOp α] [OrOp α]
    [XorOp α] [ShiftLeft α] [ShiftRight α] (a b : α)
@[ir_simp] theorem BinOp.eval_add : BinOp.add.eval a b = a + b := rfl
@[ir_simp] theorem BinOp.eval_sub : BinOp.sub.eval a b = a - b := rfl
@[ir_simp] theorem BinOp.eval_mul : BinOp.mul.eval a b = a * b := rfl
@[ir_simp] theorem BinOp.eval_div : BinOp.div.eval a b = a / b := rfl
@[ir_simp] theorem BinOp.eval_mod : BinOp.mod.eval a b = a % b := rfl
@[ir_simp] theorem BinOp.eval_land : BinOp.land.eval a b = a &&& b := rfl
@[ir_simp] theorem BinOp.eval_lor : BinOp.lor.eval a b = a ||| b := rfl
@[ir_simp] theorem BinOp.eval_xor : BinOp.xor.eval a b = a ^^^ b := rfl
@[ir_simp] theorem BinOp.eval_shiftLeft : BinOp.shiftLeft.eval a b = a <<< b := rfl
@[ir_simp] theorem BinOp.eval_shiftRight : BinOp.shiftRight.eval a b = a >>> b := rfl
end

section
variable {α : Type} [DecidableEq α] [LT α] [LE α] [DecidableRel (α := α) (· < ·)]
    [DecidableRel (α := α) (· ≤ ·)] (a b : α)
@[ir_simp] theorem CmpOp.eval_decEq : CmpOp.decEq.eval a b = decide (a = b) := rfl
@[ir_simp] theorem CmpOp.eval_decLt : CmpOp.decLt.eval a b = decide (a < b) := rfl
@[ir_simp] theorem CmpOp.eval_decLe : CmpOp.decLe.eval a b = decide (a ≤ b) := rfl
end

/-- Width conversion of a scalar value (`none` when the source width does not match). -/
def convert : Width → Width → Value → Option Value
  | .w8, .w16, .u8 a => some (.u16 a.toUInt16)
  | .w8, .w32, .u8 a => some (.u32 a.toUInt32)
  | .w8, .w64, .u8 a => some (.u64 a.toUInt64)
  | .w16, .w8, .u16 a => some (.u8 a.toUInt8)
  | .w16, .w32, .u16 a => some (.u32 a.toUInt32)
  | .w16, .w64, .u16 a => some (.u64 a.toUInt64)
  | .w32, .w8, .u32 a => some (.u8 a.toUInt8)
  | .w32, .w16, .u32 a => some (.u16 a.toUInt16)
  | .w32, .w64, .u32 a => some (.u64 a.toUInt64)
  | .w64, .w8, .u64 a => some (.u8 a.toUInt8)
  | .w64, .w16, .u64 a => some (.u16 a.toUInt16)
  | .w64, .w32, .u64 a => some (.u32 a.toUInt32)
  | _, _, _ => none

/-- Semantics of a primitive on its argument values; ill-typed or
wrong-arity applications are stuck (`none`). -/
def Prim.eval : Prim → List Value → Option Value
  | .bin op .w8, [.u8 a, .u8 b] => some (.u8 (op.eval a b))
  | .bin op .w16, [.u16 a, .u16 b] => some (.u16 (op.eval a b))
  | .bin op .w32, [.u32 a, .u32 b] => some (.u32 (op.eval a b))
  | .bin op .w64, [.u64 a, .u64 b] => some (.u64 (op.eval a b))
  | .complement .w8, [.u8 a] => some (.u8 (~~~a))
  | .complement .w16, [.u16 a] => some (.u16 (~~~a))
  | .complement .w32, [.u32 a] => some (.u32 (~~~a))
  | .complement .w64, [.u64 a] => some (.u64 (~~~a))
  | .cmp op .w8, [.u8 a, .u8 b] => some (Encode.encode (op.eval a b))
  | .cmp op .w16, [.u16 a, .u16 b] => some (Encode.encode (op.eval a b))
  | .cmp op .w32, [.u32 a, .u32 b] => some (Encode.encode (op.eval a b))
  | .cmp op .w64, [.u64 a, .u64 b] => some (Encode.encode (op.eval a b))
  | .conv a b, [v] => convert a b v
  | _, _ => none

/-! Evaluation equations, one per primitive shape (used by symbolic evaluation
instead of unfolding the large `match`). -/
section
variable (op : BinOp) (cop : CmpOp)
@[ir_simp] theorem Prim.eval_bin8 (a b : UInt8) : (Prim.bin op .w8).eval [.u8 a, .u8 b] = some (.u8 (op.eval a b)) := rfl
@[ir_simp] theorem Prim.eval_bin16 (a b : UInt16) : (Prim.bin op .w16).eval [.u16 a, .u16 b] = some (.u16 (op.eval a b)) := rfl
@[ir_simp] theorem Prim.eval_bin32 (a b : UInt32) : (Prim.bin op .w32).eval [.u32 a, .u32 b] = some (.u32 (op.eval a b)) := rfl
@[ir_simp] theorem Prim.eval_bin64 (a b : UInt64) : (Prim.bin op .w64).eval [.u64 a, .u64 b] = some (.u64 (op.eval a b)) := rfl
@[ir_simp] theorem Prim.eval_complement8 (a : UInt8) : (Prim.complement .w8).eval [.u8 a] = some (.u8 (~~~a)) := rfl
@[ir_simp] theorem Prim.eval_complement16 (a : UInt16) : (Prim.complement .w16).eval [.u16 a] = some (.u16 (~~~a)) := rfl
@[ir_simp] theorem Prim.eval_complement32 (a : UInt32) : (Prim.complement .w32).eval [.u32 a] = some (.u32 (~~~a)) := rfl
@[ir_simp] theorem Prim.eval_complement64 (a : UInt64) : (Prim.complement .w64).eval [.u64 a] = some (.u64 (~~~a)) := rfl
@[ir_simp] theorem Prim.eval_cmp8 (a b : UInt8) : (Prim.cmp cop .w8).eval [.u8 a, .u8 b] = some (Encode.encode (cop.eval a b)) := rfl
@[ir_simp] theorem Prim.eval_cmp16 (a b : UInt16) : (Prim.cmp cop .w16).eval [.u16 a, .u16 b] = some (Encode.encode (cop.eval a b)) := rfl
@[ir_simp] theorem Prim.eval_cmp32 (a b : UInt32) : (Prim.cmp cop .w32).eval [.u32 a, .u32 b] = some (Encode.encode (cop.eval a b)) := rfl
@[ir_simp] theorem Prim.eval_cmp64 (a b : UInt64) : (Prim.cmp cop .w64).eval [.u64 a, .u64 b] = some (Encode.encode (cop.eval a b)) := rfl
@[ir_simp] theorem Prim.eval_conv (a b : Width) (v : Value) : (Prim.conv a b).eval [v] = convert a b v := rfl
end

@[ir_simp] theorem convert_8_16 (a : UInt8) : convert .w8 .w16 (.u8 a) = some (.u16 a.toUInt16) := rfl
@[ir_simp] theorem convert_8_32 (a : UInt8) : convert .w8 .w32 (.u8 a) = some (.u32 a.toUInt32) := rfl
@[ir_simp] theorem convert_8_64 (a : UInt8) : convert .w8 .w64 (.u8 a) = some (.u64 a.toUInt64) := rfl
@[ir_simp] theorem convert_16_8 (a : UInt16) : convert .w16 .w8 (.u16 a) = some (.u8 a.toUInt8) := rfl
@[ir_simp] theorem convert_16_32 (a : UInt16) : convert .w16 .w32 (.u16 a) = some (.u32 a.toUInt32) := rfl
@[ir_simp] theorem convert_16_64 (a : UInt16) : convert .w16 .w64 (.u16 a) = some (.u64 a.toUInt64) := rfl
@[ir_simp] theorem convert_32_8 (a : UInt32) : convert .w32 .w8 (.u32 a) = some (.u8 a.toUInt8) := rfl
@[ir_simp] theorem convert_32_16 (a : UInt32) : convert .w32 .w16 (.u32 a) = some (.u16 a.toUInt16) := rfl
@[ir_simp] theorem convert_32_64 (a : UInt32) : convert .w32 .w64 (.u32 a) = some (.u64 a.toUInt64) := rfl
@[ir_simp] theorem convert_64_8 (a : UInt64) : convert .w64 .w8 (.u64 a) = some (.u8 a.toUInt8) := rfl
@[ir_simp] theorem convert_64_16 (a : UInt64) : convert .w64 .w16 (.u64 a) = some (.u16 a.toUInt16) := rfl
@[ir_simp] theorem convert_64_32 (a : UInt64) : convert .w64 .w32 (.u64 a) = some (.u32 a.toUInt32) := rfl

/-- `==` on the scalars is `decide (· = ·)`, the form the IR's `decEq` produces. -/
@[ir_simp] theorem beq_u8 (a b : UInt8) : (a == b) = decide (a = b) := rfl
@[ir_simp] theorem beq_u16 (a b : UInt16) : (a == b) = decide (a = b) := rfl
@[ir_simp] theorem beq_u32 (a b : UInt32) : (a == b) = decide (a = b) := rfl
@[ir_simp] theorem beq_u64 (a b : UInt64) : (a == b) = decide (a = b) := rfl

/-! Sanity checks on the name table (all by kernel evaluation). -/

example : Prim.ofName? "UInt32.add" = some (.bin .add .w32) := by decide
example : Prim.ofName? "UInt8.toUInt16" = some (.conv .w8 .w16) := by decide
example : Prim.ofName? "UInt64.decLe" = some (.cmp .decLe .w64) := by decide
example : Prim.ofName? "UInt16.toUInt16" = none := by decide
example : Prim.eval (.bin .shiftLeft .w8) [.u8 1, .u8 9] = some (.u8 2) := rfl
example : Prim.eval (.conv .w64 .w8) [.u64 0x1234] = some (.u8 0x34) := rfl

end LeanGo.Validate
