"""Resolve private state once for every checkout of this repository."""
from pathlib import Path
import subprocess


def canonical_local_root() -> Path:
    repo = Path(__file__).resolve().parents[2]
    result = subprocess.run(['git', 'rev-parse', '--path-format=absolute', '--git-common-dir'],
                            cwd=repo, capture_output=True, text=True, check=True)
    return Path(result.stdout.strip()).resolve().parent / '.raptor-local'


def lifecycle_root() -> Path:
    return canonical_local_root() / 'sandbox-lifecycle'
