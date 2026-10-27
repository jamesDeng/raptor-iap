"""Fixed local action templates and address-free output parsers.

Bindings are private configuration, never request arguments. Source hash checks
are local only; staging and worker-source review remain separate requirements.
"""
import json
import os
from pathlib import Path
import shlex
import sys
import time

from pgcat_lane_protocol import ACTIONS, Refused, require, target_valid, token, sha, identifier, validate_result, encode
from pgcat_rehearsal_worker import SOURCES as WORKER_SOURCES

BINDING_KEYS = frozenset(('workbenchCli', 'aliyunCli', 'laneWrapper', 'providerFeedPath', 'receiptPath',
    'laneLockPath', 'laneIntentDir', 'phaseWorker', 'pendingWorker', 'remoteRun',
    'contractDir', 'pendingProofWorker', 'workerKubeconfig', 'scaleoutIntentPath',
    'preScaleProofPath', 'sourceHashes', 'target'))
SOURCE_NAMES = frozenset(('laneWrapper','providerFeed','laneProtocol','laneSupervisor','laneActions',
                          'pendingProofWorker')) | frozenset(WORKER_SOURCES)
GROUP = 'asg-t4necb4dfmu6o3z2rn0g'
ACCOUNT = '1360282071200743'
REGION = 'ap-southeast-1'
WORKER = 'i-t4nj1bitmcz7cuoch82h'
PROFILE = 'infra-ops-poc'


def validate_bindings(bindings):
    require(type(bindings) is dict and set(bindings) == BINDING_KEYS)
    target_valid(bindings['target'])
    require(bindings['target']['originalInstanceIds'] == sorted(
            bindings['target']['originalInstanceIds']), 'target')
    require(bindings['target']['accountId'] == ACCOUNT and bindings['target']['regionId'] == REGION
            and bindings['target']['workerId'] == WORKER and bindings['target']['scalingGroupId'] == GROUP, 'target')
    for key in BINDING_KEYS - {'sourceHashes','target'}:
        path = bindings[key]
        require(type(path) is str and path.startswith('/') and '\x00' not in path
                and all(ord(c) >= 32 for c in path) and '..' not in Path(path).parts)
    for key in ('workbenchCli','aliyunCli'):
        require(os.path.isfile(bindings[key]) and os.access(bindings[key],os.X_OK))
    require(type(bindings['sourceHashes']) is dict and set(bindings['sourceHashes']) == SOURCE_NAMES)
    run = Path(bindings['remoteRun'])
    contract = Path(bindings['contractDir'])
    require(run.name == 'run' and contract.name == 'contract' and
            run.parent == contract.parent and
            bindings['phaseWorker'] == bindings['pendingWorker'] ==
            str(run/'pgcat_rehearsal_worker.py') and
            bindings['pendingProofWorker'] == str(run/'pgcat_pending_proof_worker.py'))


def unique(pairs):
    result = {}
    for key,value in pairs:
        require(key not in result,'parse')
        result[key] = value
    return result


class ActionCatalog:
    @staticmethod
    def command(action, bindings, run_id=None):
        require(type(action) is str and action in ACTIONS)
        validate_bindings(bindings)
        # No ambient credentials, shell startup files or arbitrary environment.
        env = {'PATH':os.defpath, 'HOME':os.path.expanduser('~'), 'LANG':'C', 'LC_ALL':'C'}
        for key in ('HTTP_PROXY','HTTPS_PROXY','ALL_PROXY','NO_PROXY',
                    'http_proxy','https_proxy','all_proxy','no_proxy'):
            if key in os.environ: env[key]=os.environ[key]
        if action == 'scaleout-two-to-three':
            return ([bindings['aliyunCli'],'ess','ModifyScalingGroup','--ScalingGroupId',GROUP,
                     '--DesiredCapacity','3','--profile',PROFILE,'--region',REGION],env,20.0)
        q = shlex.quote
        if action.startswith(('baseline-','pre-scale-')):
            identifier(run_id)
            phase, verb = action.rsplit('-',1)
            hashes={key:bindings['sourceHashes'][key] for key in WORKER_SOURCES}
            parts=['python3',bindings['phaseWorker'],verb,phase,bindings['contractDir'],
                   run_id,bindings['workerKubeconfig'],
                   json.dumps(hashes,sort_keys=True,separators=(',',':')),
                   encode(bindings['target']).decode('ascii')]
            command=' '.join(q(part) for part in parts)
        elif action.startswith('zero-gate-'):
            raise Refused('source')
        elif action == 'pending-proof':
            identifier(run_id)
            command = ('python3 '+q(bindings['pendingProofWorker'])+' '+q(bindings['contractDir'])+
                       ' '+q(run_id))
        elif action.startswith('pending-'):
            identifier(run_id)
            hashes={key:bindings['sourceHashes'][key] for key in WORKER_SOURCES}
            parts=['python3',bindings['pendingWorker'],action.split('-')[1],
                   'pending-ready',bindings['contractDir'],run_id,
                   bindings['workerKubeconfig'],
                   json.dumps(hashes,sort_keys=True,separators=(',',':')),
                   encode(bindings['target']).decode('ascii')]
            command=' '.join(q(part) for part in parts)
        elif action.startswith('collector-'):
            command = 'cd '+q(bindings['remoteRun'])+' && python3 collector_control.py '+action.split('-')[1]
        else:
            # Task 6D must install a reviewed fixed reconciliation producer.
            raise Refused('source')
        env.update(PGCAT_LANE_ROLE='external', PGCAT_LANE_REAL_CLI=bindings['workbenchCli'],
            PGCAT_LANE_LOCK=bindings['laneLockPath'], PGCAT_LANE_RECEIPT=bindings['receiptPath'],
            PGCAT_LANE_INTENT_DIR=bindings['laneIntentDir'])
        return ([sys.executable,bindings['laneWrapper'],'exec','-P',PROFILE,'-r',REGION,'-i',WORKER,'-c',command],env,5.0)

    @staticmethod
    def parse(action, raw, request, receipt=None, source_sha=None):
        require(type(raw) is bytes and len(raw) <= 8192,'parse')
        if action in ('pending-start','pending-status'):
            statuses=('STARTED',) if action == 'pending-start' else ('RUNNING','PREPARED')
            for status in statuses:
                if raw in (('PENDING_DETACHED:'+status).encode('ascii'),
                           ('PENDING_DETACHED:'+status+'\n').encode('ascii')):
                    return {'status':status}
            raise Refused('parse')
        try:
            rows = [json.loads(line,object_pairs_hook=unique) for line in raw.decode('ascii').splitlines()]
        except (ValueError,UnicodeError):
            raise Refused('parse') from None
        if action == 'reconcile-close':
            require(len(rows) == 2 and rows[0] == dict(status='PREPARED',stage='reconcile',category='rehearsal-only'),'parse')
            value=rows[1]
            require(type(value) is dict and set(value) == {'feedSession','sequence','resourceVersion'},'parse')
            require(value['feedSession'] == request['feedSession'] and type(value['sequence']) is int
                    and value['sequence'] >= request['minimumSequence'] and token(value['resourceVersion'])
                    and value['resourceVersion'].isascii() and value['resourceVersion'].isdigit()
                    and int(value['resourceVersion']) > 0,'parse')
        else:
            require(len(rows) == 1 and type(rows[0]) is dict,'parse')
            value=rows[0]
            if action == 'scaleout-two-to-three':
                require(set(value) == {'RequestId'} and token(value['RequestId']),'parse')
                value={'requestId':value['RequestId']}
            elif action == 'pending-proof':
                require(set(value) == {'runId','feedSession','newId','originalInstanceIds',
                    'sequence','observedAt','resourceVersion','clientRosterSha256',
                    'evidenceSha256','sourceSha256'}, 'parse')
                require(value['runId'] == request['runId'] and
                    value['feedSession'] == request['feedSession'] and
                    value['originalInstanceIds'] == request['target']['originalInstanceIds'] and
                    token(value['newId']) and value['newId'] not in value['originalInstanceIds'] and
                    type(value['sequence']) is int and 0 < value['sequence'] <= request['minimumSequence'] and
                    type(value['observedAt']) in (int,float) and
                    0 <= time.time()-value['observedAt'] < 30 and
                    type(value['resourceVersion']) is str and value['resourceVersion'].isascii() and
                    value['resourceVersion'].isdigit() and int(value['resourceVersion']) > 0 and
                    sha(value['clientRosterSha256']) and sha(value['evidenceSha256']) and
                    sha(source_sha) and value['sourceSha256'] == source_sha, 'parse')
                require(type(receipt) is dict and receipt.get('feedSession') == value['feedSession'] and
                        type(receipt.get('sequence')) is int and value['sequence'] <= receipt['sequence'] and
                        type(receipt.get('observedAt')) in (int,float) and
                        value['observedAt'] <= receipt['observedAt'], 'receipt')
            elif action.startswith(('baseline-','pre-scale-','zero-gate-')):
                phase,verb=action.rsplit('-',1)
                statuses = ('STARTED',) if verb == 'start' else ('RUNNING','PREPARED')
                require(value.get('status') in statuses and value.get('phase') == phase,'parse')
                keys={'status','phase'}
                if phase == 'pre-scale' and value['status'] == 'PREPARED':
                    keys |= {'feedSession','sequence'}
                    require(type(receipt) is dict and receipt.get('feedSession') == request['feedSession']
                            and type(receipt.get('sequence')) is int,'parse')
                    require(value.get('feedSession') == request['feedSession'] and type(value.get('sequence')) is int
                            and 0 < value['sequence'] <= receipt['sequence'],'parse')
                require(set(value) == keys,'parse')
            elif action == 'collector-launch':
                require(value == {'status':'LAUNCHED'},'parse')
            elif action == 'collector-status':
                require(set(value) == {'status','identity'} and value['status'] == 'ALIVE','parse')
            else:
                raise Refused('configuration')
        validate_result(dict(state='complete',category='ok',payload=value))
        return value
