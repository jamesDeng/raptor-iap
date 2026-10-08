"""Fail-closed certificate compatibility decision from sanitized probe evidence."""

def evaluate_certificate_gate(discovery: dict, probe: dict, required_versions: dict) -> dict:
    reasons = []
    if not required_versions or set(required_versions) != {'podcertificaterequests', 'clustertrustbundles'}:
        reasons.append('invalid_requirements')
    for resource, version in sorted(required_versions.items()):
        versions = [version] if isinstance(version, str) else version
        served = isinstance(versions, list) and any(
            isinstance(discovery.get(v), list) and resource in discovery[v]
            for v in versions if isinstance(v, str))
        if not served:
            reasons.append('missing_resource:' + resource)
    for check in ('approval', 'signing', 'projection'):
        if probe.get(check) is not True:
            reasons.append(check + '_unverified')
    return {'ready': not reasons, 'reasons': reasons,
            'evidence': {'required_versions': dict(required_versions),
                         'checks': {k: probe.get(k) is True for k in ('approval', 'signing', 'projection')}}}
