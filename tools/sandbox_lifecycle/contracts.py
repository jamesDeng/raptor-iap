"""Shared report contracts for fresh runner results and persisted ledgers."""
from .config import checkpoint_shape, validate_checkpoint

RUNNER_ERRORS = {'NeedsSignIn', 'RunnerFailed', 'CheckpointFailed', 'ModelFailed',
                 'TurnLimit', 'Timeout', 'InvalidJob'}
ERRORS = RUNNER_ERRORS | {
    'ControllerFailed', 'LedgerPersistenceFailed', 'OperatorAuthenticationFailed',
    'AccountMismatchOrUnavailable', 'StoragePreflightFailed', 'KeyCreateUnconfirmed',
    'KeyCleanupUnconfirmed', 'KeyInventoryUnavailable', 'SandboxCreateUnconfirmed',
    'SandboxPreparationFailed', 'RunnerOutcomeUnknown', 'InvalidRunnerResult',
    'TerminationUnconfirmed', 'CheckpointVerificationFailed', 'RecoveryResultUnavailable',
}

def validate_result(raw, phase=None, cfg=None):
    if not isinstance(raw, dict):
        raise ValueError('InvalidRunnerResult')
    phase = raw.get('phase') if phase is None else phase
    fields = {'phase', 'passed', 'error'}
    if phase in {'refresh', 'inference'}:
        fields.add('checkpoint')
    if phase == 'refresh':
        fields |= {'refresh_succeeded', 'refresh_token_changed'}
    elif phase == 'inference':
        fields |= {'tool_succeeded', 'answer_matches', 'usage'}
    elif phase != 'restore':
        raise ValueError('InvalidRunnerResult')
    if set(raw) - fields or raw.get('phase') != phase or type(raw.get('passed')) is not bool:
        raise ValueError('InvalidRunnerResult')
    for k in fields - {'phase', 'error', 'checkpoint', 'usage'}:
        if k in raw and type(raw[k]) is not bool:
            raise ValueError('InvalidRunnerResult')
    if 'error' in raw and raw['error'] not in RUNNER_ERRORS:
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
                                    or raw.get('answer_matches') is not True or 'usage' not in raw):
            raise ValueError('InvalidRunnerResult')
    return raw


def inference_outcome(result):
    """Checkpoint failure cannot erase already completed inference evidence."""
    if result['phase'] != 'inference':
        return 'unknown'
    if (result.get('tool_succeeded') is True and result.get('answer_matches') is True
            and result.get('error') in (None, 'CheckpointFailed')):
        return 'completed'
    return 'failed'
