"""Inspect the minimal ACK compatibility plan; reject all other mutations."""

def inspect_plan(plan: dict, ownership: dict) -> dict:
    expected = {'module.sandbox.alicloud_cs_managed_kubernetes.cluster',
                'module.sandbox.alicloud_vswitch.workers',
                'module.sandbox.terraform_data.account_guard'}
    reasons=[]
    if plan.get('errored') or plan.get('deferred_changes') or plan.get('resource_drift'):
        reasons.append('incomplete_or_drifted')
    if ownership.get('account_match') is not True or ownership.get('vpc_owner') != 'rdev-foundation' or ownership.get('subnet_free') is not True:
        reasons.append('ownership_unverified')
    try:
        configured = plan['configuration']['root_module']['module_calls']['sandbox']['module']['resources']
        cluster = next(r for r in configured if r['address'] == 'alicloud_cs_managed_kubernetes.cluster')
        refs = cluster['expressions']['vswitch_ids']
        if set(refs) != {'references'} or set(refs['references']) != {'alicloud_vswitch.workers.id', 'alicloud_vswitch.workers'}:
            reasons.append('wrong_subnet_reference')
    except (KeyError, TypeError, StopIteration):
        reasons.append('subnet_reference_unverified')
    rows=[r for r in plan.get('resource_changes',[]) if r.get('mode')=='managed']
    if {r.get('address') for r in rows} != expected or len(rows)!=3:
        reasons.append('unexpected_resources')
    for row in rows:
        c=row.get('change',{});a=c.get('after',{});address=row.get('address','')
        if c.get('actions')!=['create']: reasons.append('not_creation_only')
        if '.alicloud_' in address:
            if a.get('tags')!={'Project':'raptor-iap','Environment':'ax-sandbox.ali','Owner':'ax-sandbox'}: reasons.append('wrong_tags')
        if address.endswith('.cluster'):
            values={'version':'1.36.2-aliyun.1','new_nat_gateway':False,'slb_internet_enabled':False,'skip_set_certificate_authority':True,'pod_cidr':'10.74.0.0/16','service_cidr':'10.75.0.0/16','cluster_spec':'ack.standard'}
            if any(a.get(k)!=v for k,v in values.items()): reasons.append('wrong_cluster')
            unknown = c.get('after_unknown', {})
            if any(unknown.get(k) not in (None, False) for k in ('worker_number', 'worker_numbers')): reasons.append('unknown_workers')
            if a.get('worker_number') not in (None,0) or a.get('worker_numbers') not in (None,[]): reasons.append('unexpected_workers')
        if address.endswith('.workers'):
            values={'vpc_id':ownership.get('vpc_id'),'cidr_block':'10.70.3.0/24','zone_id':'ap-southeast-1a'}
            if any(a.get(k)!=v for k,v in values.items()): reasons.append('wrong_subnet')
    return {'passed':not reasons,'reasons':sorted(set(reasons)),'cloud_creates':2 if not reasons else None,'updates':0 if not reasons else None,'deletes':0 if not reasons else None}
