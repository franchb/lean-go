"""Bounded source programs and an independent mathematical unsigned model.

This module deliberately has no dependency on extraction IR, compiler output or
the Go emitter. Serialized programs are the reproducible input to the campaign.
"""
from dataclasses import dataclass, replace
import random


SCHEMA = "lean-go-differential/v1"
MAX_NODES = 2048
MAX_DEPTH = 24
MAX_LITERAL = (1 << 65) - 1
_ARITY = {
    "a": 0, "b": 0, "lit": 0, "add": 2, "sub": 2, "mul": 2,
    "shl": 2, "if": 3, "helper": 2, "wrap": 1, "record": 2,
    "update": 3, "eq": 2, "lt": 2, "le": 2,
}
_PREDICATES = frozenset(("eq", "lt", "le"))


@dataclass(frozen=True)
class Expr:
    op: str
    args: tuple["Expr", ...] = ()
    value: int = 0


@dataclass(frozen=True)
class Program:
    seed: int
    width: int
    expr: Expr
    helper: Expr


def _integer(value, label, maximum):
    if type(value) is not int or not 0 <= value <= maximum:
        raise ValueError(f"{label} must be an integer in 0..{maximum}")


def _width(width):
    if type(width) is not int or width not in (32, 64):
        raise ValueError("width must be 32 or 64")


def _check_expr(expr, boolean, allow_helper, budget, depth=0):
    if depth > MAX_DEPTH:
        raise ValueError("expression depth limit exceeded")
    budget[0] -= 1
    if budget[0] < 0:
        raise ValueError("expression node limit exceeded")
    if not isinstance(expr, Expr) or type(expr.op) is not str or expr.op not in _ARITY:
        raise ValueError("unknown expression operation")
    if type(expr.args) is not tuple or len(expr.args) != _ARITY[expr.op]:
        raise ValueError("expression arity mismatch")
    _integer(expr.value, "literal", MAX_LITERAL)
    if expr.op != "lit" and expr.value != 0:
        raise ValueError("nonliteral expression has a payload")
    if (expr.op in _PREDICATES) != boolean:
        raise ValueError("expression type mismatch")
    if expr.op == "helper" and not allow_helper:
        raise ValueError("recursive helper is not supported")
    for index, arg in enumerate(expr.args):
        _check_expr(arg, expr.op == "if" and index == 0, allow_helper, budget, depth + 1)


def validate(program):
    """Reject malformed or excessive source models before evaluation/rendering."""
    if not isinstance(program, Program):
        raise ValueError("expected a Program")
    _integer(program.seed, "seed", (1 << 64) - 1)
    _width(program.width)
    budget = [MAX_NODES]
    _check_expr(program.expr, False, True, budget)
    _check_expr(program.helper, False, False, budget)


def _eval(expr, a, b, modulus, width, helper):
    op = expr.op
    if op == "a":
        return a
    if op == "b":
        return b
    if op == "lit":
        return expr.value % modulus
    if op == "if":
        condition = _eval(expr.args[0], a, b, modulus, width, helper)
        return _eval(expr.args[1 if condition else 2], a, b, modulus, width, helper)
    values = [_eval(arg, a, b, modulus, width, helper) for arg in expr.args]
    if op == "helper":
        return _eval(helper, *values, modulus, width, helper)
    if op == "wrap":
        return values[0]
    if op == "update":
        return (values[2] + values[1]) % modulus
    left, right = values
    if op in ("add", "record"):
        return (left + right) % modulus
    if op == "sub":
        return (left - right) % modulus
    if op == "mul":
        return (left * right) % modulus
    if op == "shl":
        return (left * pow(2, right % width)) % modulus
    if op == "eq":
        return left == right
    if op == "lt":
        return left < right
    if op == "le":
        return left <= right
    raise ValueError("unknown expression operation")


def evaluate(program, a, b):
    """Evaluate source semantics with arbitrary precision and explicit reduction."""
    validate(program)
    modulus = 1 << program.width
    _integer(a, "input a", modulus - 1)
    _integer(b, "input b", modulus - 1)
    return _eval(program.expr, a, b, modulus, program.width, program.helper)


def _leaf(rng, width):
    boundary = (0, 1, 2, width - 1, width, width + 1,
                (1 << width) - 1, 1 << width, (1 << width) + 1)
    choice = rng.randrange(4)
    if choice < 2:
        return Expr("a" if choice == 0 else "b")
    return Expr("lit", value=rng.choice(boundary) if choice == 2 else rng.getrandbits(width))


def _generate_expr(rng, width, depth):
    if depth == 0 or rng.randrange(5) == 0:
        return _leaf(rng, width)
    op = rng.choice(("add", "sub", "mul", "shl", "if", "wrap", "record", "update"))
    if op == "if":
        condition = Expr(rng.choice(tuple(sorted(_PREDICATES))),
                         (_generate_expr(rng, width, depth - 1), _leaf(rng, width)))
        return Expr(op, (condition, _generate_expr(rng, width, depth - 1),
                         _generate_expr(rng, width, depth - 1)))
    return Expr(op, tuple(_generate_expr(rng, width, depth - 1) for _ in range(_ARITY[op])))


def generate(seed, width, depth=3):
    """Generate a reproducible bounded composition, with records and direct calls."""
    _integer(seed, "seed", (1 << 64) - 1)
    _width(width)
    _integer(depth, "generation depth", 4)
    rng = random.Random(seed)
    helper = Expr("add", (Expr("sub", (Expr("a"), Expr("b"))),
                          _generate_expr(rng, width, min(depth, 2))))
    branch = Expr("if", (Expr(rng.choice(tuple(sorted(_PREDICATES))), (Expr("a"), Expr("b"))),
                         _generate_expr(rng, width, depth), _generate_expr(rng, width, depth)))
    call = Expr("helper", (branch, _generate_expr(rng, width, depth)))
    record_op = rng.choice(("record", "update"))
    record_args = [Expr("wrap", (_generate_expr(rng, width, depth),))]
    record_args.extend(_generate_expr(rng, width, depth) for _ in range(_ARITY[record_op] - 1))
    operands = [call, Expr(record_op, tuple(record_args))]
    rng.shuffle(operands)
    expr = Expr(rng.choice(("add", "sub", "mul", "shl")), tuple(operands))
    program = Program(seed, width, expr, helper)
    validate(program)
    return program


def inputs(seed, width, count):
    """Return exactly count distinct pairs: boundary pairs, then seeded randoms.

    Count is the total, not the number added after boundaries. The prefix covers
    extrema, unsigned high bits, width-masked shifts and UInt64 values above 2^53.
    """
    _integer(seed, "seed", (1 << 64) - 1)
    _width(width)
    if type(count) is not int or not 1 <= count <= 4096:
        raise ValueError("input count must be in 1..4096")
    maximum = (1 << width) - 1
    high = 1 << (width - 1)
    boundaries = [(0, 0), (maximum, 1), (0, 1), (maximum, maximum),
                  (1, width), (high, 1), (1, width - 1), (1, width + 1),
                  (1, 0), (0, maximum), (high - 1, high), (high, high - 1)]
    if width == 64:
        boundaries.extend(((1 << 53, (1 << 53) + 1), ((1 << 53) + 1, 1 << 53)))
    result = boundaries[:count]
    seen = set(result)
    rng = random.Random(seed ^ 0x494E505554)
    while len(result) < count:
        pair = (rng.getrandbits(width), rng.getrandbits(width))
        if pair not in seen:
            seen.add(pair)
            result.append(pair)
    return result


def _expr_data(expr):
    return {"op": expr.op, "args": [_expr_data(arg) for arg in expr.args], "value": expr.value}


def serialize(program):
    validate(program)
    return {"schema": SCHEMA, "seed": program.seed, "width": program.width,
            "expr": _expr_data(program.expr), "helper": _expr_data(program.helper)}


def _parse_expr(data, budget, depth=0):
    if depth > MAX_DEPTH:
        raise ValueError("expression depth limit exceeded")
    budget[0] -= 1
    if budget[0] < 0:
        raise ValueError("expression node limit exceeded")
    if type(data) is not dict or set(data) != {"op", "args", "value"}:
        raise ValueError("invalid expression fields")
    if type(data["op"]) is not str or data["op"] not in _ARITY:
        raise ValueError("unknown expression operation")
    if type(data["args"]) is not list or len(data["args"]) != _ARITY[data["op"]]:
        raise ValueError("expression arity mismatch")
    return Expr(data["op"], tuple(_parse_expr(arg, budget, depth + 1) for arg in data["args"]), data["value"])


def deserialize(data):
    if type(data) is not dict or set(data) != {"schema", "seed", "width", "expr", "helper"}:
        raise ValueError("invalid program fields")
    if data["schema"] != SCHEMA:
        raise ValueError("unsupported program schema")
    budget = [MAX_NODES]
    program = Program(data["seed"], data["width"], _parse_expr(data["expr"], budget),
                      _parse_expr(data["helper"], budget))
    validate(program)
    return program


def _render(expr, scalar):
    if expr.op in ("a", "b"):
        return expr.op
    if expr.op == "lit":
        return f"({expr.value} : {scalar})"
    args = [_render(arg, scalar) for arg in expr.args]
    if expr.op == "shl":
        return f"({scalar}.shiftLeft {args[0]} {args[1]})"
    operator = {"add": "+", "sub": "-", "mul": "*",
                "eq": "==", "lt": "<", "le": "<="}.get(expr.op)
    if operator is not None:
        body = f"({args[0]} {operator} {args[1]})"
        return f"(decide {body})" if expr.op in ("lt", "le") else body
    if expr.op == "if":
        return f"(if {args[0]} then {args[1]} else {args[2]})"
    if expr.op == "helper":
        return f"(helper {args[0]} {args[1]})"
    if expr.op == "wrap":
        return f"(unbox (Box.mk {args[0]}))"
    state = f"State.mk (Box.mk {args[0]}) {args[1]}"
    if expr.op == "record":
        return f"(readState ({state}))"
    return (f"(let state := {state}; "
            f"let changed := {{ state with inner := Box.mk {args[2]} }}; "
            "readState changed)")


def render_lean(program):
    """Render one ordinary Lean module with a scalar root Generated.run."""
    validate(program)
    scalar = f"UInt{program.width}"
    return ("namespace Generated\n\n"
            f"structure Box where\n  value : {scalar}\n\n"
            f"structure State where\n  inner : Box\n  other : {scalar}\n\n"
            f"@[noinline] def unbox (box : Box) : {scalar} := box.value\n\n"
            f"@[noinline] def readState (state : State) : {scalar} :=\n"
            "  state.inner.value + state.other\n\n"
            f"@[noinline] def helper (a b : {scalar}) : {scalar} :=\n"
            f"  {_render(program.helper, scalar)}\n\n"
            f"def run (a b : {scalar}) : {scalar} :=\n"
            f"  {_render(program.expr, scalar)}\n\n"
            "end Generated\n")


def _measure(expr):
    measures = [_measure(arg) for arg in expr.args]
    return (1 + sum(size for size, _ in measures),
            expr.value + sum(literals for _, literals in measures))


def complexity(program):
    """A well-founded lexicographic metric for shrinking: nodes, literal sum."""
    validate(program)
    main, helper = _measure(program.expr), _measure(program.helper)
    return (main[0] + helper[0], main[1] + helper[1])


def _smaller_exprs(expr):
    if expr.op == "lit":
        for value in sorted({0, 1, expr.value // 2} - {expr.value}):
            if value < expr.value:
                yield Expr("lit", value=value)
    elif expr.op not in _PREDICATES and expr.args:
        yield Expr("lit")
        yield from (arg for arg in expr.args if arg.op not in _PREDICATES)
    for index, arg in enumerate(expr.args):
        for smaller in _smaller_exprs(arg):
            yield replace(expr, args=expr.args[:index] + (smaller,) + expr.args[index + 1:])


def shrink_candidates(program):
    """Yield deterministic, type-valid candidates strictly below complexity()."""
    baseline = complexity(program)
    seen = set()
    for field in ("expr", "helper"):
        for expr in _smaller_exprs(getattr(program, field)):
            candidate = replace(program, **{field: expr})
            if candidate not in seen and complexity(candidate) < baseline:
                seen.add(candidate)
                yield candidate
