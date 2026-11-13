import copy
from dataclasses import replace
import json
import unittest

from differential_model import (
    Expr, Program, MAX_DEPTH, MAX_NODES, complexity, deserialize, evaluate,
    generate, inputs, render_lean, serialize, shrink_candidates,
)


A, B = Expr("a"), Expr("b")


def literal(value):
    return Expr("lit", value=value)


def program(expr, width=32, helper=None):
    return Program(0, width, expr, helper or Expr("add", (A, B)))


class MathematicalModelTests(unittest.TestCase):
    def test_unsigned_arithmetic_wraps_at_each_width(self):
        for width in (32, 64):
            maximum = (1 << width) - 1
            for op, a, b, expected in (
                ("add", maximum, 1, 0), ("sub", 0, 1, maximum),
                ("mul", maximum, maximum, 1), ("mul", 1 << (width - 1), 2, 0),
            ):
                with self.subTest(width=width, op=op):
                    self.assertEqual(evaluate(program(Expr(op, (A, B)), width), a, b), expected)

    def test_shift_counts_are_reduced_before_mathematical_multiplication(self):
        for width in (32, 64):
            expression = program(Expr("shl", (A, B)), width)
            for shift, expected in ((0, 1), (width - 1, 1 << (width - 1)),
                                    (width, 1), (width + 1, 2)):
                self.assertEqual(evaluate(expression, 1, shift), expected)
            self.assertEqual(evaluate(expression, 2, width - 1), 0)

    def test_comparisons_are_unsigned_and_feed_selected_expression(self):
        for width in (32, 64):
            maximum = (1 << width) - 1
            for op, left, right, expected in (
                ("lt", maximum, 0, 9), ("lt", 0, maximum, 7),
                ("le", maximum, maximum, 7), ("eq", maximum, maximum, 7),
                ("eq", maximum, 0, 9),
            ):
                expression = Expr("if", (Expr(op, (A, B)), literal(7), literal(9)))
                self.assertEqual(evaluate(program(expression, width), left, right), expected)

    def test_literals_reduce_exactly_without_float_conversion(self):
        self.assertEqual(evaluate(program(literal((1 << 32) + 5)), 0, 0), 5)
        self.assertEqual(evaluate(program(literal((1 << 64) + 5), 64), 0, 0), 5)
        value = (1 << 53) + 1
        self.assertEqual(evaluate(program(literal(value), 64), 0, 0), value)

    def test_records_update_and_helper_argument_binding(self):
        expr = Expr("helper", (Expr("record", (Expr("wrap", (A,)), B)),
                               Expr("update", (A, B, literal(10)))))
        # helper arguments are 3+4 and updated.inner(10)+other(4), not original a,b.
        helper = Expr("sub", (B, A))
        self.assertEqual(evaluate(program(expr, helper=helper), 3, 4), 7)

    def test_invalid_inputs_do_not_silently_wrap(self):
        for value in (-1, 1 << 32, True, 1.0, "1"):
            with self.subTest(value=value), self.assertRaises(ValueError):
                evaluate(program(A), value, 0)


class GeneratedProgramTests(unittest.TestCase):
    def test_deterministic_bounded_varied_programs(self):
        sources = set()
        for width in (32, 64):
            for seed in range(20):
                generated = generate(seed, width)
                self.assertEqual(generated, generate(seed, width))
                self.assertEqual(generated, deserialize(json.loads(json.dumps(serialize(generated)))))
                self.assertLessEqual(complexity(generated)[0], MAX_NODES)
                for a, b in inputs(seed, width, 32):
                    self.assertTrue(0 <= evaluate(generated, a, b) < 1 << width)
                sources.add(render_lean(generated))
        self.assertEqual(len(sources), 40)

    def test_depth_limits_and_argument_validation(self):
        for depth in (0, 4):
            self.assertLessEqual(complexity(generate(1, 64, depth))[0], MAX_NODES)
        for args in ((-1, 32, 3), (True, 32, 3), (0, 16, 3), (0, 32, -1), (0, 32, 5)):
            with self.subTest(args=args), self.assertRaises(ValueError):
                generate(*args)

    def test_input_prefix_and_exact_count(self):
        for width in (32, 64):
            pairs = inputs(123, width, 32)
            self.assertEqual(pairs, inputs(123, width, 32))
            self.assertEqual(len(pairs), 32)
            self.assertEqual(len(set(pairs)), 32)
            self.assertIn(((1 << width) - 1, 1), pairs[:4])
            self.assertIn((1, width), pairs[:8])
            self.assertIn((1, width + 1), pairs[:8])
            if width == 64:
                self.assertIn(((1 << 53) + 1, 1 << 53), pairs[:16])
            self.assertEqual(inputs(123, width, 1), [(0, 0)])
        for count in (0, 4097, True, 1.0):
            with self.assertRaises(ValueError):
                inputs(0, 32, count)

    def test_lean_rendering_keeps_named_records_and_data_dependent_call(self):
        expr = Expr("helper", (Expr("if", (Expr("lt", (A, B)), A, B)),
                               Expr("update", (Expr("wrap", (A,)), B, literal(5)))))
        source = render_lean(program(expr, 64))
        for fragment in ("namespace Generated", "structure Box", "inner : Box",
                         "@[noinline] def helper", "def run (a b : UInt64)",
                         "(decide (a < b))", "{ state with inner := Box.mk (5 : UInt64) }",
                         "(helper (if", "(unbox (Box.mk a))", "readState changed",
                         "@[noinline] def readState", "@[noinline] def unbox"):
            self.assertIn(fragment, source)
        self.assertNotIn("Float", source)

    def test_shift_rendering_uses_ordinary_notation(self):
        for width in (32, 64):
            source = render_lean(program(Expr("shl", (A, B)), width))
            self.assertIn("(a <<< b)", source)
            self.assertNotIn(f"UInt{width}.shiftLeft", source)


class SerializationTests(unittest.TestCase):
    def test_exact_keys_and_schema_required(self):
        valid = serialize(program(A))
        for field, value in (("schema", "future"), ("seed", True), ("width", 8)):
            data = dict(valid, **{field: value})
            with self.subTest(field=field), self.assertRaises(ValueError):
                deserialize(data)
        with self.assertRaises(ValueError):
            deserialize(dict(valid, unexpected=1))
        data = copy.deepcopy(valid)
        data["expr"]["unexpected"] = 1
        with self.assertRaises(ValueError):
            deserialize(data)

    def test_bad_arity_types_literals_and_recursive_helper_rejected(self):
        expressions = (
            Expr("add", (A,)), Expr("add", (Expr("eq", (A, B)), B)),
            Expr("if", (A, A, B)), Expr("eq", (A, B)), Expr("unknown"),
            Expr("lit", value=True), Expr("lit", value=-1),
            Expr("lit", value=1 << 65), Expr("a", value=1),
        )
        for expression in expressions:
            with self.subTest(expression=expression), self.assertRaises(ValueError):
                serialize(program(expression))
        with self.assertRaisesRegex(ValueError, "recursive helper"):
            serialize(program(A, helper=Expr("helper", (A, B))))

    def test_parse_limits_apply_before_construction(self):
        data = serialize(program(A))
        deep = {"op": "a", "args": [], "value": 0}
        for _ in range(MAX_DEPTH + 1):
            deep = {"op": "wrap", "args": [deep], "value": 0}
        data["expr"] = deep
        with self.assertRaisesRegex(ValueError, "depth"):
            deserialize(data)
        wide = {"op": "a", "args": [], "value": 0}
        for _ in range(11):
            wide = {"op": "add", "args": [wide, wide], "value": 0}
        data["expr"] = wide
        with self.assertRaisesRegex(ValueError, "node"):
            deserialize(data)


class ShrinkingTests(unittest.TestCase):
    def test_every_candidate_is_valid_unique_and_strictly_smaller(self):
        original = generate(7, 64, 2)
        candidates = list(shrink_candidates(original))
        self.assertTrue(candidates)
        self.assertEqual(len(candidates), len(set(candidates)))
        self.assertEqual(candidates, list(shrink_candidates(original)))
        for candidate in candidates:
            self.assertLess(complexity(candidate), complexity(original))
            self.assertEqual(candidate, deserialize(serialize(candidate)))

    def test_can_reduce_both_helper_and_literal_payload(self):
        original = program(literal(100), helper=Expr("add", (A, B)))
        candidates = list(shrink_candidates(original))
        self.assertIn(replace(original, expr=literal(0)), candidates)
        self.assertIn(replace(original, expr=literal(50)), candidates)
        self.assertIn(replace(original, helper=A), candidates)
        self.assertEqual(list(shrink_candidates(program(A, helper=B))), [])


if __name__ == "__main__":
    unittest.main()
