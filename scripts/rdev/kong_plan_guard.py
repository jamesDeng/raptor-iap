"""Fail-closed action scope for the reviewed rdev Kong Terraform plan.

Caller must independently read STS identity, VPC ownership and a complete price
receipt; this function validates those inputs, not authentication itself.
"""
import math
OWNED={'account_id':'1360282071200743','region':'ap-southeast-1','vpc_id':'vpc-t4n4fi6r1a7bi6n93ftq3','vswitch_id':'vsw-t4nop9qf6v46gw2sa8l7d'}
LB='module.kong_ingress[0].alicloud_slb_load_balancer.proxy'
DNS={f'module.kong_ingress[0].alicloud_alidns_record.public["{name}"]':name for name in ['raptor.rdev','api.rdev']}
def validate_plan(plan:dict,ownership:dict,cost_receipt:dict)->dict:
 if ownership!=OWNED:raise ValueError('owned account, region, VPC and subnet required')
 c=cost_receipt
 if c.get('verified') is not True or c.get('currency')!='CNY' or c.get('includes_instance_ip_lcu_traffic') is not True:raise ValueError('complete verified CNY cost receipt required')
 total=c.get('estimated_72h_total');remaining=c.get('remaining_budget')
 if any(isinstance(v,bool) or not isinstance(v,(int,float)) or not math.isfinite(v) or v<=0 for v in [total,remaining]) or total>remaining or remaining>1000:raise ValueError('POC budget exceeded or invalid')
 seen=set();creates=0
 for r in plan.get('resource_changes',[]):
  a=r['change']['actions']
  if a==['no-op']:continue
  addr=r.get('address');after=r['change'].get('after',{})
  if addr in seen or a!=['create'] or addr not in {LB,*DNS}:raise ValueError('unreviewed resource action')
  seen.add(addr);creates+=1
  if addr==LB:
   expected={'address_type':'internet','vswitch_id':OWNED['vswitch_id'],'instance_charge_type':'PayByCLCU','internet_charge_type':'paybytraffic','master_zone_id':'ap-southeast-1a','slave_zone_id':'ap-southeast-1b','tags':{'Project':'raptor-iap','Environment':'rdev.ali','Owner':'kong-ingress'}}
   if r.get('type')!='alicloud_slb_load_balancer' or any(after.get(k)!=v for k,v in expected.items()):raise ValueError('foreign or incorrect public load balancer')
  elif r.get('type')!='alicloud_alidns_record' or after.get('domain_name')!='raptor-iap.top' or after.get('rr')!=DNS[addr] or after.get('type')!='A':raise ValueError('foreign DNS record')
 if not creates:raise ValueError('empty creation plan')
 return {'creates':creates,'updates':0,'deletes':0}
