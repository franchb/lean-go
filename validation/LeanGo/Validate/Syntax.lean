module
public import LeanGo.Validate.Prim

/-!
# Deep embedding of the module IR

The constructors mirror the JSON IR exactly (`internal/moduleir/types.go`):

* `Code.ret x`                         – `{"op":"return","var":x}`
* `Code.bind p v next`                 – `{"op":"let","bind":p,"value":v,"next":next}`
* `Code.cases x ty result alts`        – `{"op":"cases",...}`
* `Code.join j params body next`       – `{"op":"join","join":{...},"next":next}`
* `Code.jump j args`                   – `{"op":"jump","target":j,"args":args}`

and a let value is a literal, a primitive, a constructor or a call. Names of
constructors are kept for documentation; their tags (resolved by the loader
from the program's type table) are what the semantics uses. Variables and join points are natural numbers.
-/

@[expose] public section

namespace LeanGo.Validate

/-- Scalar literals (`uint8` ... `uint64` let values). -/
inductive Lit where
  | u8 (x : UInt8)
  | u16 (x : UInt16)
  | u32 (x : UInt32)
  | u64 (x : UInt64)
  deriving Repr, DecidableEq, Inhabited

def Lit.toValue : Lit → Value
  | .u8 x => .u8 x
  | .u16 x => .u16 x
  | .u32 x => .u32 x
  | .u64 x => .u64 x

/-- Right-hand sides of `let`. -/
inductive LetVal where
  | lit (l : Lit)
  | prim (p : Prim) (args : List Nat)
  | ctor (name : String) (tag : Nat) (args : List Nat)
  | call (fn : String) (args : List Nat)
  deriving Repr, Inhabited

/-- A typed binder. The type is an IR type name. -/
structure Param where
  id : Nat
  ty : String
  deriving Repr, DecidableEq, Inhabited

mutual
inductive Code where
  | ret (x : Nat)
  | bind (p : Param) (v : LetVal) (next : Code)
  | cases (x : Nat) (ty : String) (result : String) (alts : Alts)
  | join (j : Nat) (params : List Param) (body : Code) (next : Code)
  | jump (j : Nat) (args : List Nat)

/-- Case alternatives, in IR order. -/
inductive Alts where
  | nil
  | cons (ctor : String) (tag : Nat) (params : List Param) (body : Code) (rest : Alts)
end

instance : Inhabited Code := ⟨.ret 0⟩

structure Function where
  name : String
  params : List Param
  result : String
  body : Code
  deriving Inhabited

structure Program where
  types : TypeTable
  functions : List Function
  deriving Inhabited

/-- Tag of a named constructor in the type table. -/
def TypeTable.ctorTag? (tys : TypeTable) (ty ctor : String) : Option Nat :=
  (tys.lookup ty).bind fun cs => (cs.find? (·.name == ctor)).map (·.tag)

end LeanGo.Validate
