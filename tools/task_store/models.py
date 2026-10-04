"""Strict, credential-free app contracts and immutable task identities."""
from dataclasses import dataclass
import hashlib
import json
import re
import uuid

MODEL = 'gpt-5.6-luna'
STAGES = {'queued', 'starting', 'restoring-credentials', 'asking-agent',
          'saving-checkpoint', 'cleaning-up', 'finished'}
AppRecord = AppSnapshot = TaskRecord = AttemptRecord = dict


class ValidationError(ValueError):
    def __init__(self):
        super().__init__('InvalidInput')


class Conflict(ValueError):
    def __init__(self):
        super().__init__('Conflict')


class NotFound(ValueError):
    def __init__(self):
        super().__init__('NotFound')


class Blocked(ValueError):
    def __init__(self):
        super().__init__('Blocked')


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=False, allow_nan=False).encode('utf-8')


def digest(value):
    return hashlib.sha256(canonical(value)).hexdigest()


def identifier(value):
    try:
        if type(value) is not str or str(uuid.UUID(value)) != value:
            raise ValidationError()
    except (ValueError, TypeError, AttributeError):
        raise ValidationError() from None
    return value


def exact(value, fields):
    if type(value) is not dict or set(value) != set(fields):
        raise ValidationError()


def text(value, limit, nullable=False, nonsecret=False):
    if value is None and nullable:
        return None
    if type(value) is not str or len(value) > limit or '\x00' in value:
        raise ValidationError()
    try:
        value.encode('utf-8')
    except UnicodeError:
        raise ValidationError() from None
    value = value.strip()
    if not value:
        if nullable:
            return None
        raise ValidationError()
    if nonsecret and re.search(r'(?i)(?:postgres(?:ql)?://|\bsk-[a-zA-Z0-9_-]+|bearer\s+\S+|-----BEGIN .*PRIVATE KEY|(?:password|api[_-]?key|access[_-]?token)\s*[:=])', value):
        raise ValidationError()
    return value


def validate_app(value):
    exact(value, ('name', 'owner', 'environment', 'region', 'ack_cluster_id', 'namespace', 'deployment', 'dependencies'))
    result = {k: text(value[k], 128, k in {'ack_cluster_id', 'namespace', 'deployment'}, True)
              for k in value if k != 'dependencies'}
    deps = value['dependencies']
    if type(deps) is not list or len(deps) > 10:
        raise ValidationError()
    result['dependencies'] = []
    for dep in deps:
        exact(dep, ('name', 'type', 'lifecycle', 'notes'))
        if dep['type'] not in ('postgres', 'pgcat', 'other') or dep['lifecycle'] not in ('planned', 'present', 'unknown'):
            raise ValidationError()
        result['dependencies'].append(dict(name=text(dep['name'], 128, nonsecret=True),
                                          type=dep['type'], lifecycle=dep['lifecycle'],
                                          notes=text(dep['notes'], 1000, nullable=True, nonsecret=True) or ''))
    return result


def validate_snapshot(value):
    fields = ('app_id', 'version', 'updated_at', 'provenance')
    exact(value, (*fields, 'name', 'owner', 'environment', 'region', 'ack_cluster_id', 'namespace', 'deployment', 'dependencies'))
    identifier(value['app_id'])
    if type(value['version']) is not int or value['version'] < 1 or value['provenance'] != 'owner-entered':
        raise ValidationError()
    text(value['updated_at'], 64)
    validate_app({k: v for k, v in value.items() if k not in fields})
    if len(canonical(value)) > 32768:
        raise ValidationError()
    return json.loads(canonical(value))


def validate_submission(value):
    exact(value, ('app_id', 'question', 'model', 'idempotency_key'))
    identifier(value['app_id'])
    identifier(value['idempotency_key'])
    question = value['question']
    text(question, 2000)
    if len(question.encode('utf-8')) > 8192 or value['model'] != MODEL:
        raise ValidationError()
    return dict(value)


def validate_application_request(value):
    exact(value, ('kind', 'question', 'snapshot', 'binding', 'model'))
    if value['kind'] != 'app-question':
        raise ValidationError()
    snapshot = validate_snapshot(value['snapshot'])
    exact(value['binding'], ('task_id', 'attempt_id', 'snapshot_sha256'))
    binding = TaskBinding(**value['binding'])
    validate_submission(dict(app_id=snapshot['app_id'], question=value['question'],
                             model=value['model'], idempotency_key=binding.attempt_id))
    if digest(snapshot) != binding.snapshot_sha256:
        raise ValidationError()
    return json.loads(canonical(value))


@dataclass(frozen=True)
class TaskBinding:
    task_id: str
    attempt_id: str
    snapshot_sha256: str

    def __post_init__(self):
        identifier(self.task_id)
        identifier(self.attempt_id)
        if type(self.snapshot_sha256) is not str or not re.fullmatch('[a-f0-9]{64}', self.snapshot_sha256):
            raise ValidationError()


@dataclass(frozen=True)
class OutcomeReport:
    answer: str
    checkpoint: str
    cleanup: str
    overall: str
    error: str | None

    def __post_init__(self):
        if (self.answer not in ('completed', 'failed', 'unknown')
                or self.checkpoint not in ('saved', 'failed', 'unknown')
                or self.cleanup not in ('confirmed', 'failed', 'unknown')
                or self.overall not in ('completed', 'failed', 'blocked')
                or (self.error is not None and not re.fullmatch('[A-Z][A-Za-z]{1,64}', self.error))):
            raise ValidationError()
        if self.overall == 'completed' and (self.answer, self.checkpoint, self.cleanup) != ('completed', 'saved', 'confirmed'):
            raise ValidationError()
