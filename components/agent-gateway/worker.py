"""Separate local worker. Never receives cloud credentials from HTTP tasks."""
import argparse
from pathlib import Path
import signal
import sys
import time

sys.path.insert(0,str(Path(__file__).resolve().parents[2]))
from tools.task_store.store import Store
from tools.task_store.gateway import Gateway, worker_lock
from tools.sandbox_lifecycle.paths import canonical_local_root, lifecycle_root
from tools.sandbox_lifecycle.config import load_config
from tools.sandbox_lifecycle.cloud import Cloud


def main(argv=None):
    parser=argparse.ArgumentParser(description='Process durable local Raptor app questions.')
    parser.add_argument('--config',type=Path,required=True)
    parser.add_argument('--profile',default='infra-ops-poc')
    parser.add_argument('--once',action='store_true',help='Process at most one task or recovery action.')
    args=parser.parse_args(argv)
    stopped=False
    def stop(*_):
        nonlocal stopped
        stopped=True
    signal.signal(signal.SIGTERM,stop)
    signal.signal(signal.SIGINT,stop)
    try:
        cfg=load_config(args.config)
        store=Store(canonical_local_root()/'raptor')
        with worker_lock(store.root):
            cloud=Cloud.from_operator_profile(cfg,args.profile)
            gateway=Gateway(store,cfg,cloud,lifecycle_root()/cfg.account_id/'ledger.json')
            while not stopped:
                gateway._tick_locked()
                if args.once:
                    break
                time.sleep(2)
        return 0
    except Exception:
        print('Worker stopped: local state, ownership or controller unavailable. Inspect task history and use recovery.',file=sys.stderr)
        return 2


if __name__=='__main__':
    raise SystemExit(main())
