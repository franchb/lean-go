module
public meta import LeanGo.Validate.Attr

/-!
# Universal value domain

Every value an extracted IR program manipulates is a `Value`: a fixed-width
unsigned scalar, or a constructor value with a numeric tag and a field list.
Constructor tags are Lean constructor indices, exactly as the IR type table
records them (`Bool.false` = 0, `Bool.true` = 1, `List.nil` = 0, ...).

This file also contains

* the IR type table (`TypeTable`) and the structural typing judgement
  `Value.hasType` used at function boundaries;
* the `Encode` class relating a Lean source type to its IR representation,
  with the law `decode (encode a) = some a`;
* `Realizes`, the converse "typed values are encodings" property, and the
  `IRType` class packaging it with the type's IR name and declarations;
* instances for `UInt8/16/32/64`, `Bool` and `List α` (the IR byte list is
  `List UInt8`).
-/

@[expose] public section

namespace LeanGo.Validate

/-- IR values. Scalars carry Lean's own fixed-width integers. -/
inductive Value where
  | u8 (x : UInt8)
  | u16 (x : UInt16)
  | u32 (x : UInt32)
  | u64 (x : UInt64)
  | ctor (tag : Nat) (fields : List Value)
  deriving Repr, BEq, Inhabited

/-- Constructor view used by `cases`; scalars have none. -/
def Value.view : Value → Option (Nat × List Value)
  | .ctor tag fields => some (tag, fields)
  | _ => none

@[ir_simp] theorem Value.view_ctor (t : Nat) (fs : List Value) :
    (Value.ctor t fs).view = some (t, fs) := rfl
@[ir_simp] theorem Value.view_u8 (x : UInt8) : (Value.u8 x).view = none := rfl
@[ir_simp] theorem Value.view_u16 (x : UInt16) : (Value.u16 x).view = none := rfl
@[ir_simp] theorem Value.view_u32 (x : UInt32) : (Value.u32 x).view = none := rfl
@[ir_simp] theorem Value.view_u64 (x : UInt64) : (Value.u64 x).view = none := rfl

/-! ## IR type table -/

/-- One constructor of an IR type declaration. -/
structure CtorDecl where
  name : String
  tag : Nat
  fields : List String
  deriving Repr, DecidableEq, Inhabited

/-- An IR type declaration: a named inductive type with its constructors. -/
structure TypeDecl where
  name : String
  ctors : List CtorDecl
  deriving Repr, DecidableEq, Inhabited

abbrev TypeTable := List TypeDecl

/-- Scalar type names. They never denote table entries. -/
def isScalarName (T : String) : Bool :=
  T == "UInt8" || T == "UInt16" || T == "UInt32" || T == "UInt64"

/-- First declaration with the given name. -/
def TypeTable.lookup : TypeTable → String → Option (List CtorDecl)
  | [], _ => none
  | d :: ds, T => if d.name = T then some d.ctors else TypeTable.lookup ds T

/-- Field types of the first constructor with the given tag. -/
def ctorsFields : List CtorDecl → Nat → Option (List String)
  | [], _ => none
  | c :: cs, t => if c.tag = t then some c.fields else ctorsFields cs t

def TypeTable.ctorFields (tys : TypeTable) (T : String) (tag : Nat) : Option (List String) :=
  (tys.lookup T).bind fun ctors => ctorsFields ctors tag

mutual
/-- Structural typing of a value against an IR type name. Scalar names are
fixed; any other name is looked up in the table and the constructor's fields
are checked recursively. -/
def Value.hasType (tys : TypeTable) (T : String) : Value → Bool
  | .u8 _ => T == "UInt8"
  | .u16 _ => T == "UInt16"
  | .u32 _ => T == "UInt32"
  | .u64 _ => T == "UInt64"
  | .ctor tag fs =>
    if isScalarName T then false else
    match tys.ctorFields T tag with
    | some Ts => Value.hasTypes tys Ts fs
    | none => false

/-- Pointwise typing of an argument list; lengths must agree. -/
def Value.hasTypes (tys : TypeTable) : List String → List Value → Bool
  | [], [] => true
  | T :: Ts, v :: vs => Value.hasType tys T v && Value.hasTypes tys Ts vs
  | _, _ => false
end

theorem Value.hasTypes_nil_iff (tys : TypeTable) (vs : List Value) :
    Value.hasTypes tys [] vs = true ↔ vs = [] := by
  cases vs <;> simp [Value.hasTypes]

theorem Value.hasTypes_cons_iff (tys : TypeTable) (T : String) (Ts : List String)
    (vs : List Value) :
    Value.hasTypes tys (T :: Ts) vs = true ↔
      ∃ v vs', vs = v :: vs' ∧ Value.hasType tys T v = true ∧ Value.hasTypes tys Ts vs' = true := by
  cases vs with
  | nil => simp [Value.hasTypes]
  | cons v vs =>
    simp only [Value.hasTypes, Bool.and_eq_true, List.cons.injEq]
    constructor
    · intro h; exact ⟨v, vs, ⟨rfl, rfl⟩, h⟩
    · rintro ⟨_, _, ⟨rfl, rfl⟩, h⟩; exact h

theorem Value.hasType_ctor_iff (tys : TypeTable) (T : String) (tag : Nat) (fs : List Value) :
    Value.hasType tys T (.ctor tag fs) = true ↔
      isScalarName T = false ∧ ∃ Ts, tys.ctorFields T tag = some Ts ∧ Value.hasTypes tys Ts fs = true := by
  simp only [Value.hasType]
  cases isScalarName T <;> simp
  split <;> simp_all

/-! ## Source encodings -/

/-- Relates a Lean source type to its IR representation. -/
class Encode (α : Type) where
  encode : α → Value
  decode : Value → Option α
  decode_encode : ∀ a, decode (encode a) = some a

attribute [simp, ir_simp] Encode.decode_encode

open Encode

theorem Encode.encode_injective {α} [Encode α] {a b : α} (h : encode a = encode b) : a = b := by
  have := congrArg (decode (α := α)) h
  simpa using this

/-- Every value of IR type `T` (relative to `tys`) is the encoding of some
source value of type `α`. This is what lets obligations be stated over source
values while the IR boundary checks only IR types. -/
def Realizes (tys : TypeTable) (T : String) (α : Type) [Encode α] : Prop :=
  ∀ v, Value.hasType tys T v = true → ∃ a : α, encode a = v

/-- `decls ⊑ tys`: every declaration in `decls` is what `tys` finds first under its name. -/
def Subtable (decls tys : TypeTable) : Prop :=
  ∀ d ∈ decls, tys.lookup d.name = some d.ctors

instance (decls tys : TypeTable) : Decidable (Subtable decls tys) :=
  inferInstanceAs (Decidable (∀ d ∈ decls, tys.lookup d.name = some d.ctors))

/-- A source type with a fixed IR name, the IR declarations it needs, and a
proof that IR-typed values are encodings whenever those declarations are in
the table. -/
class IRType (α : Type) [Encode α] where
  irName : String
  decls : TypeTable
  realizes : ∀ tys, Subtable decls tys → Realizes tys irName α

theorem IRType.realizes_of {α} [Encode α] [IRType α] {tys : TypeTable} {T : String}
    (h : Subtable (IRType.decls α) tys) (hn : IRType.irName α = T) : Realizes tys T α :=
  hn ▸ IRType.realizes tys h

/-- Strong induction principle for proving `Realizes` of a table type, one
constructor level at a time. -/
theorem Realizes.of_ctors {α : Type} [Encode α] (tys : TypeTable) (T : String)
    (ctors : List CtorDecl) (hT : tys.lookup T = some ctors) (hs : isScalarName T = false)
    (h : ∀ tag Ts fs, ctorsFields ctors tag = some Ts →
      (∀ w ∈ fs, Value.hasType tys T w = true → ∃ a : α, encode a = w) →
      Value.hasTypes tys Ts fs = true → ∃ a : α, encode a = Value.ctor tag fs) :
    Realizes tys T α := by
  intro v
  induction v using Value.rec
    (motive_2 := fun fs => ∀ w ∈ fs, Value.hasType tys T w = true → ∃ a : α, encode a = w) with
  | u8 x => simp [Value.hasType, isScalarName] at hs ⊢; intro h; subst h; simp at hs
  | u16 x => simp [Value.hasType, isScalarName] at hs ⊢; intro h; subst h; simp at hs
  | u32 x => simp [Value.hasType, isScalarName] at hs ⊢; intro h; subst h; simp at hs
  | u64 x => simp [Value.hasType, isScalarName] at hs ⊢; intro h; subst h; simp at hs
  | ctor tag fs ih =>
    intro hv
    rw [Value.hasType_ctor_iff] at hv
    obtain ⟨_, Ts, hTs, hfs⟩ := hv
    simp only [TypeTable.ctorFields, hT, Option.bind_some] at hTs
    exact h tag Ts fs hTs ih hfs
  | nil => simp_all
  | cons w ws ihw ihws =>
    rename_i x hx hxT
    simp only [List.mem_cons] at hx
    rcases hx with rfl | hx
    · exact ihw hxT
    · exact ihws x hx hxT

/-! ## Scalar instances -/

instance : Encode UInt8 where
  encode := .u8
  decode | .u8 x => some x | _ => none
  decode_encode _ := rfl

instance : IRType UInt8 where
  irName := "UInt8"
  decls := []
  realizes tys _ v h := by
    cases v <;> simp_all [Value.hasType, isScalarName]
    exact ⟨_, rfl⟩

instance : Encode UInt16 where
  encode := .u16
  decode | .u16 x => some x | _ => none
  decode_encode _ := rfl

instance : IRType UInt16 where
  irName := "UInt16"
  decls := []
  realizes tys _ v h := by
    cases v <;> simp_all [Value.hasType, isScalarName]
    exact ⟨_, rfl⟩

instance : Encode UInt32 where
  encode := .u32
  decode | .u32 x => some x | _ => none
  decode_encode _ := rfl

instance : IRType UInt32 where
  irName := "UInt32"
  decls := []
  realizes tys _ v h := by
    cases v <;> simp_all [Value.hasType, isScalarName]
    exact ⟨_, rfl⟩

instance : Encode UInt64 where
  encode := .u64
  decode | .u64 x => some x | _ => none
  decode_encode _ := rfl

instance : IRType UInt64 where
  irName := "UInt64"
  decls := []
  realizes tys _ v h := by
    cases v <;> simp_all [Value.hasType, isScalarName]
    exact ⟨_, rfl⟩

@[ir_simp] theorem encode_u8 (x : UInt8) : encode x = .u8 x := rfl
@[ir_simp] theorem encode_u16 (x : UInt16) : encode x = .u16 x := rfl
@[ir_simp] theorem encode_u32 (x : UInt32) : encode x = .u32 x := rfl
@[ir_simp] theorem encode_u64 (x : UInt64) : encode x = .u64 x := rfl
@[ir_simp] theorem decode_u8 (x : UInt8) : (decode (.u8 x) : Option UInt8) = some x := rfl
@[ir_simp] theorem decode_u16 (x : UInt16) : (decode (.u16 x) : Option UInt16) = some x := rfl
@[ir_simp] theorem decode_u32 (x : UInt32) : (decode (.u32 x) : Option UInt32) = some x := rfl
@[ir_simp] theorem decode_u64 (x : UInt64) : (decode (.u64 x) : Option UInt64) = some x := rfl

/-! ## Bool -/

def boolDecl : TypeDecl :=
  { name := "Bool", ctors := [⟨"Bool.false", 0, []⟩, ⟨"Bool.true", 1, []⟩] }

def Value.ofBool : Bool → Value
  | false => .ctor 0 []
  | true => .ctor 1 []

def Value.toBool? : Value → Option Bool
  | .ctor 0 [] => some false
  | .ctor 1 [] => some true
  | _ => none

instance : Encode Bool where
  encode := Value.ofBool
  decode := Value.toBool?
  decode_encode b := by cases b <;> rfl

instance : IRType Bool where
  irName := "Bool"
  decls := [boolDecl]
  realizes tys h := by
    have hT : tys.lookup "Bool" = some boolDecl.ctors := h boolDecl (by simp)
    apply Realizes.of_ctors tys "Bool" boolDecl.ctors hT rfl
    intro tag Ts fs hTs _ hfs
    simp only [boolDecl, ctorsFields] at hTs
    split at hTs
    · cases hTs; subst tag
      rw [Value.hasTypes_nil_iff] at hfs; subst hfs; exact ⟨false, rfl⟩
    · split at hTs
      · cases hTs; subst tag
        rw [Value.hasTypes_nil_iff] at hfs; subst hfs; exact ⟨true, rfl⟩
      · cases hTs

@[ir_simp] theorem encode_false : encode false = .ctor 0 [] := rfl
@[ir_simp] theorem encode_true : encode true = .ctor 1 [] := rfl
@[ir_simp] theorem decode_bool_0 : (decode (.ctor 0 []) : Option Bool) = some false := rfl
@[ir_simp] theorem decode_bool_1 : (decode (.ctor 1 []) : Option Bool) = some true := rfl

/-! ## Lists (the IR byte list `List UInt8`) -/

def listDecl (elem : String) : TypeDecl :=
  { name := "List " ++ elem,
    ctors := [⟨"List.nil", 0, []⟩, ⟨"List.cons", 1, [elem, "List " ++ elem]⟩] }

def encodeList {α} [Encode α] : List α → Value
  | [] => .ctor 0 []
  | x :: xs => .ctor 1 [encode x, encodeList xs]

def decodeList {α} [Encode α] : Value → Option (List α)
  | .ctor 0 [] => some []
  | .ctor 1 [v, vs] => (decode v).bind fun x => (decodeList vs).bind fun xs => some (x :: xs)
  | _ => none

instance {α} [Encode α] : Encode (List α) where
  encode := encodeList
  decode := decodeList
  decode_encode xs := by
    induction xs with
    | nil => rfl
    | cons x xs ih => simp [encodeList, decodeList, ih]

@[ir_simp] theorem encode_nil {α} [Encode α] : encode ([] : List α) = .ctor 0 [] := rfl
@[ir_simp] theorem encode_cons {α} [Encode α] (x : α) (xs : List α) :
    encode (x :: xs) = .ctor 1 [encode x, encode xs] := rfl
@[ir_simp] theorem decode_list_0 {α} [Encode α] :
    (decode (.ctor 0 []) : Option (List α)) = some [] := rfl
@[ir_simp] theorem decode_list_1 {α} [Encode α] (v vs : Value) :
    (decode (.ctor 1 [v, vs]) : Option (List α)) =
      (decode v).bind fun x => (decode vs).bind fun xs => some (x :: xs) := rfl

instance {α} [Encode α] [IRType α] : IRType (List α) where
  irName := "List " ++ IRType.irName α
  decls := listDecl (IRType.irName α) :: IRType.decls α
  realizes tys h := by
    have hT := h (listDecl (IRType.irName α)) (by simp)
    have hα : Realizes tys (IRType.irName α) α :=
      IRType.realizes tys (fun d hd => h d (by simp [hd]))
    refine Realizes.of_ctors tys _ _ hT ?_ ?_
    · simp only [isScalarName, Bool.or_eq_false_iff, beq_eq_false_iff_ne, ne_eq]
      refine ⟨⟨⟨?_, ?_⟩, ?_⟩, ?_⟩ <;> intro h <;> have := congrArg String.toList h <;> simp at this
    intro tag Ts fs hTs ih hfs
    simp only [listDecl, ctorsFields] at hTs
    split at hTs
    · cases hTs; subst tag
      rw [Value.hasTypes_nil_iff] at hfs; subst hfs; exact ⟨[], rfl⟩
    · split at hTs
      · cases hTs; subst tag
        simp only [Value.hasTypes_cons_iff, Value.hasTypes_nil_iff] at hfs
        obtain ⟨v, _, rfl, hv, w, _, rfl, hw, rfl⟩ := hfs
        obtain ⟨x, rfl⟩ := hα v hv
        obtain ⟨xs, rfl⟩ := ih w (by simp) hw
        exact ⟨x :: xs, rfl⟩
      · cases hTs

end LeanGo.Validate
