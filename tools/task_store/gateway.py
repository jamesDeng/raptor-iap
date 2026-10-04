"""Single-account queue worker; unknown ownership is never a lease to replay."""
from contextlib import contextmanager
from dataclasses import asdict
import fcntl
import os
from pathlib import Path
from .models import TaskBinding, OutcomeReport, NotFound
from tools.sandbox_lifecycle.config import Request
from tools.sandbox_lifecycle.cloud import CloudError
from tools.sandbox_lifecycle.state import account_lock, load_ledger, protect_dir, safe_path
from tools.sandbox_lifecycle.lifecycle import _execute_locked, recover_locked, output
from tools.sandbox_lifecycle.task_results import TaskResultSink

RESOURCE_FIELDS = ('sandbox_id','key_id','cleanup_key_ids','pending','candidate')


@contextmanager
def worker_lock(root: Path):
    path = safe_path(protect_dir(root)/'worker.lock')
    fd = os.open(path,os.O_CREAT|os.O_RDWR|os.O_NOFOLLOW,0o600)
    try:
        try:
            fcntl.flock(fd,fcntl.LOCK_EX|fcntl.LOCK_NB)
        except BlockingIOError:
            raise ValueError('Busy') from None
        yield
    finally:
        os.close(fd)


class Gateway:
    def __init__(self, store, cfg, cloud, ledger_path):
        self.store,self.cfg,self.cloud=store,cfg,cloud
        self.path=Path(ledger_path).absolute()
        if self.path != self.path.parent.parent/cfg.account_id/'ledger.json':
            raise ValueError('InvalidLedgerLocation')

    def _binding(self, attempt):
        task=self.store.get_task(attempt['task_id'])
        return TaskBinding(task['task_id'],attempt['attempt_id'],task['snapshot_sha256'])

    def _known(self, binding):
        task=self.store.get_task(binding.task_id)
        attempt=next((a for a in task['attempts'] if a['attempt_id']==binding.attempt_id),None)
        if attempt is None or task['snapshot_sha256'] != binding.snapshot_sha256:
            raise ValueError('TaskBindingMismatch')
        return attempt

    def _reconcile_locked(self):
        state=load_ledger(self.path,self.cfg)
        current=TaskBinding(**state['task_binding']) if 'task_binding' in state else None
        prior=None
        if current:
            try:
                prior=self._known(current)
            except (ValueError,NotFound):
                self.store.block(None,'TaskBindingMismatch')
                return False
        for attempt in self.store.unfinished():
            binding=self._binding(attempt)
            conflicting=current and binding!=current and (prior['state'] not in ('completed','failed') or state['phase']!='finished' or any(k in state for k in RESOURCE_FIELDS))
            gate=self.store.gate()
            error='TaskBindingMismatch' if conflicting else (gate['error'] if gate and gate['binding']==asdict(binding) else 'RunnerOutcomeUnknown')
            self.store.block(binding,error)
        if not self.store.unfinished() and (any(k in state for k in RESOURCE_FIELDS) or state['phase']=='blocked'):
            self.store.block(current,'RunnerOutcomeUnknown')
        gate=self.store.gate()
        if (gate and gate['binding'] is None and gate['error']=='RunnerOutcomeUnknown'
                and current is None and state['phase']=='finished' and not any(k in state for k in RESOURCE_FIELDS)):
            self.store.clear_gate(None)
        return True

    def reconcile(self):
        with worker_lock(self.store.root), account_lock(self.path.parent.parent,self.cfg.account_id):
            self._reconcile_locked()

    def tick(self):
        try:
            with worker_lock(self.store.root):
                return self._tick_locked()
        except ValueError as error:
            if str(error)=='Busy':
                return False
            raise

    def _tick_locked(self):
        try:
            with account_lock(self.path.parent.parent,self.cfg.account_id):
                if not self._reconcile_locked():
                    return False
                recovery=self.store.claim_recovery()
                if recovery:
                    self._recover(recovery)
                    return True
                attempt=self.store.claim_next()
                if attempt is None:
                    return False
                binding=self._binding(attempt)
                task=self.store.get_task(binding.task_id)
                request=Request('app-question',application=dict(kind='app-question',question=task['question'],
                    snapshot=task['snapshot'],binding=asdict(binding),model=task['model']))
                try:
                    report=_execute_locked(self.cfg,request,self.path,self.cloud,binding=binding,
                                           observer=lambda stage:self.store.append_event(binding,stage))
                    self._finish(binding,report)
                except Exception:
                    self.store.block(binding,'TaskPersistenceFailed')
                return True
        except ValueError as error:
            if str(error)=='Busy':
                return False
            raise

    def _finish(self, binding, report):
        result=report.get('result',{})
        answer=None
        answer_outcome='failed' if report.get('request_outcome')=='failed' else 'unknown'
        if result.get('kind')=='app-question' and result.get('binding')==asdict(binding) and result.get('answer_generated'):
            answer_outcome='completed'
            if 'answer_ref' in result:
                try:
                    answer=TaskResultSink(self.path.parent,binding).read(result['answer_ref'])
                    answer_outcome='completed'
                except (OSError,ValueError):
                    report=dict(report,passed=False,error='AnswerArtifactFailed')
            else:
                report=dict(report,passed=False,error='AnswerArtifactFailed')
        checkpoint='unknown'
        if result.get('phase')=='inference':
            checkpoint='saved' if result.get('checkpoint') and result['checkpoint']==report.get('checkpoint') else 'failed'
        cleanup='confirmed' if report.get('cleanup_confirmed') else 'failed'
        blocked=('candidate' in load_ledger(self.path,self.cfg) or cleanup!='confirmed' or report.get('phase')=='blocked' or report.get('persistence_error')
                 or report.get('error') in ('TaskPersistenceFailed','LedgerPersistenceFailed','AnswerArtifactFailed','TaskBindingMismatch'))
        overall='completed' if answer is not None and report.get('passed') and (answer_outcome,checkpoint,cleanup)==('completed','saved','confirmed') else ('blocked' if blocked else 'failed')
        evidence=None
        if result.get('kind')=='app-question' and result.get('binding')==asdict(binding):
            evidence={k:result[k] for k in ('actual_model','context_sha256','tool_succeeded','usage') if k in result}
        self.store.finish(binding,OutcomeReport(answer_outcome,checkpoint,cleanup,overall,report.get('error')),answer,evidence=evidence)
        return overall!='blocked'

    def _recover(self, binding):
        state=load_ledger(self.path,self.cfg)
        current=TaskBinding(**state['task_binding']) if 'task_binding' in state else None
        if current!=binding:
            previous_safe=False
            if current:
                try:
                    previous_safe=self._known(current)['state'] in ('completed','failed') and state['phase']=='finished'
                except (ValueError,NotFound):
                    pass
            if any(k in state for k in RESOURCE_FIELDS) or (current is not None and not previous_safe):
                self.store.block(binding,'TaskBindingMismatch')
                self.store.finish_recovery(binding,cleared=False)
                return
            # A claimed question without a run ledger is unknown, never replayable.
            self.store.finish(binding,OutcomeReport('unknown','unknown','confirmed','failed',None),None)
            self.store.finish_recovery(binding,cleared=True)
            return
        result=recover_locked(self.cfg,self.path,self.cloud,state)
        clear=self._finish(binding,result)
        self.store.finish_recovery(binding,cleared=clear)
