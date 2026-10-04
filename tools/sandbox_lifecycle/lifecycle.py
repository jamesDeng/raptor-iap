"""Sequential lifecycle; unknown cloud outcomes never authorize replacement."""
from dataclasses import asdict
from datetime import datetime, timedelta, timezone
from pathlib import Path
import uuid
from .config import validate_checkpoint
from .state import account_lock, load_ledger, save_ledger
from .cloud import CloudError, safe_result
from .contracts import ERRORS, inference_outcome
from tools.task_store.models import TaskBinding
from .task_results import TaskResultSink


def error_code(error):
    return str(error) if isinstance(error, CloudError) and str(error) in ERRORS else 'ControllerFailed'


def output(state, passed=False):
    return {
        'passed': passed, 'phase': state['phase'],
        'request_outcome': state.get('request_outcome', 'unknown'),
        'checkpoint': state['checkpoint'],
        'cleanup_confirmed': not any(k in state for k in ('sandbox_id', 'key_id', 'cleanup_key_ids', 'pending')),
        'error': state.get('failure'),
        **{k: state[k] for k in ('result', 'refresh_result', 'persistence_error') if k in state},
    }


def locked_path(cfg, path):
    path = Path(path).absolute()
    root = path.parent.parent
    if path != root / cfg.account_id / 'ledger.json':
        raise ValueError('InvalidLedgerLocation')
    return account_lock(root, cfg.account_id)


def persist(state, path):
    try:
        save_ledger(path, state)
    except (OSError, ValueError):
        state['persistence_error'] = True
        raise CloudError('LedgerPersistenceFailed') from None


def best_effort_persist(state, path):
    try:
        persist(state, path)
        return True
    except CloudError:
        return False


def record_failure(state, path, error):
    state['phase'] = 'blocked'
    state['failure'] = error_code(error)
    if state['failure'] == 'LedgerPersistenceFailed':
        state['persistence_error'] = True
    best_effort_persist(state, path)


def new_key(state, path, cloud, handles):
    if 'key_id' in state:
        state['cleanup_key_ids'] = list(dict.fromkeys(state.get('cleanup_key_ids', []) + [state.pop('key_id')]))
    state['key_name'] = 'raptor-lifecycle-' + uuid.uuid4().hex
    state['key_expiry'] = (datetime.now(timezone.utc) + timedelta(minutes=60)).strftime('%Y-%m-%dT%H:%M:%SZ')
    state.update(phase='creating-key', pending='key')
    persist(state, path)
    key = cloud.create_key(state['key_name'], state['key_expiry'])
    handles['key'] = key  # In-memory only, usable for cleanup even if the next write fails.
    state['key_id'] = key.id
    state.pop('pending')
    persist(state, path)
    return key


def cleanup(state, path, cloud, key):
    persistence_failed = False
    if 'sandbox_id' in state:
        state['phase'] = 'terminating'
        persistence_failed |= not best_effort_persist(state, path)
        cloud.terminate_and_confirm(state['sandbox_id'], key)
        state.pop('sandbox_id')
        persistence_failed |= not best_effort_persist(state, path)
    identities = state.get('cleanup_key_ids', []) + ([state['key_id']] if 'key_id' in state else [])
    for identity in list(dict.fromkeys(identities)):
        cloud.remove_key(identity)
        if state.get('key_id') == identity:
            state.pop('key_id')
        if identity in state.get('cleanup_key_ids', []):
            state['cleanup_key_ids'].remove(identity)
        if not state.get('cleanup_key_ids'):
            state.pop('cleanup_key_ids', None)
        persistence_failed |= not best_effort_persist(state, path)
    if state.get('pending') == 'key':
        cloud.resolve_key_intent(state['key_name'])
        state.pop('pending')
        persistence_failed |= not best_effort_persist(state, path)
    if persistence_failed:
        raise CloudError('LedgerPersistenceFailed')


def select_candidate(state, path):
    proposed = dict(state, checkpoint=state['candidate'])
    proposed.pop('candidate')
    # Do not mutate the selected in-memory reference until publication succeeds.
    persist(proposed, path)
    state.clear()
    state.update(proposed)


def publish(state, path, cloud, result, cfg):
    if 'checkpoint' not in result:
        return
    ref = validate_checkpoint(result['checkpoint'], cfg)
    state['candidate'] = asdict(ref)
    persist(state, path)
    cloud.verify_checkpoint(ref)
    select_candidate(state, path)


def remember_result(state, result):
    state['result'] = result
    if result['phase'] == 'refresh':
        state['refresh_result'] = result
    elif result['phase'] == 'inference':
        state['request_outcome'] = inference_outcome(result)


def recover_locked(cfg, path, cloud, state):
    if state.get('pending') == 'sandbox' and 'sandbox_id' not in state:
        record_failure(state, path, CloudError('SandboxCreateUnconfirmed'))
        return output(state)
    handles = {}
    failure = None
    try:
        cloud.assert_identity()
        if state.get('pending') == 'key':
            cloud.resolve_key_intent(state['key_name'])
            state.pop('pending')
            persist(state, path)
        if 'sandbox_id' in state:
            key = new_key(state, path, cloud, handles)
            options = {}
            if 'task_binding' in state and state.get('job_phase') == 'inference':
                options['result_sink'] = TaskResultSink(path.parent, TaskBinding(**state['task_binding']))
            result = cloud.read_completed_result(state['sandbox_id'], key,
                                                state.get('job_generation'), state.get('job_phase', 'inference'), **options)
            if result:
                result = safe_result(result, cfg, state.get('job_phase', 'inference'))
                remember_result(state, result)
                if 'checkpoint' in result:
                    state['candidate'] = result['checkpoint']
                persist(state, path)
    except Exception as error:
        failure = error
        record_failure(state, path, error)
    # A result-read or persistence failure must not skip cleanup of known resources.
    try:
        cleanup(state, path, cloud, handles.get('key'))
    except Exception as error:
        record_failure(state, path, error)
        return output(state)
    if failure is not None:
        record_failure(state, path, failure)
        return output(state)
    try:
        if 'candidate' in state:
            cloud.verify_checkpoint(validate_checkpoint(state['candidate'], cfg))
            select_candidate(state, path)
        state['phase'] = 'finished'
        state.pop('failure', None)
        persist(state, path)
        return output(state, True)
    except Exception as error:
        record_failure(state, path, error)
        return output(state)


def execute(cfg, request, ledger_path, cloud):
    with locked_path(cfg, ledger_path) as path:
        state = load_ledger(path, cfg)
        if any(k in state for k in ('sandbox_id', 'key_id', 'cleanup_key_ids', 'pending')):
            recovered = recover_locked(cfg, path, cloud, state)
            if not recovered['passed']:
                return recovered
        if state['phase'] == 'blocked':
            return output(state)
        state = {k: v for k, v in state.items() if k in ('schema_version', 'fingerprint', 'checkpoint')}
        binding = TaskBinding(**request.application['binding']) if request.application else None
        state.update(phase='idle', attempt_id=binding.attempt_id if binding else str(uuid.uuid4()), request_outcome='unknown')
        if binding:
            state['task_binding'] = asdict(binding)
        persist(state, path)  # Before any new external resource exists.
        handles = {}
        passed = False
        try:
            cloud.assert_storage()
            cloud.verify_checkpoint(validate_checkpoint(state['checkpoint'], cfg, True))
            key = new_key(state, path, cloud, handles)
            state.update(phase='creating', pending='sandbox')
            persist(state, path)
            state['sandbox_id'] = cloud.create_sandbox(state['attempt_id'], key)
            state.pop('pending')
            persist(state, path)
            cloud.prepare(state['sandbox_id'], key)
            phases = ['restore'] + (['refresh'] if request.force_refresh else []) + ['inference']
            for phase in phases:
                state.update(phase='restoring' if phase == 'restore' else 'running',
                             job_generation=str(uuid.uuid4()), job_phase=phase)
                persist(state, path)
                job = {'phase': phase, 'generation': state['job_generation'], 'reference': state['checkpoint'],
                       'limits': {'model_seconds': cfg.model_seconds, 'max_turns': cfg.max_turns,
                                  'max_output_tokens': cfg.max_output_tokens}}
                options = {}
                if binding and phase == 'inference':
                    job['request'] = request.application
                    options['result_sink'] = TaskResultSink(path.parent, binding)
                result = safe_result(cloud.run_job(state['sandbox_id'], job, key, **options), cfg, phase)
                remember_result(state, result)
                persist(state, path)
                if phase != 'restore':
                    state['phase'] = 'checkpointing'
                    persist(state, path)
                    publish(state, path, cloud, result, cfg)
                if not result['passed']:
                    raise CloudError(result.get('error', 'ModelFailed'))
            passed = True
        except Exception as error:
            record_failure(state, path, error)
        try:
            cleanup(state, path, cloud, handles.get('key'))
        except Exception as error:
            record_failure(state, path, error)
            return output(state)
        state['phase'] = 'blocked' if 'pending' in state else 'finished'
        if not best_effort_persist(state, path):
            record_failure(state, path, CloudError('LedgerPersistenceFailed'))
        return output(state, passed and 'failure' not in state and state['phase'] == 'finished')


def recover(cfg, ledger_path, cloud):
    with locked_path(cfg, ledger_path) as path:
        return recover_locked(cfg, path, cloud, load_ledger(path, cfg))


def status(cfg, ledger_path, cloud):
    state = load_ledger(ledger_path, cfg)
    cloud.assert_identity()
    cloud.verify_checkpoint(validate_checkpoint(state['checkpoint'], cfg, True))
    return output(state, not any(k in state for k in ('sandbox_id', 'key_id', 'pending', 'cleanup_key_ids')))
