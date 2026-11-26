#!/usr/bin/env python3
"""Regenerate a copied normalizer demo with an installed CLI and run it with Go alone."""
from pathlib import Path

from check_release_candidate import pinned_go
from export import run
import standalone_demo as shared

ROOT = Path(__file__).resolve().parents[1]
DEMO = Path('examples/proofs/normalizer')
OUTPUT = (
    'normalize(not (x4294967295 and U)) = (not x4294967295 or U)\n'
    'normalize(not not F) = F\n'
    'normalize(not (F or x0)) = (T and not x0)\n'
)
SPEC = shared.Demo('normalizer', 'Normalizer', ('Normalizer.normalize',), (
    ('NormalizerProofs.normalize_is_normal', 'NormalizerContract.normalize_is_normal'),
    ('NormalizerProofs.normalize_preserves_eval', 'NormalizerContract.normalize_preserves_eval'),
))


def artifacts(project):
    return shared.artifacts(project, SPEC.name)


def compare_artifacts(project, candidate):
    shared.compare_artifacts(project, candidate, SPEC.name)


def run_go_demo(project, go, environment, workspace):
    shared.run_go_demo(project, go, environment, workspace, OUTPUT, run)


def verify_demo(cli, workspace, environment):
    shared.verify_demo(cli, workspace, environment, ROOT / DEMO, SPEC,
                       run=run, pinned_go=pinned_go, consumer=run_go_demo)


def main():
    shared.main(verify_demo, SPEC.name)


if __name__ == '__main__':
    main()
