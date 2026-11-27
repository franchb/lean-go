"""The complete unsigned matrix: every operation of UInt8/16/32/64 and all
twelve conversions, compiler rewrites of the new operations, rejections that
must stay, and a three-way comparison of native Lean, both generated Go APIs
and an arithmetic model on boundary values."""
import json
import os
from pathlib import Path
import subprocess
import sys
import unittest

import export_module as producer
import test_uint64_admission as wide

WIDTHS = (8, 16, 32, 64)
OPERATIONS = ('add', 'sub', 'mul', 'div', 'mod', 'land', 'lor', 'xor', 'complement',
              'shiftLeft', 'shiftRight', 'decEq', 'decLt', 'decLe')
MATRIX = {f'UInt{w}.{op}' for w in WIDTHS for op in OPERATIONS} | {
    f'UInt{w}.toUInt{v}' for w in WIDTHS for v in WIDTHS if v != w}
FIELDS = ('add', 'sub', 'mul', 'div', 'mod', 'and', 'or', 'xor', 'not', 'shl', 'shr', 'eq', 'lt', 'le')
NOTATION = ('a + b', 'a - b', 'a * b', 'a / b', 'a % b', 'a &&& b', 'a ||| b', 'a ^^^ b', '~~~a',
            'a <<< b', 'a >>> b', 'a == b', 'decide (a < b)', 'decide (a ≤ b)')


def targets(width):
    return [v for v in WIDTHS if v != width]


def matrix_source(direct=False):
    """One root per width returning every operation and conversion in a record."""
    lines = []
    for w in WIDTHS:
        t = f'UInt{w}'
        lines.append(f'structure Ops{w} where')
        for field in FIELDS:
            lines.append(f'  {field} : {"Bool" if field in ("eq", "lt", "le") else t}')
        for v in targets(w):
            lines.append(f'  to{v} : UInt{v}')
        if direct:
            values = [f'{t}.{op} a' + ('' if op == 'complement' else ' b') for op in OPERATIONS[:11]]
            values += [f'@decide (a = b) ({t}.decEq a b)', f'@decide (a < b) ({t}.decLt a b)',
                       f'@decide (a ≤ b) ({t}.decLe a b)']
            values += [f'{t}.toUInt{v} a' for v in targets(w)]
        else:
            values = list(NOTATION) + [f'a.toUInt{v}' for v in targets(w)]
        lines.append(f'def ops{w} (a b : {t}) : Ops{w} :=\n  ⟨{", ".join(values)}⟩')
    return '\n'.join(lines) + '\n'


def model(width, a, b):
    """Arithmetic only: Lean's definitions on natural numbers."""
    m = 1 << width
    bit = lambda f: sum(f((a >> i) & 1, (b >> i) & 1) << i for i in range(width))
    return [(a + b) % m, (a - b) % m, (a * b) % m, a // b if b else 0, a % b if b else a,
            bit(lambda x, y: x * y), bit(lambda x, y: max(x, y)), bit(lambda x, y: (x + y) % 2),
            m - 1 - a, a * 2 ** (b % width) % m, a // 2 ** (b % width),
            int(a == b), int(a < b), int(a <= b)] + [a % (1 << v) for v in targets(width)]


def boundary(width):
    maximum = (1 << width) - 1
    values = {0, 1, 2, 3, 7, width - 1, width, width + 1, 2 * width, 255 & maximum,
              maximum, maximum - 1, 1 << (width - 1), (1 << (width - 1)) - 1}
    values |= {1 << k for k in range(0, width, 3)}
    return sorted(values)


@unittest.skipUnless(os.environ.get('LEAN_GO_MODULES') == '1', 'set LEAN_GO_MODULES=1 for fresh Lean builds')
class UnsignedMatrixTests(unittest.TestCase):
    setUp = wide.UInt64AdmissionTests.setUp
    export = wide.UInt64AdmissionTests.export
    assert_rejected = wide.UInt64AdmissionTests.assert_rejected
    imported_attribute_probe = staticmethod(wide.UInt64AdmissionTests.imported_attribute_probe)

    def export_roots(self, body, roots):
        (self.source / 'Numeric.lean').write_text('namespace Numeric\n' + body + 'end Numeric\n')
        producer.export_module(self.root, self.source, 'Numeric', [f'Numeric.{r}' for r in roots], self.output)
        return json.loads(self.output.read_bytes())

    def test_ordinary_notation_admits_every_primitive_of_every_width(self):
        for direct in (False, True):
            with self.subTest(direct=direct):
                program = self.export_roots(matrix_source(direct), [f'ops{w}' for w in WIDTHS])
                self.assertEqual(producer.emitted_primitives(program), MATRIX)
                self.assertEqual(program['features'], ['bitwise', 'division', 'uint16', 'uint32', 'uint64',
                                                       'uint8', 'width_conversion'])
                self.assertEqual(program['rewrites'], [])
                provenance = {item['name'] for item in program['provenance']}
                if not direct:
                    for w in WIDTHS:
                        self.assertLessEqual({f'instDivUInt{w}', f'instModUInt{w}', f'instAndOpUInt{w}',
                                              f'instComplementUInt{w}', f'instLinearOrderPackageUInt{w}'},
                                             provenance)

    def test_compiler_rewrites_of_division_remainder_and_multiplication(self):
        source = '''def scaled (a : UInt16) : UInt16 := a * 8
def quotient (a : UInt8) : UInt8 := a / 8
def remainder (a : UInt64) : UInt64 := a % 1024
def byZero (a : UInt32) : UInt32 := a / 0
def modZero (a : UInt32) : UInt32 := a % 0
def byOne (a : UInt16) : UInt16 := a / 1
def modOne (a : UInt16) : UInt16 := a % 1
def wrapped (a : UInt8) : UInt8 := a / 256
def odd (a : UInt32) : UInt32 := a / 3
def top (a : UInt64) : UInt64 := a / 9223372036854775808
def numerator (b : UInt16) : UInt16 := 0 / b
def folded : UInt16 := 100 / 7 + 100 % 0
'''
        roots = ['byOne', 'byZero', 'folded', 'modOne', 'modZero', 'numerator', 'odd', 'quotient',
                 'remainder', 'scaled', 'top', 'wrapped']
        program = self.export_roots(source, roots)
        shapes = {}
        for function in program['functions']:
            code, steps = function['body'], []
            while code['op'] == 'let':
                value = code['value']
                steps.append((value['kind'], value['name']))
                code = code['next']
            shapes[function['name'].removeprefix('Numeric.')] = steps
        self.assertEqual(shapes, {
            'scaled': [('uint16', '3'), ('primitive', 'UInt16.shiftLeft')],
            'quotient': [('uint8', '3'), ('primitive', 'UInt8.shiftRight')],
            'remainder': [('uint64', '1024'), ('primitive', 'UInt64.mod')],
            'byZero': [('uint32', '0'), ('primitive', 'UInt32.div')],
            'modZero': [], 'byOne': [], 'modOne': [('uint16', '0')],
            # 256 wraps to the UInt8 literal 0, so the division stays and yields 0.
            'wrapped': [('uint8', '0'), ('primitive', 'UInt8.div')],
            'odd': [('uint32', '3'), ('primitive', 'UInt32.div')],
            'top': [('uint64', '63'), ('primitive', 'UInt64.shiftRight')],
            'numerator': [('uint16', '0')],
            # 100 / 7 + 100 % 0 = 14 + 100: Lean's own folding is exact.
            'folded': [('uint16', '114')],
        })

    def test_erased_division_is_audited_but_not_emitted(self):
        for w in WIDTHS:
            with self.subTest(width=w):
                program = self.export(f'def run (a b : UInt{w}) :=\n  let _unused := a / b + a % b\n  a\n')
                self.assertFalse(producer.emitted_primitives(program))
                self.assertEqual(program['features'], [f'uint{w}'])
                provenance = {item['name'] for item in program['provenance']}
                self.assertLessEqual({f'instDivUInt{w}', f'instModUInt{w}'}, provenance)

    def test_division_rewrite_history_and_wrapper_replacement_reject(self):
        wide.check_primitive_history(self, [('UInt16.div', 'UInt16', False), ('UInt8.mod', 'UInt8', False)],
                                     'division')
        prelude = ('def replacement : Mod UInt64 := instModUInt64\n'
                   'theorem replacement_eq : instModUInt64 = replacement := rfl\n')
        call = 'def run (a b : UInt64) := a % b\n'
        erased = 'def run (a b : UInt64) :=\n  let _unused := a % b\n  a\n'
        for source in wide.rewrite_sources(prelude, call, erased):
            self.assert_rejected(source, 'rewrite|replacement|repeated compilation')

    def test_changed_division_instance_or_primitive_attributes_reject(self):
        for name, body in (('instDivUInt32', 'def run (a b : UInt32) := a / b\n'),
                           ('UInt8.div', 'def run (a b : UInt8) := UInt8.div a b\n'),
                           ('UInt16.toUInt64', 'def run (a : UInt16) := a.toUInt64\n')):
            with self.subTest(name=name):
                probe = self.imported_attribute_probe('externAttr', name,
                                                      '{ entries := [.standard `all "untrusted"] }')
                self.assert_rejected(body, 'Numeric source extern changed|numeric primitive', probe=probe)

    def test_signed_natural_platform_and_float_numbers_stay_rejected(self):
        cases = {
            'Int8': 'def run (a b : Int8) := a / b\n',
            'Int16': 'def run (a b : Int16) := a + b\n',
            'Int32': 'def run (a b : Int32) := a % b\n',
            'Int64': 'def run (a b : Int64) := a &&& b\n',
            'Nat': 'def run (a b : Nat) := a / b\n',
            'Nat helper': 'def run (a b : UInt32) : UInt32 := (a.toNat / b.toNat).toUInt32\n',
            'USize': 'def run (a b : USize) := a / b\n',
            'USize conversion': 'def run (a : UInt64) := a.toUSize\n',
            'Float': 'def run (a b : Float) := a / b\n',
            'Float conversion': 'def run (a : UInt16) := a.toFloat\n',
        }
        for name, body in cases.items():
            with self.subTest(case=name):
                self.assert_rejected(body, 'source audit|Source audit|unsupported|Unsupported|extern source dependency')

    def test_native_lean_both_go_apis_and_model_agree_on_boundaries(self):
        compiler = producer.selected_compiler()
        env = dict(os.environ, ELAN_TOOLCHAIN=compiler.toolchain, CGO_ENABLED='0', GOWORK='off',
                   GOTOOLCHAIN='local', GOFLAGS='', GOPROXY='off')
        source = 'namespace Matrix\n' + matrix_source() + 'end Matrix\n'
        sources, native, consumer = (self.directory / name for name in ('sources', 'native', 'consumer'))
        for directory in (sources, native, consumer):
            directory.mkdir()
        (sources / 'Matrix.lean').write_text(source)
        (native / 'Matrix.lean').write_text(source)
        pairs = {w: [(a, b) for a in boundary(w) for b in boundary(w)] for w in WIDTHS}
        reference = ['import Matrix', 'open Matrix', 'def main : IO Unit := do']
        for w in WIDTHS:
            listed = ', '.join(f'({a}, {b})' for a, b in pairs[w])
            fields = [f'(if r.{f} then 1 else 0)' if f in ('eq', 'lt', 'le') else f'r.{f}.toNat' for f in FIELDS]
            fields += [f'r.to{v}.toNat' for v in targets(w)]
            reference.append(f'  for (a, b) in ([{listed}] : List (UInt{w} × UInt{w})) do')
            reference.append(f'    let r := ops{w} a b')
            reference.append(f'    IO.println (toString [{", ".join(fields)}])')
        (native / 'Reference.lean').write_text('\n'.join(reference) + '\n')
        (native / 'lean-toolchain').write_bytes(compiler.pin_file)
        (native / 'lakefile.toml').write_text('name = "matrix_reference"\nversion = "0.0.0"\n[[lean_lib]]\n'
                                              'name = "Matrix"\n[[lean_exe]]\nname = "reference"\nroot = "Reference"\n')
        producer.run(['lake', 'build', 'reference'], native, env)
        exe = native / '.lake/build/bin' / ('reference.exe' if os.name == 'nt' else 'reference')
        native_rows = [json.loads(line) for line in producer.run([str(exe)], native, env).splitlines()]
        command = [sys.executable, str(self.root / 'scripts/generate.py'), '--source-dir', str(sources),
                   '--module', 'Matrix', '--out', str(consumer / 'extracted'),
                   '--ir-output', str(self.directory / 'matrix.ir.json')]
        for w in WIDTHS:
            command += ['--root', f'Matrix.ops{w}']
        subprocess.run(command, cwd=self.root, check=True, capture_output=True, text=True)
        (consumer / 'go.mod').write_text('module matrixconsumer\n\ngo 1.27.4\n')
        (consumer / 'main.go').write_text(GO_CONSUMER)
        (consumer / 'pairs.json').write_text(json.dumps({str(w): pairs[w] for w in WIDTHS}))
        go_rows = json.loads(producer.run(['go', 'run', '.'], consumer, env))
        expected = [model(w, a, b) for w in WIDTHS for a, b in pairs[w]]
        self.assertEqual(len(native_rows), len(expected))
        self.assertGreater(len(expected), 1000)
        for index, want in enumerate(expected):
            self.assertEqual(native_rows[index], want, index)
            self.assertEqual(go_rows['raw'][index], want, index)
            self.assertEqual(go_rows['typed'][index], want, index)


# The raw API is read generically: every record field is a scalar or a Bool.
# The typed API calls each root and reads the same fields through reflection.
GO_CONSUMER = r'''package main

import (
	"encoding/json"
	"os"
	"reflect"
	"strconv"

	x "matrixconsumer/extracted"
)

func scalar(v x.Value) uint64 {
	if a, ok := v.UInt8(); ok {
		return uint64(a)
	}
	if a, ok := v.UInt16(); ok {
		return uint64(a)
	}
	if a, ok := v.UInt32(); ok {
		return uint64(a)
	}
	if a, ok := v.UInt64(); ok {
		return a
	}
	switch v.Constructor() {
	case "Bool.true":
		return 1
	case "Bool.false":
		return 0
	}
	panic("unexpected field " + v.Type())
}

func input(width int, n uint64) *x.Input {
	switch width {
	case 8:
		return &x.Input{Type: "UInt8", UInt8: uint8(n)}
	case 16:
		return &x.Input{Type: "UInt16", UInt16: uint16(n)}
	case 32:
		return &x.Input{Type: "UInt32", UInt32: uint32(n)}
	}
	return &x.Input{Type: "UInt64", UInt64: n}
}

func typed(width int, a, b uint64, l x.Limits) []uint64 {
	run := reflect.ValueOf(map[int]any{8: x.RunOps8, 16: x.RunOps16, 32: x.RunOps32, 64: x.RunOps64}[width])
	argument := func(n uint64) reflect.Value { return reflect.ValueOf(n).Convert(run.Type().In(0)) }
	out := run.Call([]reflect.Value{argument(a), argument(b), reflect.ValueOf(l)})
	if !out[1].IsNil() {
		panic(out[1].Interface())
	}
	record := out[0]
	var values []uint64
	for _, name := range []string{"Add", "Sub", "Mul", "Div", "Mod", "And", "Or", "Xor", "Not", "Shl", "Shr", "Eq", "Lt", "Le", "To8", "To16", "To32", "To64"} {
		method := record.MethodByName(name)
		if !method.IsValid() {
			continue
		}
		result := method.Call(nil)
		if !result[1].Bool() {
			panic("missing field " + name)
		}
		switch v := result[0].Interface().(type) {
		case bool:
			if v {
				values = append(values, 1)
			} else {
				values = append(values, 0)
			}
		default:
			values = append(values, reflect.ValueOf(v).Convert(reflect.TypeFor[uint64]()).Uint())
		}
	}
	return values
}

func main() {
	data, err := os.ReadFile("pairs.json")
	if err != nil {
		panic(err)
	}
	var pairs map[string][][2]uint64
	if err := json.Unmarshal(data, &pairs); err != nil {
		panic(err)
	}
	l := x.DefaultLimits()
	result := struct{ Raw, Typed [][]uint64 }{}
	for _, width := range []int{8, 16, 32, 64} {
		for _, pair := range pairs[strconv.Itoa(width)] {
			args := make([]x.Value, 2)
			for i, n := range pair {
				if args[i], err = x.Import(input(width, n), l); err != nil {
					panic(err)
				}
			}
			raw, err := x.RunRoot("Matrix.ops"+strconv.Itoa(width), args, l)
			if err != nil {
				panic(err)
			}
			var row []uint64
			for _, field := range raw.Fields() {
				row = append(row, scalar(field))
			}
			result.Raw = append(result.Raw, row)
			result.Typed = append(result.Typed, typed(width, pair[0], pair[1], l))
		}
	}
	out, err := json.Marshal(map[string][][]uint64{"raw": result.Raw, "typed": result.Typed})
	if err != nil {
		panic(err)
	}
	os.Stdout.Write(out)
}
'''


if __name__ == '__main__':
    unittest.main()
