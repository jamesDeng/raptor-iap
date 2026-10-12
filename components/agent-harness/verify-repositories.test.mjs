import test from 'node:test'
import assert from 'node:assert/strict'
import {mkdtempSync,mkdirSync,writeFileSync,readFileSync} from 'node:fs'
import {tmpdir} from 'node:os'
import {join} from 'node:path'
import {execFileSync,spawnSync} from 'node:child_process'

test('repository verifier accepts exact origin and commit, names failed public URL',()=>{
 const root=mkdtempSync(join(tmpdir(),'raptor-repos-')), repo=join(root,'repo'), metadata=join(root,'repos.json'), failure=join(root,'failure.json')
 mkdirSync(repo)
 execFileSync('git',['init','-q',repo]);execFileSync('git',['-C',repo,'config','user.email','test@example.com']);execFileSync('git',['-C',repo,'config','user.name','Test'])
 writeFileSync(join(repo,'README.md'),'test');execFileSync('git',['-C',repo,'add','README.md']);execFileSync('git',['-C',repo,'commit','-qm','fixture'])
 execFileSync('git',['-C',repo,'remote','add','origin','https://github.com/example/infra.git'])
 const sha=execFileSync('git',['-C',repo,'rev-parse','HEAD'],{encoding:'utf8'}).trim(), rows=[{url:'https://github.com/example/infra.git',baseCommit:sha,path:repo,directory:'.'}]
 writeFileSync(metadata,JSON.stringify(rows))
 let run=spawnSync(process.execPath,['verify-repositories.mjs',metadata,failure],{cwd:new URL('.',import.meta.url),encoding:'utf8'})
 assert.equal(run.status,0,run.stderr)
 rows[0].baseCommit='b'.repeat(40);writeFileSync(metadata,JSON.stringify(rows))
 run=spawnSync(process.execPath,['verify-repositories.mjs',metadata,failure],{cwd:new URL('.',import.meta.url),encoding:'utf8'})
 assert.notEqual(run.status,0);assert.equal(JSON.parse(readFileSync(failure,'utf8')).url,rows[0].url)
 assert.ok(!run.stdout.includes('https://github.com'))
})
