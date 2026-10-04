"""Only task-bound, bounded answers cross from runner to private local storage."""
from dataclasses import asdict
import hashlib
import json
import os
from pathlib import Path
import tempfile
from .contracts import validate_result, validate_answer_ref
from .state import protect_dir, safe_path
from tools.task_store.models import canonical, TaskBinding, MODEL


class TaskResultSink:
    def __init__(self, root: Path, expected: TaskBinding):
        self.root = protect_dir(root)
        self.expected = expected
        self.relative = f'task-results/{expected.task_id}/{expected.attempt_id}.json'

    def accept(self, raw):
        if not isinstance(raw, dict):
            raise ValueError('InvalidRunnerResult')
        if raw.get('phase') != 'inference':
            return validate_result(raw)
        if raw.get('kind') != 'app-question':
            if set(raw) <= {'phase', 'passed', 'error', 'checkpoint'} and raw.get('passed') is False:
                return validate_result(raw)
            raise ValueError('InvalidRunnerResult')
        if raw.get('binding') != asdict(self.expected):
            raise ValueError('InvalidRunnerResult')
        safe = {k: v for k, v in raw.items() if k != 'answer'}
        if not raw.get('answer_generated'):
            if 'answer' in raw:
                raise ValueError('InvalidRunnerResult')
            return validate_result(safe)
        answer = raw.get('answer')
        if type(answer) is not str or not answer.strip() or len(answer.encode('utf-8')) > 16384:
            raise ValueError('InvalidRunnerResult')
        artifact = {k: raw[k] for k in ('binding', 'context_sha256', 'actual_model', 'tool_succeeded', 'usage')}
        artifact['answer'] = answer
        data = canonical(artifact)
        descriptor = dict(relative_path=self.relative, sha256=hashlib.sha256(data).hexdigest(), bytes=len(data))
        safe['answer_ref'] = descriptor
        validate_result(safe)
        try:
            self._publish(data)
        except (OSError, ValueError):
            safe.pop('answer_ref')
            safe.update(passed=False, error='AnswerArtifactFailed')
        return validate_result(safe)

    def _publish(self, data):
        target = safe_path(self.root / self.relative)
        protect_dir(target.parent.parent)
        protect_dir(target.parent)
        if target.exists():
            if target.stat().st_size != len(data) or target.read_bytes() != data:
                raise ValueError('AnswerArtifactFailed')
            return
        fd, name = tempfile.mkstemp(prefix='.answer-', dir=target.parent)
        try:
            with os.fdopen(fd, 'wb') as stream:
                os.fchmod(stream.fileno(), 0o600)
                stream.write(data)
                stream.flush()
                os.fsync(stream.fileno())
            os.replace(name, target)
            directory = os.open(target.parent, os.O_RDONLY)
            try:
                os.fsync(directory)
            finally:
                os.close(directory)
        finally:
            if os.path.exists(name):
                os.unlink(name)

    def read(self, ref):
        validate_answer_ref(ref, self.expected)
        target = safe_path(self.root / ref['relative_path'])
        if target.stat().st_size != ref['bytes']:
            raise ValueError('InvalidRunnerResult')
        data = target.read_bytes()
        if hashlib.sha256(data).hexdigest() != ref['sha256']:
            raise ValueError('InvalidRunnerResult')
        value = json.loads(data)
        if (set(value) != {'binding','context_sha256','actual_model','tool_succeeded','usage','answer'}
                or value['binding'] != asdict(self.expected) or value['context_sha256'] != self.expected.snapshot_sha256
                or value['actual_model'] != MODEL or value['tool_succeeded'] is not True
                or type(value['answer']) is not str or not value['answer'].strip()
                or len(value['answer'].encode('utf-8')) > 16384):
            raise ValueError('InvalidRunnerResult')
        return value['answer']
