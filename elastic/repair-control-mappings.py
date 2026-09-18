#!/usr/bin/env python3
"""Operator-only migration of legacy control indices, preserving every document.

Run with setup credentials in the environment. A verified archive is created
before the old concrete name is atomically replaced by an alias to a correctly
mapped empty index. Legacy fixtures remain in the archive, not the verdict path.
"""
import datetime,json,os,pathlib,urllib.request
base=os.environ['ELASTIC_CLOUD_ENDPOINT'];key=os.environ['ELASTIC_API_KEY']
def call(method,path,body=None):
 req=urllib.request.Request(base+path,method=method,data=None if body is None else json.dumps(body).encode(),headers={'Authorization':'ApiKey '+key,'Content-Type':'application/json'})
 with urllib.request.urlopen(req,timeout=90) as r:return json.load(r)
stamp=datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ').lower()
for kind in ['contracts','ledger']:
 name='sloproof-'+kind;mapping=json.load(open('elastic/templates/'+name+'.json'))['template']['mappings']
 current=call('GET','/'+name+'/_mapping')
 if len(current)!=1:raise SystemExit('Refusing multiple backing indices')
 backing,old=next(iter(current.items()))
 if old['mappings'].get('properties')==mapping['properties'] and old['mappings'].get('dynamic')==mapping['dynamic']:
  print(name,'already correct');continue
 if backing!=name:raise SystemExit('Refusing to replace existing alias')
 docs=call('POST','/'+name+'/_search',{'size':10000,'track_total_hits':True,'query':{'match_all':{}}})['hits']
 if docs['total']['value']!=len(docs['hits']):raise SystemExit('Too many legacy records for safe migration')
 archive='sloproof-archive-'+kind+'-'+stamp
 call('PUT','/'+archive,{'mappings':old['mappings']})
 result=call('POST','/_reindex?refresh=true',{'source':{'index':name},'dest':{'index':archive,'op_type':'create'}})
 if result.get('failures') or result.get('created')!=len(docs['hits']):raise SystemExit('Archive reindex failed; original untouched')
 copied=call('POST','/'+archive+'/_search',{'size':10000,'query':{'match_all':{}}})['hits']['hits']
 normalize=lambda hits:{h['_id']:h['_source'] for h in hits}
 if normalize(copied)!=normalize(docs['hits']):raise SystemExit('Archive mismatch; original untouched')
 new=name+'-v1-'+stamp
 call('PUT','/'+new,{'mappings':mapping})
 pathlib.Path('evidence').mkdir(exist_ok=True)
 receipt={'original':name,'archive':archive,'replacement':new,'preserved_count':len(copied),'legacy_documents':normalize(copied),'at':stamp}
 # Durable local backup and remote archive both exist before the alias swap.
 pathlib.Path('evidence/'+kind+'-mapping-migration.json').write_text(json.dumps(receipt,indent=2)+'\n')
 call('POST','/_aliases',{'actions':[{'remove_index':{'index':name}},{'add':{'index':new,'alias':name,'is_write_index':True}}]})
 print(name,'repaired;',len(copied),'legacy documents preserved in',archive)
