import Lean
import Lean.Compiler.LCNF
import Lean.Compiler.CSimpAttr
import Lean.Compiler.ExternAttr
import Lean.Compiler.ImplementedByAttr
import Lean.Util.CollectAxioms

open Lean Elab Command
open Lean.Compiler.LCNF

#check getBaseDecl?
#check getMonoDecl?
#check DeclValue.forCodeM
#check Code.forM
#check Lean.getExternAttrData?
#check Lean.Compiler.getImplementedBy?
#check Lean.getConstInfoInduct
#check Lean.getConstInfoCtor
#check Lean.collectAxioms

namespace Probe

def reportExtern (data : ExternAttrData) : CoreM Unit := do
  for entry in data.entries do
    match entry with
    | .standard backend symbol => logInfo m!"EXTERN standard backend={backend} symbol={symbol}"
    | .inline backend pattern => logInfo m!"EXTERN inline backend={backend} pattern={pattern}"
    | .adhoc backend => logInfo m!"EXTERN adhoc backend={backend}"
    | .opaque => logInfo "EXTERN opaque: body unavailable"

def auditClaim (name : Name) : CoreM Unit := do
  let info ← getConstInfo name
  logInfo m!"CLAIM {name}: {info.type}"
  let axioms ← collectAxioms name
  logInfo m!"AXIOMS {name}: {axioms}"
  for axiomName in axioms do
    unless [``propext, ``Quot.sound, ``Classical.choice].contains axiomName do
      throwError "Unexpected axiom {axiomName} in {name}"
  logInfo m!"AXIOMS_ACCEPTED {name}: {axioms}"

-- Source dependencies overapproximate original attributes separately from executable stages.
-- CSimp lookup observes mappings in the current environment. It cannot reconstruct
-- earlier scoped rewrites; a general adapter needs compile-time provenance or restrictions.
partial def sourceClosure (pending : List Name) (seen : List Name := []) : CoreM Unit := do
  match pending with
  | [] => pure ()
  | name :: rest =>
      if seen.contains name then sourceClosure rest seen
      else
        let env ← getEnv
        let some info := env.find? name | throwError "Unresolved source dependency {name}"
        logInfo m!"SOURCE {name}"
        let mut replacements := []
        if let some replacement := Compiler.getImplementedBy? env name then
          logInfo m!"SOURCE_REPLACEMENT {name} -> {replacement}"
          replacements := replacement :: replacements
        if let some entry := (Compiler.CSimp.ext.getState env).map.find? name then
          logInfo m!"SOURCE_CSIMP {entry.fromDeclName} -> {entry.toDeclName} theorem={entry.thmName}"
          let theoremInfo ← getConstInfo entry.thmName
          match theoremInfo with
          | .thmInfo _ => auditClaim entry.thmName
          | _ => throwError "Compiler simplification evidence {entry.thmName} is not a theorem"
          replacements := entry.toDeclName :: replacements
        if let some data := getExternAttrData? env name then
          logInfo m!"SOURCE_EXTERN {name}"
          reportExtern data
        let dependencies := match info with
          | .defnInfo value => value.value.getUsedConstants.toList
          | .opaqueInfo value => value.value.getUsedConstants.toList
          | _ => []
        sourceClosure (replacements ++ dependencies ++ rest) (name :: seen)

-- Code.forM includes nested function and join bodies. Empty-argument fvar references
-- and partial constants still require a manual control-flow audit.
def directCalls (decl : Decl .pure) : CoreM (Array Name) := do
  let found ← IO.mkRef (#[] : Array Name)
  decl.value.forCodeM (m := CoreM) fun code =>
    code.forM (m := CoreM) fun node => do
      match node with
      | .let binding _ =>
          match binding.value with
          | .const name .. => found.modify fun names => names.push name
          | .fvar _ args =>
              unless args.isEmpty do throwError "Unresolved indirect call in {decl.name}"
          | _ => pure ()
      | .jmp target _ => logInfo m!"JOIN_EDGE {decl.name} target={target.name}"
      | _ => pure ()
  found.get

-- This is a conservative named-dependency inventory, not a complete call graph
-- or automatic primitive admission. Join edges are local to their declaration.
partial def reportClosure (stage : String) (lookup : Name → CoreM (Option (Decl .pure)))
    (pending : List Name) (seen : List Name := []) : CoreM Unit := do
  match pending with
  | [] => pure ()
  | name :: rest =>
      if seen.contains name then reportClosure stage lookup rest seen
      else
        let seen := name :: seen
        let env ← getEnv
        if let some replacement := Compiler.getImplementedBy? env name then
          logInfo m!"REPLACEMENT {stage} {name} -> {replacement}"
          reportClosure stage lookup (replacement :: rest) seen
        else
          match env.find? name with
          | some (.ctorInfo ctor) =>
              logInfo m!"CONSTRUCTOR {stage} {name} tag={ctor.cidx} fields={ctor.numFields}"
              reportClosure stage lookup rest seen
          | _ =>
              let some decl ← lookup name | throwError "Unresolved {stage} dependency {name}"
              match decl.value with
              | .extern data =>
                  logInfo m!"EXTERNAL_LEAF {stage} {name}"
                  reportExtern data
                  reportClosure stage lookup rest seen
              | .code _ =>
                  logInfo m!"CODE {stage} {name} safe={decl.safe} recursive={decl.recursive}"
                  let calls ← directCalls decl
                  for callee in calls do logInfo m!"EDGE {stage} {name} -> {callee}"
                  reportClosure stage lookup (calls.toList ++ rest) seen

def captureRoot (name : Name) : CoreM Unit := do
  let some base ← getBaseDecl? name | throwError "Missing base-stage root {name}"
  let some mono ← getMonoDecl? name | throwError "Missing mono-stage root {name}"
  match base.value with
  | .code _ => logInfo m!"CAPTURED base {base.name} size={base.size}"
  | .extern _ => throwError "Base root has no implementation body"
  match mono.value with
  | .code _ => logInfo m!"CAPTURED mono {mono.name} size={mono.size}"
  | .extern _ => throwError "Mono root has no implementation body"
  reportClosure "base" getBaseDecl? [name]
  logInfo "CLOSURE_COMPLETE base"
  reportClosure "mono" getMonoDecl? [name]
  logInfo "CLOSURE_COMPLETE mono"

end Probe
