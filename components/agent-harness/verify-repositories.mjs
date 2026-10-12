import {readFileSync,writeFileSync,realpathSync,rmSync,statSync} from 'node:fs'
import {dirname,isAbsolute,relative,sep} from 'node:path'
import {execFileSync} from 'node:child_process'

const [metadataPath,failurePath]=process.argv.slice(2)
const publicURL=/^https:\/\/github\.com\/[A-Za-z0-9][A-Za-z0-9-]*\/[A-Za-z0-9][A-Za-z0-9_.-]*(?:\.git)?$/
const sha=/^[a-f0-9]{40}$/
let current=''
try {
 if(!isAbsolute(metadataPath)||!isAbsolute(failurePath)) throw Error('invalid path')
 const workspace=realpathSync(dirname(metadataPath))
 const rows=JSON.parse(readFileSync(metadataPath,'utf8'))
 if(!Array.isArray(rows)||rows.length<1||rows.length>32)throw Error('invalid rows')
 for(const row of rows){
  current=typeof row?.url==='string'&&publicURL.test(row.url)?row.url:''
  if(!current||!sha.test(row.baseCommit)||typeof row.path!=='string'||!isAbsolute(row.path)||typeof row.directory!=='string')throw Error('invalid repository')
  const selected=realpathSync(row.path)
  if(!statSync(selected).isDirectory())throw Error('missing directory')
  const repoRoot=execFileSync('git',['-C',selected,'rev-parse','--show-toplevel'],{encoding:'utf8',timeout:15000,maxBuffer:4096,stdio:['ignore','pipe','ignore']}).trim()
  const canonicalRoot=realpathSync(repoRoot)
  const relativeToWorkspace=relative(workspace,canonicalRoot)
  if(relativeToWorkspace===''||relativeToWorkspace==='..'||relativeToWorkspace.startsWith('..'+sep))throw Error('repository outside workspace')
  const relativeToRepo=relative(canonicalRoot,selected)
  if(relativeToRepo==='..'||relativeToRepo.startsWith('..'+sep))throw Error('directory outside repository')
  const origin=execFileSync('git',['-C',canonicalRoot,'remote','get-url','origin'],{encoding:'utf8',timeout:15000,maxBuffer:4096,stdio:['ignore','pipe','ignore']}).trim()
  const head=execFileSync('git',['-C',canonicalRoot,'rev-parse','HEAD'],{encoding:'utf8',timeout:15000,maxBuffer:4096,stdio:['ignore','pipe','ignore']}).trim()
  if(origin!==row.url||head!==row.baseCommit)throw Error('repository mismatch')
 }
 rmSync(failurePath,{force:true})
} catch {
 try {writeFileSync(failurePath,JSON.stringify({url:current}),{mode:0o600})} catch {}
 process.exitCode=1
}
