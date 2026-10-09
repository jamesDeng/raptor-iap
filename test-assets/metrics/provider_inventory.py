"""Complete ESS inventory for explicit metrics refresh, including protected nodes."""
def collect_members(fetch, group_id):
    total = None
    rows, seen = [], set()
    for page in range(1, 101):
        body = fetch(page)
        count = body.get('TotalCount')
        if type(count) is not int or count < 1 or count > 5000 or body.get('PageNumber') != page:
            raise ValueError('Incomplete provider inventory')
        if total is None:
            total = count
        if total != count:
            raise ValueError('Changed provider inventory')
        batch = body.get('ScalingInstances', {}).get('ScalingInstance')
        if not isinstance(batch, list) or not batch:
            raise ValueError('Incomplete provider inventory')
        for row in batch:
            node = row.get('InstanceId')
            if not node or node in seen or row.get('ScalingGroupId') != group_id:
                raise ValueError('Foreign or duplicate provider member')
            if row.get('LifecycleState') not in ('InService', 'Protected') or row.get('HealthStatus') != 'Healthy':
                raise ValueError('Transitional provider inventory')
            seen.add(node)
            rows.append(row)
        if len(rows) > total:
            raise ValueError('Changed provider inventory')
        if len(rows) == total:
            return rows
    raise ValueError('Incomplete provider inventory')
