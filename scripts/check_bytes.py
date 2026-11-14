#!/usr/bin/env python3
"""Fresh byte extraction compared with native Lean and an arithmetic bit model."""
import argparse
import json
import os
from pathlib import Path
import random
import sys

from differential_backend import Backend, BackendError, checked_values
from export import TOOLCHAIN

ROOT = Path(__file__).resolve().parents[1]
ROOTS = ('calculate', 'choose', 'narrow', 'widen')


def samples():
    boundary = [0, 1, 2, 7, 8, 9, 31, 32, 63, 64, 127, 128, 170, 254, 255]
    words = [0, 1, 255, 256, 257, 65535, 65536, 2147483648, 4294967295]
    values = [(a, b, words[(i * len(boundary) + j) % len(words)])
              for i, a in enumerate(boundary) for j, b in enumerate(boundary)]
    rng = random.Random(825632)
    values += [(rng.randrange(256), rng.randrange(256), rng.randrange(2**32)) for _ in range(128)]
    return values


def model(a, b, word):
    # Boolean truth tables and positional arithmetic, independently of target
    # bit operators, casts and fixed-width machine arithmetic.
    left, right = [(a // 2**i) % 2 for i in range(8)], [(b // 2**i) % 2 for i in range(8)]
    both = sum(x * y * 2**i for i, (x, y) in enumerate(zip(left, right, strict=True)))
    either = sum(min(1, x + y) * 2**i for i, (x, y) in enumerate(zip(left, right, strict=True)))
    different = sum(((x + y) % 2) * 2**i for i, (x, y) in enumerate(zip(left, right, strict=True)))
    total = (a + b) % 256
    return [total, (a - b) % 256, (a * b) % 256, (a * 2**(b % 8)) % 256,
            both, either, different, 255 - a, a // 2**(b % 8),
            int(a == b), int(a < b), int(a <= b), a, word % 256, 255, 1,
            total, word % 256, a]


def reference_source(inputs):
    triples = ',\n  '.join(f'({a}, {b}, {word})' for a, b, word in inputs)
    return '''import Bytes

def samples : List (UInt8 × UInt8 × UInt32) := [
  ''' + triples + ''']

def main : IO Unit := do
  let rows := samples.map fun (a, b, word) =>
    let r := Bytes.calculate a b word
    let values := [r.add.toNat, r.sub.toNat, r.mul.toNat, r.left.toNat,
      r.and.toNat, r.or.toNat, r.xor.toNat, r.not.toNat, r.right.toNat,
      r.eq.toNat, r.lt.toNat, r.le.toNat, r.wide.toNat, r.narrow.toNat,
      r.maximum.toNat, r.wrapped.toNat, (Bytes.choose (a < b) a b).toNat,
      (Bytes.narrow word).toNat, (Bytes.widen a).toNat]
    "[" ++ String.intercalate "," (values.map toString) ++ "]"
  IO.println ("[" ++ String.intercalate "," rows ++ "]")
'''


CONSUMER = r'''package main
import("encoding/json";"errors";"os";x "byteconsumer/extracted")
func boolean(b bool)uint64{if b{return 1};return 0}
func typed(row [3]uint32,l x.Limits)([]uint64,error){
 a,b,n:=uint8(row[0]),uint8(row[1]),row[2]
 r,e:=x.RunCalculate(a,b,n,l);if e!=nil{if r!=(x.Result{}){panic("partial typed record")};return nil,e}
 out:=[]uint64{}
 for _,f:=range []func()(uint8,bool){r.Add,r.Sub,r.Mul,r.Left,r.And,r.Or,r.Xor,r.Not,r.Right}{v,ok:=f();if !ok{panic("byte accessor")};out=append(out,uint64(v))}
 for _,f:=range []func()(bool,bool){r.Eq,r.Lt,r.Le}{v,ok:=f();if !ok{panic("bool accessor")};out=append(out,boolean(v))}
 wide,ok:=r.Wide();if !ok{panic("wide accessor")};out=append(out,uint64(wide))
 for _,f:=range []func()(uint8,bool){r.Narrow,r.Maximum,r.Wrapped}{v,ok:=f();if !ok{panic("literal accessor")};out=append(out,uint64(v))}
 choice,e:=x.RunChoose(a<b,a,b,l);if e!=nil{return nil,e};narrow,e:=x.RunNarrow(n,l);if e!=nil{return nil,e};widen,e:=x.RunWiden(a,l);if e!=nil{return nil,e}
 return append(out,uint64(choice),uint64(narrow),uint64(widen)),nil
}
func arguments(row [3]uint32)[]x.Value{
 args:=[]x.Value{};for _,in:=range []*x.Input{{Type:"UInt8",UInt8:uint8(row[0])},{Type:"UInt8",UInt8:uint8(row[1])},{Type:"UInt32",UInt32:row[2]}}{v,e:=x.Import(in,x.DefaultLimits());if e!=nil{panic(e)};args=append(args,v);in.UInt8=42;in.UInt32=42};return args
}
func raw(row [3]uint32,l x.Limits)([]uint64,error){
 args:=arguments(row);v,e:=x.RunRoot("Bytes.calculate",args,l);if e!=nil{if v.Type()!=""{panic("partial raw record")};return nil,e}
 out:=[]uint64{};for i,f:=range v.Fields(){
  if i>=9&&i<=11{switch f.Constructor(){case "Bool.false":out=append(out,0);case "Bool.true":out=append(out,1);default:panic("bool shape")};continue}
  if i==12{n,ok:=f.UInt32();if !ok{panic("wide shape")};if _,ok=f.UInt8();ok{panic("inactive byte payload")};out=append(out,uint64(n));continue}
  n,ok:=f.UInt8();if !ok{panic("byte shape")};if _,ok=f.UInt32();ok{panic("inactive small payload")};if _,ok=f.UInt64();ok{panic("inactive wide payload")};out=append(out,uint64(n))
 }
 flag:="Bool.false";if row[0]<row[1]{flag="Bool.true"};b,e:=x.Import(&x.Input{Type:"Bool",Constructor:flag},x.DefaultLimits());if e!=nil{panic(e)}
 choice,e:=x.RunRoot("Bytes.choose",[]x.Value{b,args[0],args[1]},l);if e!=nil{return nil,e};c,ok:=choice.UInt8();if !ok{panic("choice type")}
 narrow,e:=x.RunRoot("Bytes.narrow",args[2:],l);if e!=nil{return nil,e};n,ok:=narrow.UInt8();if !ok{panic("narrow type")}
 wide,e:=x.RunRoot("Bytes.widen",args[:1],l);if e!=nil{return nil,e};w,ok:=wide.UInt32();if !ok{panic("wide type")}
 return append(out,uint64(c),uint64(n),uint64(w)),nil
}
func budget(row [3]uint32)[]int{
 out:=[]int{};for dimension:=0;dimension<2;dimension++{found:=0;for bound:=1;bound<=64;bound++{l:=x.DefaultLimits();if dimension==0{l.MaxAllocations=uint64(bound)}else{l.MaxSteps=uint64(bound)};r,e:=raw(row,l);t,te:=typed(row,l);if e==nil{if te!=nil||len(t)!=len(r){panic("budget divergence")};found=bound;break};if !errors.Is(e,x.ErrResource)||!errors.Is(te,x.ErrResource)||r!=nil||t!=nil{panic("partial budget result")}};if found==0{panic("budget exceeded search")};out=append(out,found)};return out
}
func main(){var inputs [][3]uint32;if e:=json.NewDecoder(os.Stdin).Decode(&inputs);e!=nil{panic(e)};result:=struct{Raw,Typed [][]uint64;Budgets []int}{};for _,row:=range inputs{r,e:=raw(row,x.DefaultLimits());if e!=nil{panic(e)};t,e:=typed(row,x.DefaultLimits());if e!=nil{panic(e)};result.Raw=append(result.Raw,r);result.Typed=append(result.Typed,t)};result.Budgets=budget(inputs[len(inputs)-1]);if e:=json.NewEncoder(os.Stdout).Encode(result);e!=nil{panic(e)}}
'''


def check(work, repo=ROOT):
    work = Path(work).resolve()
    work.mkdir(parents=True, exist_ok=False)
    backend = Backend(repo, work)
    backend.last_work = work
    backend._preflight()
    inputs = samples()
    source = (repo / 'compatibility/corpus/bytes/source/Bytes.lean').read_text()
    source_dir, native, consumer = (work / name for name in ('source', 'native', 'consumer'))
    for directory in (source_dir, native, consumer):
        directory.mkdir()
    for directory in (source_dir, native):
        (directory / 'Bytes.lean').write_text(source)
    (native / 'Reference.lean').write_text(reference_source(inputs))
    (native / 'lean-toolchain').write_bytes(TOOLCHAIN)
    (native / 'lakefile.toml').write_text('name = "byte_reference"\nversion = "0.0.0"\n'
                                       '[[lean_lib]]\nname = "Bytes"\n'
                                       '[[lean_exe]]\nname = "reference"\nroot = "Reference"\n')
    (work / 'inputs.json').write_text(json.dumps(inputs) + '\n')
    (work / 'toolchain.json').write_text(json.dumps(backend._versions, indent=2) + '\n')
    backend._run('native-build', [backend._tools['lake'], 'build', 'reference'], native)
    suffix = '.exe' if os.name == 'nt' else ''
    native_output = backend._run('native-run', [str(native / '.lake/build/bin' / ('reference' + suffix))], native)
    roots = [part for name in ROOTS for part in ('--root', 'Bytes.' + name)]
    backend._run('extract', [sys.executable, str(repo / 'scripts/generate.py'), '--source-dir', str(source_dir),
                            '--module', 'Bytes', *roots, '--out', str(consumer / 'extracted'),
                            '--ir-output', str(work / 'program.ir.json')], repo)
    (consumer / 'go.mod').write_text('module byteconsumer\n\ngo 1.27.4\n')
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
            checked_values(row, 19, 32, label)
            if row != results['model'][i]:
                raise BackendError(label, f'case {i} disagrees with arithmetic model')
    ir = json.loads((work / 'program.ir.json').read_bytes())
    if (ir['schema'], ir['profile']) != ('lean-go-ir/v13', 'pure-module/v11'):
        raise BackendError('extract', 'wrong byte schema')
    report = {'status': 'passed', 'cases': len(inputs), 'roots': list(ROOTS), 'budgets': go_values['Budgets'],
              'assurance': 'Bounded differential testing; not a proof of compiler or universal Go correctness.'}
    (work / 'results.json').write_text(json.dumps(results, indent=2) + '\n')
    (work / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
    return report


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--artifacts', type=Path, required=True)
    print(json.dumps(check(parser.parse_args().artifacts), indent=2))
