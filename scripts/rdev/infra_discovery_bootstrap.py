"""Validate a narrow existing-environment update; contains no credentials or cloud calls."""
ACCOUNT = '1360282071200743'
CLUSTER = 'c92787e953503492ea141a744c81498f1'
URL = 'https://infra-api.raptor-iap.top'

def validate_environment_update(before: dict, after: dict) -> None:
    if not isinstance(before, dict) or not isinstance(after, dict):
        raise ValueError('Environment objects required')
    if before.get('code') != 'rdev.ali' or after.get('code') != 'rdev.ali':
        raise ValueError('Owned environment required')
    if {k:v for k,v in before.items() if k != 'config'} != {k:v for k,v in after.items() if k != 'config'}:
        raise ValueError('Environment identity or metadata changed')
    old, new = before.get('config'), after.get('config')
    if not isinstance(old, dict) or not isinstance(new, dict):
        raise ValueError('Environment configuration required')
    for key, value in {'cloud':'aliyun', 'cloudAccountId':ACCOUNT, 'region':'ap-southeast-1'}.items():
        if old.get(key) != value or new.get(key) != value:
            raise ValueError('Cloud scope differs')
    if old.get('clusterId', old.get('ackClusterId')) != CLUSTER or new.get('clusterId') != CLUSTER:
        raise ValueError('Cluster scope differs')
    allowed = {'clusterId', 'infraApiUrl'}
    if {k:v for k,v in old.items() if k not in allowed} != {k:v for k,v in new.items() if k not in allowed}:
        raise ValueError('Unrelated configuration changed')
    if new.get('infraApiUrl') != URL:
        raise ValueError('Exact owned HTTPS endpoint required')
