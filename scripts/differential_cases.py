"""Compare independent source semantics and reduce reproducible mismatches."""
from dataclasses import dataclass
from itertools import chain
import json
from pathlib import Path

from differential_model import deserialize, evaluate, serialize, shrink_candidates


CHANNELS = ('native', 'raw', 'typed')
CASE_SCHEMA = 'lean-go-differential-case/v1'


def validate_inputs(program, values):
    if not isinstance(values, (list, tuple)) or not 1 <= len(values) <= 4096:
        raise ValueError('inputs must contain 1..4096 pairs')
    pairs = []
    for pair in values:
        if not isinstance(pair, (list, tuple)) or len(pair) != 2:
            raise ValueError('each input must be a pair')
        if any(type(n) is not int or not 0 <= n < 2 ** program.width for n in pair):
            raise ValueError('input is outside its unsigned width')
        pairs.append(tuple(pair))
    return pairs


@dataclass(frozen=True)
class Observation:
    expected: list[int]
    outputs: dict[str, list[int]]
    mismatches: dict[str, list[int]]

    @property
    def signature(self):
        return tuple(name for name in CHANNELS if self.mismatches[name])


def compare(program, pairs, outputs):
    pairs = validate_inputs(program, pairs)
    if not isinstance(outputs, dict) or set(outputs) != set(CHANNELS):
        raise ValueError('backend must report native, raw and typed results')
    expected = [evaluate(program, a, b) for a, b in pairs]
    mismatches = {}
    for name in CHANNELS:
        values = outputs[name]
        if not isinstance(values, list) or len(values) != len(pairs):
            raise ValueError(f'{name}: result count does not match inputs')
        if any(type(n) is not int or not 0 <= n < 2 ** program.width for n in values):
            raise ValueError(f'{name}: invalid unsigned result')
        mismatches[name] = [i for i, (a, b) in enumerate(zip(expected, values)) if a != b]
    return Observation(expected, outputs, mismatches)


def save_case(path, program, pairs, observation=None, error=None):
    """Store full source AST and inputs; seed alone is insufficient for replay."""
    pairs = validate_inputs(program, pairs)
    data = {'schema': CASE_SCHEMA, 'program': serialize(program), 'inputs': pairs}
    if observation is not None:
        data['observation'] = {
            'expected': observation.expected, 'outputs': observation.outputs,
            'mismatches': observation.mismatches,
        }
    if error is not None:
        data['error'] = str(error)
    Path(path).write_text(json.dumps(data, indent=2) + '\n', encoding='utf-8')


def load_case(path):
    path = Path(path)
    if path.stat().st_size > 2_000_000:
        raise ValueError('case artifact exceeds 2 MB')
    data = json.loads(path.read_text(encoding='utf-8'))
    if not isinstance(data, dict) or data.get('schema') != CASE_SCHEMA:
        raise ValueError('unsupported differential case schema')
    required = {'schema', 'program', 'inputs'}
    if not required <= data.keys() or data.keys() - required - {'observation', 'error'}:
        raise ValueError('unexpected differential case fields')
    program = deserialize(data['program'])
    return program, validate_inputs(program, data['inputs'])


def input_candidates(pairs):
    """First isolate a case, then decrease values without changing the width."""
    if len(pairs) > 1:
        for pair in pairs:
            yield [pair]
        middle = len(pairs) // 2
        yield pairs[:middle]
        yield pairs[middle:]
    for i, pair in enumerate(pairs):
        for side, value in enumerate(pair):
            for smaller in sorted({0, 1, value // 2, value & (value - 1)}):
                if smaller >= value:
                    continue
                changed = list(pair)
                changed[side] = smaller
                yield pairs[:i] + [tuple(changed)] + pairs[i + 1:]


@dataclass(frozen=True)
class Reduction:
    program: object
    inputs: list[tuple[int, int]]
    observation: Observation
    attempts: int
    rejected_errors: int


def reduce_case(program, pairs, initial, check, attempts=20):
    """Greedy bounded reduction preserving exactly the original failing channels.

    check returns an Observation. An execution/protocol failure cannot replace a
    semantic mismatch. The caller records any such diagnostics in backend logs.
    This produces a smaller counterexample, not a globally minimal program.
    """
    if type(attempts) is not int or not 0 <= attempts <= 1000:
        raise ValueError('shrink attempts must be in 0..1000')
    pairs = validate_inputs(program, pairs)
    if not initial.signature:
        raise ValueError('cannot shrink a passing case')
    signature = initial.signature
    tried = errors = 0
    current = initial
    while tried < attempts:
        candidates = ((program, ps) for ps in input_candidates(pairs))
        # Keep input reduction before source reduction; both strictly decrease.
        candidates = chain(candidates, ((p, pairs) for p in shrink_candidates(program)))
        accepted = False
        for candidate, values in candidates:
            if tried >= attempts:
                break
            tried += 1
            try:
                observation = check(candidate, values)
            except (RuntimeError, ValueError, OSError):
                errors += 1
                continue
            if observation.signature == signature:
                program, pairs, current = candidate, values, observation
                accepted = True
                break
        if not accepted:
            break
    return Reduction(program, pairs, current, tried, errors)
