"""Stdlib-only root for isolated, source-snapshot local lane execution.

Enter through ROOT with -I -S, never by an ordinary application import.
No runtime switch grants paid capability or disables verification.
"""
import hashlib
import importlib
import importlib.abc
import importlib.util
import json
import os
from pathlib import Path
import stat
import sys
import types

SOURCES = {
    'sourceBootstrap':'pgcat_source_bootstrap.py',
    'controller':'pgcat_rehearsal_controller.py','client':'pgcat_lane_client.py',
    'runner':'pgcat_lane_runner.py','laneProtocol':'pgcat_lane_protocol.py',
    'laneSupervisor':'pgcat_lane_supervisor.py','laneActions':'pgcat_lane_actions.py',
    'laneWrapper':'pgcat_workbench_lane.py','providerFeed':'provider_feed.py',
    'collectorControl':'pgcat_collector_control.py','laneTimingAssessor':'pgcat_lane_timing_assessor.py',
    'rehearsalWorker':'pgcat_rehearsal_worker.py','rehearsalContract':'pgcat_rehearsal_contract.py',
    'pendingProofWorker':'pgcat_pending_proof_worker.py','preDrainProofWorker':'pgcat_pre_drain_proof_worker.py',
    'zeroProofWorker':'pgcat_zero_proof_worker.py','captureWindow':'capture_window.py',
    'finishClients':'finish_clients.py','workerCapture':'worker_capture.py',
    'segmentedCapture':'segmented_capture.py'}
ROOT = '''import hashlib,os,stat,sys,types
p,h=sys.argv[1:3]
f=os.open(p,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
try:
 s=os.fstat(f);b=os.read(f,262145)
 if not (stat.S_ISREG(s.st_mode) and s.st_uid==os.geteuid() and len(b)==s.st_size and len(b)<=262144 and hashlib.sha256(b).hexdigest()==h):raise ValueError('source')
finally:os.close(f)
m=types.ModuleType('pgcat_source_bootstrap');m.__file__=p;m.__verified_root_sha256__=h
sys.modules[m.__name__]=m
exec(compile(b,p,'exec',dont_inherit=True),m.__dict__)
sys.argv=[p]+sys.argv[3:]
sys.exit(m.main())
'''


def _check(value):
    if not value: raise ValueError('source')


def _read(path):
    path=Path(path)
    _check(path.is_absolute() and '..' not in path.parts)
    for parent in path.parents: _check(not parent.is_symlink())
    fd=os.open(str(path),os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
    try:
        before=os.fstat(fd);raw=os.read(fd,262145);after=os.fstat(fd)
        _check(stat.S_ISREG(before.st_mode) and before.st_uid==os.geteuid() and
               len(raw)==before.st_size and len(raw)<=262144 and
               (before.st_dev,before.st_ino,before.st_size,before.st_mtime_ns)==
               (after.st_dev,after.st_ino,after.st_size,after.st_mtime_ns))
        return raw
    finally:os.close(fd)


class VerifiedFinder(importlib.abc.MetaPathFinder,importlib.abc.Loader):
    def __init__(self,root,snapshot):self.root=root;self.snapshot=snapshot
    def find_spec(self,name,path=None,target=None):
        if name in self.snapshot:
            spec=importlib.util.spec_from_loader(name,self,origin=str(self.root/(name+'.py')))
            spec.has_location=True
            return spec
        # Refuse undeclared local files/packages, even when -I removed their path.
        if (self.root/(name.split('.')[0]+'.py')).exists() or (self.root/name.split('.')[0]).is_dir():
            raise ValueError('source')
        return None
    def create_module(self,spec):return None
    def exec_module(self,module):
        raw=self.snapshot[module.__name__]
        module.__verified_source_sha256__=hashlib.sha256(raw).hexdigest()
        exec(compile(raw,module.__file__,'exec',dont_inherit=True),module.__dict__)


_finder=None

def install(hashes):
    global _finder
    _check(sys.flags.isolated and sys.flags.no_site and _finder is None)
    _check(type(hashes) is dict and set(hashes)==set(SOURCES))
    _check(globals().get('__verified_root_sha256__')==hashes['sourceBootstrap'])
    root=Path(__file__).absolute().parent;snapshot={}
    for key,name in SOURCES.items():
        raw=_read(root/name);_check(hashlib.sha256(raw).hexdigest()==hashes[key])
        snapshot[name[:-3]]=raw
    for name,module in list(sys.modules.items()):
        if name=='pgcat_source_bootstrap':
            _check(module is sys.modules[__name__]);continue
        location=getattr(module,'__file__',None)
        _check(name not in snapshot and not (location and root in Path(location).absolute().parents))
    _finder=VerifiedFinder(root,snapshot);sys.meta_path.insert(0,_finder)
    return _finder


def require_verified(name):
    module=sys.modules.get(name)
    _check(_finder is not None and _finder in sys.meta_path and module is not None and
           getattr(module,'__loader__',None) is _finder and
           getattr(module,'__verified_source_sha256__',None)==hashlib.sha256(_finder.snapshot[name]).hexdigest())


def command(hashes,entry,args):
    _check(entry in ('runner','providerFeed','laneWrapper'))
    return [sys.executable,'-I','-S','-c',ROOT,str(Path(__file__).absolute()),
            hashes['sourceBootstrap'],entry,json.dumps(hashes,sort_keys=True,separators=(',',':'))]+list(args)


def current_hashes():
    _check(_finder is not None and _finder in sys.meta_path)
    return {key:hashlib.sha256(_finder.snapshot[name[:-3]]).hexdigest()
            for key,name in SOURCES.items()}


def load_controller(binding_path):
    # This function is called in an isolated root before any application import.
    value=json.loads(_read(Path(binding_path)).decode('ascii'))
    hashes=value['laneBindings']['sourceHashes']
    _check(value['sourceHashes']=={k:hashes[k] for k in ('controller','client','runner')})
    install(hashes)
    return importlib.import_module('pgcat_rehearsal_controller').Controller(binding_path)


def main():
    _check(len(sys.argv)>=3)
    entry,raw=sys.argv[1:3];_check(entry in ('runner','providerFeed','laneWrapper'))
    install(json.loads(raw))
    name=SOURCES[entry][:-3];module=importlib.import_module(name)
    sys.argv=[str(Path(__file__).parent/SOURCES[entry])]+sys.argv[3:]
    return module.main()
