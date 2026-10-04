"""Shared report contracts for fresh runner results and persisted ledgers."""
from .config import checkpoint_shape, validate_checkpoint
from tools.task_store.models import TaskBinding, MODEL
import re

RUNNER_ERRORS = {'NeedsSignIn', 'RunnerFailed', 'CheckpointFailed', 'ModelFailed',
                 'TurnLimit', 'Timeout', 'InvalidJob', 'ContextUnavailable', 'InvalidAnswer'}
APP_ERRORS = {'AnswerArtifactFailed', 'TaskBindingMismatch'}
ERRORS = RUNNER_ERRORS | {
    'ControllerFailed', 'LedgerPersistenceFailed', 'OperatorAuthenticationFailed',
    'AccountMismatchOrUnavailable', 'StoragePreflightFailed', 'KeyCreateUnconfirmed',
    'KeyCleanupUnconfirmed', 'KeyInventoryUnavailable', 'SandboxCreateUnconfirmed',
    'SandboxPreparationFailed', 'RunnerOutcomeUnknown', 'InvalidRunnerResult',
    'TerminationUnconfirmed', 'CheckpointVerificationFailed', 'RecoveryResultUnavailable',
} | APP_ERRORS | {'TaskPersistenceFailed'}


def validate_answer_ref(ref, binding):
    if (not isinstance(ref, dict) or set(ref) != {'relative_path', 'sha256', 'bytes'}
            or ref['relative_path'] != f'task-results/{binding.task_id}/{binding.attempt_id}.json'
            or type(ref['sha256']) is not str or not re.fullmatch('[a-f0-9]{64}', ref['sha256'])
            or type(ref['bytes']) is not int or not 0 < ref['bytes'] <= 65536):
        raise ValueError('InvalidRunnerResult')
    return ref

def validate_result(raw, phase=None, cfg=None):
    if not isinstance(raw, dict):
        raise ValueError('InvalidRunnerResult')
    phase = raw.get('phase') if phase is None else phase
    app = raw.get('kind') == 'app-question'
    fields = {'phase', 'passed', 'error'}
    flags = {'passed'}
    if phase in {'refresh', 'inference'}:
        fields.add('checkpoint')
    if phase == 'refresh':
        fields |= {'refresh_succeeded', 'refresh_token_changed'}
        flags |= {'refresh_succeeded', 'refresh_token_changed'}
    elif phase == 'inference':
        fields |= {'tool_succeeded', 'usage'}
        flags.add('tool_succeeded')
        if app:
            fields |= {'kind', 'binding', 'actual_model', 'context_sha256', 'answer_generated', 'answer_ref'}
            flags.add('answer_generated')
        else:
            fields.add('answer_matches')
            flags.add('answer_matches')
    elif phase != 'restore':
        raise ValueError('InvalidRunnerResult')
    if set(raw) - fields or raw.get('phase') != phase or type(raw.get('passed')) is not bool:
        raise ValueError('InvalidRunnerResult')
    if app and phase != 'inference':
        raise ValueError('InvalidRunnerResult')
    for k in flags:
        if k in raw and type(raw[k]) is not bool:
            raise ValueError('InvalidRunnerResult')
    if 'error' in raw and raw['error'] not in (RUNNER_ERRORS | APP_ERRORS if app else RUNNER_ERRORS):
        raise ValueError('InvalidRunnerResult')
    if app:
        if not isinstance(raw.get('binding'), dict):
            raise ValueError('InvalidRunnerResult')
        binding = TaskBinding(**raw['binding'])
        if raw.get('actual_model') != MODEL:
            raise ValueError('InvalidRunnerResult')
        if 'context_sha256' in raw and raw['context_sha256'] != binding.snapshot_sha256:
            raise ValueError('InvalidRunnerResult')
        if raw.get('tool_succeeded') and raw.get('context_sha256') != binding.snapshot_sha256:
            raise ValueError('InvalidRunnerResult')
        if raw.get('answer_generated') and not raw.get('tool_succeeded'):
            raise ValueError('InvalidRunnerResult')
        if 'answer_ref' in raw:
            validate_answer_ref(raw['answer_ref'], binding)
            if raw.get('answer_generated') is not True:
                raise ValueError('InvalidRunnerResult')
    if 'usage' in raw:
        if not isinstance(raw['usage'], list) or len(raw['usage']) > 4:
            raise ValueError('InvalidRunnerResult')
        for usage in raw['usage']:
            if (not isinstance(usage, dict) or set(usage) != {'input', 'output', 'total_tokens'}
                    or any(type(v) is not int or v < 0 for v in usage.values())):
                raise ValueError('InvalidRunnerResult')
    if 'checkpoint' in raw:
        checkpoint_shape(raw['checkpoint'])
        if cfg is not None:
            validate_checkpoint(raw['checkpoint'], cfg)
    if raw['passed']:
        if 'error' in raw:
            raise ValueError('InvalidRunnerResult')
        if phase in {'refresh', 'inference'} and 'checkpoint' not in raw:
            raise ValueError('InvalidRunnerResult')
        if phase == 'refresh' and (raw.get('refresh_succeeded') is not True
                                  or 'refresh_token_changed' not in raw):
            raise ValueError('InvalidRunnerResult')
        if phase == 'inference' and (raw.get('tool_succeeded') is not True
                                    or raw.get('answer_generated' if app else 'answer_matches') is not True
                                    or 'usage' not in raw or (app and 'answer_ref' not in raw)):
            raise ValueError('InvalidRunnerResult')
    return raw


def inference_outcome(result):
    """Checkpoint failure cannot erase already completed inference evidence."""
    if result['phase'] != 'inference':
        return 'unknown'
    app = result.get('kind') == 'app-question'
    if (result.get('tool_succeeded') is True and result.get('answer_generated' if app else 'answer_matches') is True
            and result.get('error') in ((None, 'CheckpointFailed', 'AnswerArtifactFailed') if app else (None, 'CheckpointFailed'))):
        return 'completed'
    return 'failed'
