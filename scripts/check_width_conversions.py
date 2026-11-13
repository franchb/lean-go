#!/usr/bin/env python3
"""Bounded mixed-width native Lean/raw Go/typed Go comparison with arithmetic oracles."""
import argparse
import json
import os
from pathlib import Path
import random
import sys

from differential_backend import Backend, BackendError, checked_values
from export import TOOLCHAIN

ROOT = Path(__file__).resolve().parents[1]
ROOTS = ('widen', 'narrow', 'pack', 'high', 'low')
RADIX = 4294967296


def samples():
    small = [0, 1, 2147483647, 2147483648, 4294967294, 4294967295, 2863311530, 1431655765]
    wide = small + [4294967296, 4294967297, 9223372036854775808, 18446744073709551615,
                    12297829382473034410, 6148914691236517205]
    values = [(a, b, wide[(i * len(small) + j) % len(wide)])
              for i, a in enumerate(small) for j, b in enumerate(small)]
    values += [(n % RADIX, n // RADIX, n) for n in wide]
    rng = random.Random(320647)
    values += [(rng.randrange(RADIX), rng.randrange(RADIX), rng.randrange(RADIX * RADIX)) for _ in range(128)]
    return values


def model(a, b, word):
    # Arithmetic alone: no target casts, masks or bitwise shifts.
    return [a, word % RADIX, a * RADIX + b, word // RADIX, word % RADIX]


def reference_source(inputs):
    triples = ',\n  '.join(f'({a}, {b}, {word})' for a, b, word in inputs)
    return '''import Widths

def samples : List (UInt32 × UInt32 × UInt64) := [
  ''' + triples + ''']

def main : IO Unit := do
  let rows := samples.map fun (a, b, word) =>
    let values := [(Widths.widen a).toNat, (Widths.narrow word).toNat,
                   (Widths.pack a b).toNat, (Widths.high word).toNat, (Widths.low word).toNat]
    "[" ++ String.intercalate "," (values.map toString) ++ "]"
  IO.println ("[" ++ String.intercalate "," rows ++ "]")
'''


def trace_source():
    names = ', '.join('``Widths.' + name for name in ROOTS)
    return '''import Widths
import Lean
import Lean.Compiler.LCNF
open Lean Lean.Compiler Lean.Elab.Command
run_elab do
  for name in [''' + names + '''] do
    let some decl ← LCNF.getMonoDecl? name | throwError "missing mono declaration"
    decl.value.forCodeM (m := CoreM) fun code =>
      code.forM (m := CoreM) fun node => do
        match node with
        | .let binding _ =>
          match binding.value with
          | .const callee _ _ _ => logInfo m!"LEAF {name} -> {callee}"
          | .lit _ => pure ()
          | _ => throwError "unexpected value"
        | .return _ => pure ()
        | _ => throwError "unexpected control"
'''


CONSUMER = r'''package main
import (
 "encoding/json"
 "errors"
 "os"
 x "widthconsumer/extracted"
)
var roots=[]string{"widen","narrow","pack","high","low"}
func arguments(root string,row [3]uint64)[]x.Value{
 inputs:=[]*x.Input{{Type:"UInt64",UInt64:row[2]}}
 if root=="widen"||root=="pack"{inputs=[]*x.Input{{Type:"UInt32",UInt32:uint32(row[0])}}}
 if root=="pack"{inputs=append(inputs,&x.Input{Type:"UInt32",UInt32:uint32(row[1])})}
 args:=make([]x.Value,len(inputs));for i,in:=range inputs{v,e:=x.Import(in,x.DefaultLimits());if e!=nil{panic(e)};args[i]=v;in.UInt32=0;in.UInt64=0};return args
}
func typed(root string,row [3]uint64,l x.Limits)(uint64,error){
 switch root{
 case "widen":return x.RunWiden(uint32(row[0]),l)
 case "pack":return x.RunPack(uint32(row[0]),uint32(row[1]),l)
 case "narrow":n,e:=x.RunNarrow(row[2],l);return uint64(n),e
 case "high":n,e:=x.RunHigh(row[2],l);return uint64(n),e
 case "low":n,e:=x.RunLow(row[2],l);return uint64(n),e
 };panic("root")
}
func read(root string,v x.Value)uint64{
 if root=="widen"||root=="pack"{n,ok:=v.UInt64();if !ok{panic("wide payload")};if _,ok:=v.UInt32();ok{panic("inactive payload")};return n}
 n,ok:=v.UInt32();if !ok{panic("narrow payload")};if _,ok:=v.UInt64();ok{panic("inactive payload")};return uint64(n)
}
func budgets(root string,row [3]uint64)[2]int{
 args:=arguments(root,row);found:=[2]int{}
 for dimension:=0;dimension<2;dimension++{
  for bound:=1;bound<=16;bound++{
   l:=x.DefaultLimits();if dimension==0{l.MaxAllocations=uint64(bound)}else{l.MaxSteps=uint64(bound)}
   raw,e:=x.RunRoot("Widths."+root,args,l);got,te:=typed(root,row,l)
   if e==nil{if te!=nil||got!=read(root,raw){panic("typed budget divergence")};found[dimension]=bound;break}
   if !errors.Is(e,x.ErrResource)||raw.Type()!=""||!errors.Is(te,x.ErrResource)||got!=0{panic("partial budget output")}
  }
  if found[dimension]==0{panic("budget search exceeded bound")}
 }
 l:=x.DefaultLimits();l.MaxAllocations=uint64(found[0]);l.MaxSteps=uint64(found[1])
 if _,e:=x.RunRoot("Widths."+root,args,l);e!=nil{panic(e)}
 // Allocation budget zero is invalid, rather than a partially executed result.
 l.MaxAllocations=0
 raw,e:=x.RunRoot("Widths."+root,args,l);got,te:=typed(root,row,l)
 if !errors.Is(e,x.ErrLimits)||raw.Type()!=""||!errors.Is(te,x.ErrLimits)||got!=0{panic("invalid budget output")}
 return found
}
func main(){
 var inputs [][3]uint64;if e:=json.NewDecoder(os.Stdin).Decode(&inputs);e!=nil{panic(e)}
 result:=struct{Raw,Typed [][]uint64;Budgets map[string][2]int}{Budgets:map[string][2]int{}}
 for _,row:=range inputs{
  rawRow,typedRow:=[]uint64{},[]uint64{}
  for _,root:=range roots{
   args:=arguments(root,row);raw,e:=x.RunRoot("Widths."+root,args,x.DefaultLimits());if e!=nil{panic(e)}
   native,e:=typed(root,row,x.DefaultLimits());if e!=nil{panic(e)}
   rawRow=append(rawRow,read(root,raw));typedRow=append(typedRow,native)
  }
  result.Raw=append(result.Raw,rawRow);result.Typed=append(result.Typed,typedRow)
 }
 for _,root:=range roots{result.Budgets[root]=budgets(root,inputs[len(inputs)-1])}
 if e:=json.NewEncoder(os.Stdout).Encode(result);e!=nil{panic(e)}
}
'''


def check(work, repo=ROOT):
    work = Path(work).resolve()
    work.mkdir(parents=True, exist_ok=False)
    backend = Backend(repo, work)
    backend.last_work = work
    backend._preflight()
    inputs = samples()
    source = (repo / 'compatibility/corpus/width_conversions/source/Widths.lean').read_text()
    source_dir, native, consumer = (work / name for name in ('source', 'native', 'consumer'))
    for directory in (source_dir, native, consumer):
        directory.mkdir()
    for directory in (source_dir, native):
        (directory / 'Widths.lean').write_text(source)
    (native / 'Reference.lean').write_text(reference_source(inputs))
    (native / 'Trace.lean').write_text(trace_source())
    (native / 'lean-toolchain').write_bytes(TOOLCHAIN)
    (native / 'lakefile.toml').write_text('name = "width_reference"\nversion = "0.0.0"\n'
                                       '[[lean_lib]]\nname = "Widths"\n'
                                       '[[lean_exe]]\nname = "reference"\nroot = "Reference"\n')
    (work / 'inputs.json').write_text(json.dumps(inputs) + '\n')
    (work / 'toolchain.json').write_text(json.dumps(backend._versions, indent=2) + '\n')
    backend._run('native-build', [backend._tools['lake'], 'build', 'reference'], native)
    trace = backend._run('pure-mono-trace', [backend._tools['lake'], 'env', 'lean', 'Trace.lean'], native)
    (work / 'pure-mono-trace.txt').write_text(trace)
    expected_leaves = {'widen': ['UInt32.toUInt64'], 'narrow': ['UInt64.toUInt32'],
                       'low': ['UInt64.toUInt32'], 'high': ['UInt64.shiftRight', 'UInt64.toUInt32'],
                       'pack': ['UInt32.toUInt64', 'UInt64.shiftLeft', 'UInt32.toUInt64', 'UInt64.lor']}
    for root, names in expected_leaves.items():
        prefix = f'LEAF Widths.{root} -> '
        actual = [line.split(prefix, 1)[1] for line in trace.splitlines() if prefix in line]
        if actual != names:
            raise BackendError('pure-mono-trace', f'{root}: expected {names}, got {actual}')
    suffix = '.exe' if os.name == 'nt' else ''
    native_output = backend._run('native-run', [str(native / '.lake/build/bin' / ('reference' + suffix))], native)
    roots = [part for name in ROOTS for part in ('--root', 'Widths.' + name)]
    backend._run('extract', [sys.executable, str(repo / 'scripts/generate.py'), '--source-dir', str(source_dir),
                            '--module', 'Widths', *roots, '--out', str(consumer / 'extracted'),
                            '--ir-output', str(work / 'program.ir.json')], repo)
    (consumer / 'go.mod').write_text('module widthconsumer\n\ngo 1.27.4\n')
    (consumer / 'main.go').write_text(CONSUMER)
    binary = consumer / ('consumer' + suffix)
    backend._run('go-build', [backend._tools['go'], 'build', '-buildvcs=false', '-o', str(binary), '.'], consumer)
    output = backend._run('go-run', [str(binary)], consumer, data=json.dumps(inputs), go_only=True)
    native_values, go_values = json.loads(native_output), json.loads(output)
    results = {'model': [model(*row) for row in inputs], 'native': native_values,
               'raw': go_values['Raw'], 'typed': go_values['Typed']}
    for label, rows in results.items():
        if len(rows) != len(inputs):
            raise BackendError(label, 'wrong row count')
        for i, row in enumerate(rows):
            checked_values(row, 5, 64, label)
            if row != results['model'][i]:
                raise BackendError(label, f'case {i} disagrees with arithmetic model')
    ir = json.loads((work / 'program.ir.json').read_bytes())
    if (ir['schema'], ir['profile']) != ('lean-go-ir/v11', 'pure-module/v9'):
        raise BackendError('extract', 'wrong mixed-width schema')
    report = {'status': 'passed', 'cases': len(inputs), 'roots': list(ROOTS), 'budgets': go_values['Budgets'],
              'assurance': 'Bounded differential testing; not a proof of compiler or universal Go correctness.'}
    (work / 'results.json').write_text(json.dumps(results, indent=2) + '\n')
    (work / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--artifacts', type=Path, required=True, help='new directory for retained evidence')
    args = parser.parse_args()
    print(json.dumps(check(args.artifacts), indent=2))


if __name__ == '__main__':
    main()
