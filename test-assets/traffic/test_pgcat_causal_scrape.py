"""Bounded causal scrape contracts; all data here are offline fixtures."""
import copy
import hashlib
import importlib.util
import time
import unittest
from unittest.mock import patch

causal = None
if importlib.util.find_spec('pgcat_causal_scrape'):
    import pgcat_causal_scrape as causal

NODES = [dict(id='old-a', address='10.70.1.228'), dict(id='old-b', address='10.70.1.229')]
GROUPS = [dict(node=n['id'], pool='test', user='poc_app') for n in NODES]
IMAGE = 'prom/prometheus@sha256:efd719c99d83b060d9daefdcf00360461adf279f45ef5391f8d111892118753e'
ARGS = ['--config.file=/config/prometheus.yml', '--storage.tsdb.path=/data', '--storage.tsdb.retention.time=2h', '--storage.tsdb.retention.size=128MB']


def labels(node):
    return dict(job='pgcat', env='rdev.ali', db_proxy_code='DP00001', target_db_code='D00002', ecs_instance_id=node['id'], instance=node['address']+':9930')


def matrix(times=(101., 102., 103.), totals=None):
    totals = totals or {'old-a': (0, 0, 0), 'old-b': (0, 0, 0)}
    rows=[]
    for node in NODES:
        for family in ('up', 'active', 'idle', 'waiting'):
            metric=labels(node)
            metric['__name__']='up' if family=='up' else 'pgcat_pools_cl_'+family
            if family!='up':metric.update(pool='test', user='poc_app')
            values=[(1 if family=='up' else totals[node['id']][i] if family=='idle' else 0) for i in range(len(times))]
            rows.append(dict(metric=metric, values=[[t,str(v)] for t,v in zip(times,values)]))
    return dict(status='success', data=dict(resultType='matrix', result=rows))


# Exact independently inspected API definitions; not derived from the validator.
REVIEWED_ACTIVE_RULES = [{'name': 'infra-pgcat-client-observation', 'file': '/config/recording-rules.yaml', 'interval': 5, 'limit': 0, 'rules': [{'type': 'recording', 'name': 'infra_pgcat_connected_clients', 'query': 'sum by (env, db_proxy_code, target_db_code, ecs_instance_id) (pgcat_pools_cl_active + pgcat_pools_cl_idle + pgcat_pools_cl_waiting) unless on (env, db_proxy_code, target_db_code, ecs_instance_id) count by (env, db_proxy_code, target_db_code, ecs_instance_id) (count by (env, db_proxy_code, target_db_code, ecs_instance_id, pool, user) ({__name__=~"pgcat_pools_cl_(active|idle|waiting)"}) != 3)', 'labels': {}}, {'type': 'recording', 'name': 'infra_pgcat_client_observation_timestamp_seconds', 'query': 'min by (env, db_proxy_code, target_db_code, ecs_instance_id) (label_replace(timestamp(pgcat_pools_cl_active), "source_metric", "active", "", "") or label_replace(timestamp(pgcat_pools_cl_idle), "source_metric", "idle", "", "") or label_replace(timestamp(pgcat_pools_cl_waiting), "source_metric", "waiting", "", "")) and on (env, db_proxy_code, target_db_code, ecs_instance_id) infra_pgcat_connected_clients', 'labels': {}}]}]

def loaded_rules():
    groups=copy.deepcopy(REVIEWED_ACTIVE_RULES)
    for group in groups:
        group.update(evaluationTime=.01,lastEvaluation='2026-11-02T00:36:05Z')
        for rule in group['rules']:
            rule.update(health='ok',evaluationTime=.002,lastEvaluation='2026-11-02T00:36:05Z')
    return dict(groups=groups)


def source():
    depmeta=dict(name='rdev-test-metrics-metrics',namespace='raptor-test',uid='metrics-deployment',generation=1)
    podmeta=dict(name='rdev-test-metrics-metrics-abc-pod',namespace='raptor-test',uid='metrics-pod',labels={'app.kubernetes.io/name':'rdev-test-metrics-metrics'},ownerReferences=[dict(kind='ReplicaSet',uid='metrics-rs',controller=True)])
    container=dict(name='prometheus',image=IMAGE,args=ARGS)
    podspec=dict(containers=[container],volumes=[dict(name='config',configMap=dict(name='rdev-test-metrics-metrics')),dict(name='inventory',configMap=dict(name='pgcat-provider-inventory')),dict(name='data',emptyDir=dict(sizeLimit='256Mi'))])
    container['volumeMounts']=[dict(name='config',mountPath='/config',readOnly=True),dict(name='inventory',mountPath='/inventory',readOnly=True),dict(name='data',mountPath='/data')]
    kubernetes=dict(
        deployment=dict(metadata=depmeta,spec=dict(replicas=1,template=dict(spec=copy.deepcopy(podspec))),status=dict(observedGeneration=1,replicas=1,updatedReplicas=1,readyReplicas=1,availableReplicas=1)),
        pods=dict(items=[dict(metadata=podmeta,spec=copy.deepcopy(podspec),status=dict(phase='Running',podIP='10.1.2.3',conditions=[dict(type='Ready',status='True')],containerStatuses=[dict(name='prometheus',ready=True,restartCount=0,containerID='containerd://'+'a'*64,imageID=IMAGE,state=dict(running=dict(startedAt='2026-11-02T00:36:04Z')))]))]),
        replicaSets=dict(items=[dict(metadata=dict(uid='metrics-rs',namespace='raptor-test',ownerReferences=[dict(kind='Deployment',uid='metrics-deployment',controller=True)]))]),
        configMap=dict(metadata=dict(name='rdev-test-metrics-metrics',namespace='raptor-test',uid='metrics-config',resourceVersion='12'),data={'prometheus.yml':'fixture manifest','recording-rules.yaml':'fixture rules'}))
    targets=[dict(scrapePool='pgcat',scrapeUrl='http://'+n['address']+':9930/metrics',labels=labels(n),discoveredLabels=dict(__address__=n['address']+':9930'),health='up',scrapeInterval='5s',scrapeTimeout='3s') for n in NODES]
    return dict(kubernetes=kubernetes,rules=loaded_rules(),config='fixture loaded config',build=dict(version='3.15.0',revision='5241a27fe3c6983549fccc32f6e65917408c63cd'),runtime=dict(startTime='2026-11-02T00:36:04.685058168Z',lastConfigTime='2026-11-02T00:36:05Z',reloadConfigSuccess=True,corruptionCount=0),flags={'web.enable-remote-write-receiver':'false','web.enable-otlp-receiver':'false','enable-feature':'','scrape.discovery-reload-interval':'5s'},targets=dict(activeTargets=targets,droppedTargets=[]))


def fixture_policy():
    policy=copy.deepcopy(causal.SOURCE_POLICY)
    for k,v in [('configSha256','fixture loaded config'),('configMapConfigSha256','fixture manifest'),('rulesSha256','fixture rules')]:
        policy[k]=hashlib.sha256(v.encode()).hexdigest()
    return policy


class CausalSamplesTests(unittest.TestCase):
    def setUp(self):self.assertIsNotNone(causal,'causal scrape kernel missing')
    def validate(self,value=None,watermark=100000,now=104.,maximum=1,groups=None):
        return causal.validate_causal_samples(watermark,matrix() if value is None else value,nodes=NODES,metric_groups=GROUPS if groups is None else groups,now=now,max_combined=maximum)
    def test_two_rejected_three_complete_raw_samples_accepted(self):
        with self.assertRaises(ValueError):self.validate(matrix((101.,102.)))
        result=self.validate()
        self.assertEqual(result['combinedMaximum'],0)
        self.assertEqual([s['timestamp'] for s in result['nodes'][0]['samples']],[101.,102.,103.])
    def test_sum_of_per_node_maxima_never_uses_numerically_latest_low_value(self):
        with self.assertRaises(ValueError):self.validate(matrix(totals={'old-a':(2,0,0),'old-b':(0,0,0)}))
        with self.assertRaises(ValueError):self.validate(matrix(totals={'old-a':(1,0,0),'old-b':(0,1,0)}))
        result=self.validate(matrix(totals={'old-a':(1,0,0),'old-b':(0,0,0)}))
        self.assertEqual(result['combinedMaximum'],1)
        self.assertEqual(result['nodes'][0]['samples'][0]['total'],1)
    def test_replay_future_and_age_boundary(self):
        self.validate(matrix((101.,102.,103.)),now=116.)
        for timestamps,now in [((100.,101.,102.),104.),((99.,100.,101.),104.),((101.,102.,103.),116.0001),((101.,102.,105.),104.)]:
            with self.subTest(timestamps=timestamps,now=now),self.assertRaises(ValueError):self.validate(matrix(timestamps),now=now)
    def test_partial_duplicate_foreign_nonfinite_and_pairing_fail_closed(self):
        changes=[lambda r:r.pop(),lambda r:r.append(copy.deepcopy(r[0])),lambda r:r[1]['metric'].update(instance='10.70.1.9:9930'),lambda r:r[1]['metric'].update(env='other'),lambda r:r[1]['metric'].update(extra='writer'),lambda r:r[0]['values'][0].__setitem__(1,'0'),lambda r:r[1]['values'][0].__setitem__(1,'NaN'),lambda r:r[1]['values'][0].__setitem__(1,'Inf'),lambda r:r[1]['values'][0].__setitem__(1,'-1'),lambda r:r[1]['values'][0].__setitem__(1,'0.5'),lambda r:r[1]['values'][0].__setitem__(0,float('nan')),lambda r:r[1]['values'][0].__setitem__(0,101.5),lambda r:r[1]['values'].append(r[1]['values'][0]),lambda r:r[1]['values'].__setitem__(0,[101.,'0','extra'])]
        for change in changes:
            value=matrix();change(value['data']['result'])
            with self.subTest(change=changes.index(change)),self.assertRaises(ValueError):self.validate(value)
    def test_all_baseline_and_newly_discovered_groups_cover_each_timestamp(self):
        with self.assertRaises(ValueError):self.validate(groups=GROUPS+[dict(node='old-a',pool='new',user='new')])
        value=matrix()
        for row in copy.deepcopy(value['data']['result'][1:4]):
            row['metric'].update(pool='discovered',user='new');row['values'].pop(0);value['data']['result'].append(row)
        with self.assertRaises(ValueError):self.validate(value)
        for row in value['data']['result'][-3:]:row['values'].insert(0,[101.,'0'])
        self.assertEqual(self.validate(value)['combinedMaximum'],0)
    def test_diagnostic_lifts_only_threshold(self):
        value=matrix(totals={'old-a':(8,5,4),'old-b':(3,6,2)})
        self.assertEqual(self.validate(value,maximum=None)['combinedMaximum'],14)
        value['data']['result'].pop()
        with self.assertRaises(ValueError):self.validate(value,maximum=None)
    def test_vector_and_evaluation_time_only_are_not_observations(self):
        value=matrix();value['data']['resultType']='vector'
        with self.assertRaises(ValueError):self.validate(value)
        value=matrix();value['data']['result'][0]['value']=[103.,'1'];del value['data']['result'][0]['values']
        with self.assertRaises(ValueError):self.validate(value)


class CausalSourceTests(unittest.TestCase):
    def setUp(self):
        self.assertIsNotNone(causal,'causal scrape kernel missing')
        self.policy=fixture_policy();self.binding=patch.object(causal,'SOURCE_POLICY',self.policy);self.binding.start();self.addCleanup(self.binding.stop)
    def test_active_rule_projection_rejects_extra_or_changed_loaded_writers(self):
        good=causal.validate_source(source(),self.policy)
        self.assertEqual(good['activeRules'],REVIEWED_ACTIVE_RULES)
        changes=[lambda groups:groups.append(copy.deepcopy(groups[0])),lambda groups:groups[0]['rules'].append(copy.deepcopy(groups[0]['rules'][0])),lambda groups:groups[0]['rules'][0].update(name='up'),lambda groups:groups[0]['rules'][0].update(query='vector(0)'),lambda groups:groups[0]['rules'][0].update(labels={'__name__':'pgcat_pools_cl_idle'}),lambda groups:groups[0].update(file='/unexpected-rules.yaml'),lambda groups:groups[0].update(interval=10),lambda groups:groups[0].update(queryOffset=5),lambda groups:groups[0]['rules'][0].update(health='err',lastError='private error')]
        for change in changes:
            value=source();change(value['rules']['groups'])
            with self.subTest(change=changes.index(change)),self.assertRaises(ValueError):causal.validate_source(value,self.policy)
        value=source();value['rules']['groupNextToken']='truncated'
        with self.assertRaises(ValueError):causal.validate_source(value,self.policy)
        value=source();value.pop('rules')
        with self.assertRaises(ValueError):causal.validate_source(value,self.policy)

    def test_active_rule_runtime_evaluation_progress_does_not_change_definition_identity(self):
        initial=causal.validate_source(source(),self.policy)
        value=source();value['rules']['groups'][0].update(lastEvaluation='2026-11-02T00:36:10Z',evaluationTime=.02)
        value['rules']['groups'][0]['rules'][0].update(lastEvaluation='2026-11-02T00:36:10Z',evaluationTime=.004)
        self.assertEqual(causal.validate_source(value,self.policy),initial)

    def test_kubernetes_default_mode_does_not_change_read_only_mount_identity(self):
        value=source()
        for spec in (value['kubernetes']['deployment']['spec']['template']['spec'],value['kubernetes']['pods']['items'][0]['spec']):
            for volume in spec['volumes'][:2]:volume['configMap']['defaultMode']=420
        self.assertEqual(causal.validate_source(value,self.policy)['podUid'],'metrics-pod')
        value['kubernetes']['pods']['items'][0]['spec']['volumes'][0]['configMap']['items']=[]
        with self.assertRaises(ValueError):causal.validate_source(value,self.policy)

    def test_complete_identity_is_pinned_without_raw_configuration(self):
        result=causal.validate_source(source(),self.policy)
        self.assertEqual(result['podUid'],'metrics-pod')
        self.assertEqual(result['origin'],'http://10.1.2.3:9090')
        self.assertNotIn('fixture loaded config',str(result))
        self.assertEqual(len(result['targets']),2)
    def test_missing_identity_unreviewed_policy_config_rules_and_flags_rejected(self):
        bad_policy=copy.deepcopy(self.policy);bad_policy['configSha256']='0'*64
        with self.assertRaises(ValueError):causal.validate_source(source(),bad_policy)
        changes=[lambda s:s['kubernetes']['pods']['items'][0]['metadata'].pop('uid'),lambda s:s.update(config='changed'),lambda s:s['kubernetes']['configMap']['data'].update({'recording-rules.yaml':'raw writer'}),lambda s:s['runtime'].update(reloadConfigSuccess=False),lambda s:s['flags'].update({'enable-feature':'scrape-on-shutdown'}),lambda s:s['flags'].update({'web.enable-remote-write-receiver':'true'}),lambda s:s['build'].update(revision='unknown'),lambda s:s['kubernetes']['pods']['items'][0]['spec']['containers'][0]['args'].append('--web.enable-admin-api')]
        for change in changes:
            value=source();change(value)
            with self.subTest(change=changes.index(change)),self.assertRaises(ValueError):causal.validate_source(value,self.policy)
    def test_duplicate_loop_foreign_job_or_target_identity_rejected(self):
        changes=[lambda t:t.append(copy.deepcopy(t[0])),lambda t:t[0]['labels'].update(job='test-client'),lambda t:t[0]['labels'].update(instance='10.70.1.3:9930'),lambda t:t[0]['labels'].update(extra='unknown'),lambda t:t[0].update(scrapePool='foreign'),lambda t:t[0]['discoveredLabels'].update(__address__='10.70.1.3:9930')]
        for change in changes:
            value=source();change(value['targets']['activeTargets'])
            with self.subTest(change=changes.index(change)),self.assertRaises(ValueError):causal.validate_source(value,self.policy)


class CausalCollectionTests(CausalSourceTests):
    def setUp(self):
        super().setUp()
        import pgcat_native_controller as native
        self.reader=object.__new__(native.FixedWorkerReads);self.reader.deadline=time.monotonic()+25
        self.current=source();self.calls=[];self.value=matrix()
        def kubernetes():return copy.deepcopy(self.current['kubernetes'])
        def get(path):
            self.calls.append(path)
            if path=='/api/v1/status/tsdb':return dict(status='success',data=dict(headStats=dict(maxTime=100000)))
            if path.startswith('/api/v1/query?'):return copy.deepcopy(self.value)
            key={'/api/v1/status/config':'config','/api/v1/status/buildinfo':'build','/api/v1/status/runtimeinfo':'runtime','/api/v1/status/flags':'flags','/api/v1/targets?state=active':'targets','/api/v1/rules':'rules'}[path]
            data=self.current[key]
            return dict(status='success',data=dict(yaml=data) if key=='config' else copy.deepcopy(data))
        for p in [patch.object(self.reader,'_metrics_kubernetes',side_effect=kubernetes),patch.object(self.reader,'_prometheus_get',side_effect=get),patch('pgcat_source_bootstrap.require_verified')]:
            p.start();self.addCleanup(p.stop)
    def collect(self,pending=None):
        return causal.collect_causal_scrapes(source_read=self.reader.metrics_source,prometheus_get=self.reader.prometheus_get,nodes=NODES,metric_groups=GROUPS,max_combined=1,now=lambda:104.,deadline=self.reader.deadline,pending=pending)
    def test_restored_configmap_cannot_hide_loaded_raw_family_recording_writer(self):
        # Current ConfigMap/config hashes and runtime incarnation remain approved;
        # only the rule manager's active definition differs.
        self.current['rules']=dict(groups=[dict(name='unexpected',file='/config/recording-rules.yaml',interval=5,limit=0,rules=[dict(type='recording',name='pgcat_pools_cl_idle',query='vector(0)',labels={},health='ok')])])
        with self.assertRaises(ValueError):self.collect()

    def test_loaded_rules_are_read_again_after_samples_and_mismatch_refuses_receipt(self):
        get=self.reader._prometheus_get.side_effect
        def changed_after_matrix(path):
            result=get(path)
            if path.startswith('/api/v1/query?'):
                self.current['rules']['groups'][0]['rules'][0]['name']='pgcat_pools_cl_idle'
            return result
        with patch.object(self.reader,'_prometheus_get',side_effect=changed_after_matrix):
            with self.assertRaises(ValueError):self.collect()
        self.assertEqual(self.calls.count('/api/v1/rules'),2)

    def test_real_reader_order_and_process_local_receipt_no_offline_forgery(self):
        result=self.collect();receipt=result['receipt'];evidence=causal.issued_evidence(receipt)
        self.assertEqual(result['status'],'VERIFIED');self.assertEqual(evidence['watermarkMs'],100000)
        self.assertLess(self.calls.index('/api/v1/status/tsdb'),next(i for i,p in enumerate(self.calls) if p.startswith('/api/v1/query?')))
        self.assertEqual(self.calls.count('/api/v1/status/runtimeinfo'),2)
        self.assertEqual(self.calls.count('/api/v1/rules'),2)
        self.assertEqual(evidence['source']['activeRules'],REVIEWED_ACTIVE_RULES)
        for forged in (evidence,copy.copy(receipt),object()):
            with self.assertRaises(ValueError):causal.issued_evidence(forged)
        with self.assertRaises(ValueError):causal.collect_causal_scrapes(source_read=lambda:source(),prometheus_get=lambda p:self.value,nodes=NODES,metric_groups=GROUPS,max_combined=1,now=lambda:104.,deadline=self.reader.deadline)
    def test_pending_remembers_discovered_groups_even_if_next_query_omits_them(self):
        self.value=matrix((101.,102.))
        for row in copy.deepcopy(self.value['data']['result'][1:4]):
            row['metric'].update(pool='discovered',user='new');self.value['data']['result'].append(row)
        ticket=self.collect()['pending'];self.value=matrix()
        with self.assertRaises(ValueError):self.collect(ticket)

    def test_receipt_preserves_raw_timestamp_value_pairs_for_independent_revalidation(self):
        result=self.collect();evidence=causal.issued_evidence(result['receipt'])
        self.assertEqual(evidence['matrix'],self.value)
        self.assertEqual(causal.validate_causal_samples(evidence['watermarkMs'],evidence['matrix'],nodes=NODES,metric_groups=GROUPS,now=104.,max_combined=1)['combinedMaximum'],0)

    def test_pending_keeps_original_watermark_deadline_and_source(self):
        self.value=matrix((101.,102.));result=self.collect()
        self.assertEqual(result['status'],'PENDING');self.assertNotIn('receipt',result)
        ticket=result['pending'];self.value=matrix()
        success=self.collect(ticket);self.assertEqual(success['status'],'VERIFIED')
        self.assertEqual(self.calls.count('/api/v1/status/tsdb'),1)
        with self.assertRaises(ValueError):self.collect(ticket)
    def test_reload_restart_or_pod_swap_invalidate_pending(self):
        changes=[lambda s:s['runtime'].update(lastConfigTime='2026-11-02T00:40:00Z'),lambda s:s['runtime'].update(startTime='2026-11-02T00:40:00Z'),lambda s:s['kubernetes']['pods']['items'][0]['metadata'].update(uid='replacement-pod'),lambda s:s['kubernetes']['pods']['items'][0]['status']['containerStatuses'][0].update(containerID='containerd://'+'b'*64)]
        for change in changes:
            self.current=source();self.value=matrix((101.,102.));ticket=self.collect()['pending'];change(self.current);self.value=matrix()
            with self.subTest(change=changes.index(change)),self.assertRaises(ValueError):self.collect(ticket)
    def test_collection_deadline_bounds_source_http_even_with_longer_reader_deadline(self):
        import socket,threading
        import pgcat_native_controller as native
        client,server=socket.socketpair();self.addCleanup(client.close)
        def serve():
            with server:
                server.recv(4096)
                for byte in b'HTTP/1.1 200 OK\r\n':
                    try:server.send(bytes([byte]));time.sleep(.1)
                    except OSError:break
        threading.Thread(target=serve,daemon=True).start();started=time.monotonic()
        with patch.object(self.reader,'_prometheus_get',side_effect=lambda p:native.FixedWorkerReads._prometheus_get(self.reader,p)),patch('http.client.HTTPConnection.connect',new=lambda conn:setattr(conn,'sock',client)):
            with self.assertRaises(ValueError):
                causal.collect_causal_scrapes(source_read=self.reader.metrics_source,prometheus_get=self.reader.prometheus_get,nodes=NODES,metric_groups=GROUPS,max_combined=1,now=lambda:104.,deadline=started+.2)
        self.assertLess(time.monotonic()-started,.8)

    def test_deadline_exhaustion_and_sanitized_error(self):
        self.reader.deadline=time.monotonic()-1
        with self.assertRaises(ValueError) as caught:self.collect()
        self.assertEqual(str(caught.exception),'causal scrape');self.assertIsNone(caught.exception.__context__)
        self.reader.deadline=time.monotonic()+25
        with patch.object(self.reader,'_prometheus_get',side_effect=RuntimeError('private credential')):
            with self.assertRaises(ValueError) as caught:self.collect()
        self.assertNotIn('private',str(caught.exception));self.assertIsNone(caught.exception.__context__)
    def test_four_target_source_still_counts_only_bound_original_pair(self):
        for node in [dict(id='new-a',address='10.70.1.230'),dict(id='new-b',address='10.70.1.231')]:
            target=copy.deepcopy(self.current['targets']['activeTargets'][0])
            target.update(labels=labels(node),scrapeUrl='http://'+node['address']+':9930/metrics',discoveredLabels=dict(__address__=node['address']+':9930'))
            self.current['targets']['activeTargets'].append(target)
        evidence=causal.issued_evidence(self.collect()['receipt'])
        self.assertEqual(len(evidence['source']['targets']),4)
        self.assertEqual([n['node'] for n in evidence['samples']['nodes']],['old-a','old-b'])
        row=copy.deepcopy(self.value['data']['result'][0]);row['metric'].update(ecs_instance_id='new-a',instance='10.70.1.230:9930')
        self.value['data']['result'].append(row)
        with self.assertRaises(ValueError):self.collect()

    def test_history_cannot_create_success_and_original_addresses_must_match(self):
        self.value=matrix((98.,99.,100.));self.assertEqual(self.collect()['status'],'PENDING')
        self.current['targets']['activeTargets'][0]['labels']['ecs_instance_id']='new-node'
        with self.assertRaises(ValueError):self.collect()


if __name__=='__main__':unittest.main()
