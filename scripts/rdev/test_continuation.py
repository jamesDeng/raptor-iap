import copy
import unittest
from test_live_plan import fixture
from live_plan import validate_plan, PARENT_REFERENCES

PINS={key:'owned-'+key for key in ('alicloud_vpc.env','alicloud_vswitch.workers','alicloud_vswitch.database','alicloud_eip_address.outbound','alicloud_db_instance.platform','terraform_data.account_guard','terraform_data.service_role_guard')}
def continuation():
    p=fixture(); changes={r['address'].removeprefix('module.foundation.'):r['change'] for r in p['resource_changes']}
    for key,id in PINS.items():
        c=changes[key];c['actions']=['no-op'];c['after']['id']=id
        if key=='alicloud_eip_address.outbound':c['after']['ip_address']='192.0.2.1'
        if key.startswith('alicloud_'):c['after']['tags']={'Project':'raptor-iap','Environment':'rdev.ali','Owner':'rdev-foundation'}
    for key,fields in PARENT_REFERENCES.items():
        for field,ref in fields.items():
            parent,attribute=ref.rsplit('.',1)
            if parent in PINS:
                value=changes[parent]['after'][attribute]
                changes[key]['after'][field]=[value] if field=='vswitch_ids' else value
                changes[key]['after_unknown'][field]=[False] if field=='vswitch_ids' else False
    for key in PINS:changes[key]['before']=copy.deepcopy(changes[key]['after'])
    return p
class Continuation(unittest.TestCase):
    def test_only_five_missing_resources_create(self):
        s=validate_plan(continuation(),'123456',preserved=PINS)
        self.assertEqual((s['cloud_creates'],s['unchanged'],s['local_guards']),(5,7,0))
    def test_wrong_id_recreate_update_or_drift_stops(self):
        for kind in ('id','recreate','update','drift','changed_noop'):
            p=continuation();c=p['resource_changes'][0]['change']
            if kind=='id':c['after']['id']='foreign';c['before']['id']='foreign'
            if kind=='recreate':c['actions']=['create']
            if kind=='update':c['actions']=['update']
            if kind=='drift':p['resource_drift']=[{}]
            if kind=='changed_noop':c['after']['extra']='change'
            with self.subTest(kind=kind),self.assertRaises(ValueError):validate_plan(p,'123456',preserved=PINS)
    def test_reused_foreign_parent_and_wrong_owner_stop(self):
        p=continuation();nat=next(r['change'] for r in p['resource_changes'] if r['type']=='alicloud_nat_gateway');nat['after']['vpc_id']='foreign'
        with self.assertRaises(ValueError):validate_plan(p,'123456',preserved=PINS)
        p=continuation();c=p['resource_changes'][0]['change'];c['before']['tags']=c['after']['tags']={'Environment':'prod'}
        with self.assertRaises(ValueError):validate_plan(p,'123456',preserved=PINS)
    def test_extra_or_incomplete_pinset_stops(self):
        for pins in ({}, {**PINS,'alicloud_nat_gateway.outbound':'foreign'}):
            with self.assertRaises(ValueError):validate_plan(continuation(),'123456',preserved=pins)

class Normalization(unittest.TestCase):
    def test_only_observed_null_to_empty_refresh_is_allowed(self):
        p=continuation()
        for key, field, empty in [('alicloud_db_instance.platform','template_id_list',[]),('alicloud_eip_address.outbound','security_protection_types',[]),('alicloud_vswitch.workers','tags',{})]:
            b={'id':PINS[key],field:None};a={'id':PINS[key],field:empty}
            p.setdefault('resource_drift',[]).append({'address':'module.foundation.'+key,'mode':'managed','change':{'actions':['update'],'before':b,'after':a}})
        self.assertTrue(validate_plan(p,'123456',preserved=PINS)['passed'])
        p['resource_drift'][0]['change']['after']['template_id_list']=['unexpected']
        with self.assertRaises(ValueError):validate_plan(p,'123456',preserved=PINS)
