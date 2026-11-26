module
public meta import Lean
public import LeanGo.Validate.Spec
public meta section

/-!
# JSON bridge: `validate_ir_program`

```
validate_ir_program prog from "IR/clamp.ir.json"
```

reads a module IR file (path relative to the current source file) and
defines

* `prog.types : TypeTable` – the `types` array;
* `prog.«F» : Function` – one definition per entry of `functions`, named by
  the IR function name `F`;
* `prog : Program`.

Only `types` and `functions` are read. The metadata fields (`schema`,
`profile`, `features`, `inputs`, `provenance`, `claims`, `rewrites`, `api`,
…) are ignored. The bridge checks, and fails loudly otherwise, that every
primitive name is in the semantics' matrix (`Prim.ofName?`), that literals fit
their width, and that every constructor named in a `let` or `cases` exists in
the type table; the constructor's tag is taken from the table. For each
primitive used it also emits a kernel-checked `example` that the embedded
`Prim` has exactly that IR name.

The bridge is part of the trusted base: it is what says that the `Program`
term *is* the JSON file.
-/

open Lean Elab Command Meta Term

namespace LeanGo.Validate.Meta

def V' (n : Name) : Ident := mkCIdent (`LeanGo.Validate ++ n)

def jerr (msg : String) : CommandElabM α := throwError "validate_ir_program: {msg}"

def getStr (j : Json) (k : String) : CommandElabM String :=
  match j.getObjValAs? String k with
  | .ok s => pure s
  | .error e => jerr s!"missing string field {k}: {e}"

def getNat (j : Json) (k : String) : CommandElabM Nat :=
  match j.getObjValAs? Nat k with
  | .ok s => pure s
  | .error e => jerr s!"missing natural field {k}: {e}"

/-- Optional (Go `omitempty`) natural field. -/
def getNatD (j : Json) (k : String) : CommandElabM Nat :=
  match j.getObjVal? k with
  | .ok _ => getNat j k
  | .error _ => pure 0

def getArr (j : Json) (k : String) : CommandElabM (Array Json) :=
  match j.getObjVal? k with
  | .ok (.arr a) => pure a
  | .ok .null => pure #[]
  | .ok _ => jerr s!"field {k} is not an array"
  | .error _ => pure #[]

def getObj (j : Json) (k : String) : CommandElabM Json :=
  match j.getObjVal? k with
  | .ok v => pure v
  | .error e => jerr s!"missing field {k}: {e}"

structure TypeRow where
  name : String
  ctors : Array (String × Nat × Array String)

def widthIdent : String → Option Name
  | "UInt8" => some `w8 | "UInt16" => some `w16 | "UInt32" => some `w32 | "UInt64" => some `w64
  | _ => none

/-- Syntax of the embedded primitive for an IR primitive name. -/
def primSyntax (name : String) : CommandElabM Term := do
  match name.splitOn "." with
  | [ty, op] =>
    let some w := widthIdent ty | jerr s!"unknown primitive {name}"
    let wS := V' (`Width ++ w)
    if op.startsWith "to" then
      let some w' := widthIdent (op.drop 2).toString | jerr s!"unknown primitive {name}"
      if w == w' then jerr s!"unknown primitive {name}"
      return ← `($(V' `Prim.conv) $wS $(V' (`Width ++ w')))
    if op == "complement" then return ← `($(V' `Prim.complement) $wS)
    if ["decEq", "decLt", "decLe"].contains op then
      return ← `($(V' `Prim.cmp) $(V' (`CmpOp ++ Name.mkSimple op)) $wS)
    if ["add", "sub", "mul", "div", "mod", "land", "lor", "xor", "shiftLeft", "shiftRight"].contains op then
      return ← `($(V' `Prim.bin) $(V' (`BinOp ++ Name.mkSimple op)) $wS)
    jerr s!"unknown primitive {name}"
  | _ => jerr s!"unknown primitive {name}"

def litSyntax (kind : String) (n : Nat) : CommandElabM Term := do
  let (ctor, bits) ← match kind with
    | "uint8" => pure (`Lit.u8, 8) | "uint16" => pure (`Lit.u16, 16)
    | "uint32" => pure (`Lit.u32, 32) | "uint64" => pure (`Lit.u64, 64)
    | _ => jerr s!"unknown literal kind {kind}"
  unless n < 2 ^ bits do jerr s!"literal {n} out of range for {kind}"
  `($(V' ctor) $(quote n))

def ctorTag (types : Array TypeRow) (ty? : Option String) (ctor : String) : CommandElabM Nat := do
  for t in types do
    if ty?.all (· == t.name) then
      for (c, tag, _) in t.ctors do
        if c == ctor then return tag
  jerr s!"constructor {ctor} not found in the type table"

def natList (xs : Array Nat) : CommandElabM Term := do
  let xs := xs.map fun n => (quote n : Term)
  `([$xs,*])

def paramSyntax (j : Json) : CommandElabM Term := do
  let id ← getNat j "id"
  let ty ← getStr j "type"
  `(({ id := $(quote id), ty := $(Syntax.mkStrLit ty) } : $(V' `Param)))

def natArr (j : Json) (k : String) : CommandElabM (Array Nat) := do
  (← getArr j k).mapM fun x => match x.getNat? with
    | .ok n => pure n
    | .error e => jerr s!"bad natural in {k}: {e}"

partial def codeSyntax (types : Array TypeRow) (prims : IO.Ref (Array String)) (j : Json) :
    CommandElabM Term := do
  match ← getStr j "op" with
  | "return" => `($(V' `Code.ret) $(quote (← getNat j "var")))
  | "let" =>
    let p ← paramSyntax (← getObj j "bind")
    let v ← getObj j "value"
    let kind ← getStr v "kind"
    let name ← getStr v "name"
    let args ← natArr v "args"
    let val ← match kind with
      | "primitive" =>
        prims.modify (·.push name)
        `($(V' `LetVal.prim) $(← primSyntax name) $(← natList args))
      | "constructor" =>
        `($(V' `LetVal.ctor) $(Syntax.mkStrLit name) $(quote (← ctorTag types none name)) $(← natList args))
      | "call" => `($(V' `LetVal.call) $(Syntax.mkStrLit name) $(← natList args))
      | k =>
        unless args.isEmpty do jerr s!"literal with arguments"
        let some n := name.toNat? | jerr s!"bad literal {name}"
        `($(V' `LetVal.lit) $(← litSyntax k n))
    let next ← codeSyntax types prims (← getObj j "next")
    `($(V' `Code.bind) $p $val $next)
  | "cases" =>
    let x ← getNat j "var"
    let ty ← getStr j "type"
    let res ← getStr j "result"
    let mut alts ← `($(V' `Alts.nil))
    for a in (← getArr j "alts").reverse do
      let c ← getStr a "constructor"
      let tag ← ctorTag types (some ty) c
      let ps ← (← getArr a "params").mapM paramSyntax
      let body ← codeSyntax types prims (← getObj a "body")
      alts ← `($(V' `Alts.cons) $(Syntax.mkStrLit c) $(quote tag) [$ps,*] $body $alts)
    `($(V' `Code.cases) $(quote x) $(Syntax.mkStrLit ty) $(Syntax.mkStrLit res) $alts)
  | "join" =>
    let jn ← getObj j "join"
    let id ← getNat jn "id"
    let ps ← (← getArr jn "params").mapM paramSyntax
    let body ← codeSyntax types prims (← getObj jn "body")
    let next ← codeSyntax types prims (← getObj j "next")
    `($(V' `Code.join) $(quote id) [$ps,*] $body $next)
  | "jump" =>
    `($(V' `Code.jump) $(quote (← getNatD j "target")) $(← natList (← natArr j "args")))
  | op => jerr s!"unknown op {op}"

syntax (name := validateIRProgram) "validate_ir_program " ident " from " str : command

@[command_elab validateIRProgram] def elabValidateIRProgram : CommandElab := fun stx => do
  let progId : Ident := ⟨stx[1]⟩
  let path : String := (stx[3].isStrLit?).getD ""
  let base := (System.FilePath.mk (← getFileName)).parent.getD "."
  let file := base / path
  let contents ← IO.FS.readFile file
  let json ← match Json.parse contents with
    | .ok j => pure j
    | .error e => jerr s!"{file}: {e}"
  -- type table
  let mut types : Array TypeRow := #[]
  for t in ← getArr json "types" do
    let mut ctors := #[]
    for c in ← getArr t "constructors" do
      let fields ← (← getArr c "fields").mapM fun f => match f.getStr? with
        | .ok s => pure s
        | .error e => jerr s!"bad field type: {e}"
      ctors := ctors.push (← getStr c "name", ← getNat c "tag", fields)
    types := types.push { name := ← getStr t "name", ctors }
  let tyTerms ← types.mapM fun t => do
    let cs ← t.ctors.mapM fun (c, tag, fs) => do
      let fs := fs.map Syntax.mkStrLit
      `(({ name := $(Syntax.mkStrLit c), tag := $(quote tag), fields := [$fs,*] } : $(V' `CtorDecl)))
    `(({ name := $(Syntax.mkStrLit t.name), ctors := [$cs,*] } : $(V' `TypeDecl)))
  let progName := progId.getId
  let typesId := mkIdent (progName ++ `types)
  elabCommand (← `(command| def $typesId : $(V' `TypeTable) := [$tyTerms,*]))
  -- functions
  let prims ← IO.mkRef #[]
  let mut fnIds : Array Ident := #[]
  for f in ← getArr json "functions" do
    let name ← getStr f "name"
    let ps ← (← getArr f "params").mapM paramSyntax
    let result ← getStr f "result"
    let body ← codeSyntax types prims (← getObj f "body")
    let fnId := mkIdent (progName ++ Name.mkSimple name)
    fnIds := fnIds.push fnId
    elabCommand (← `(command|
      def $fnId : $(V' `Function) :=
        { name := $(Syntax.mkStrLit name), params := [$ps,*], result := $(Syntax.mkStrLit result),
          body := $body }))
  elabCommand (← `(command|
    def $progId : $(V' `Program) := { types := $typesId, functions := [$fnIds,*] }))
  -- bind each embedded primitive to its IR name through the semantics' own table
  let mut done : Array String := #[]
  for p in ← prims.get do
    unless done.contains p do
      done := done.push p
      elabCommand (← `(command|
        example : $(V' `Prim.ofName?) $(Syntax.mkStrLit p) = some $(← primSyntax p) := by decide))

end LeanGo.Validate.Meta
