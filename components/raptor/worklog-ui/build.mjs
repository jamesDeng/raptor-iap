import {build} from 'esbuild';
import {readFileSync,writeFileSync} from 'node:fs';
const paths=['../web/poc/worklog.js','../web/poc/worklog.css'];
const prior=process.argv.includes('--check')?paths.map(path=>readFileSync(path,'utf8')):null;
await build({entryPoints:['src/index.tsx'],bundle:true,format:'esm',minify:true,jsx:'automatic',target:'es2022',outfile:paths[0],define:{'process.env.NODE_ENV':'"production"'},legalComments:'eof'});
// Retain the assistant-ui MIT notice in the distributed embedded asset.
const licenses=[...new Set(['react','core','store'].map(name=>readFileSync(`node_modules/@assistant-ui/${name}/LICENSE`,'utf8')))];
writeFileSync(paths[0],'/* assistant-ui licenses:\n'+licenses.join('\n')+'*/\n'+readFileSync(paths[0],'utf8'));
writeFileSync(paths[1],readFileSync('src/styles.css','utf8'));
if(prior&&paths.some((path,index)=>readFileSync(path,'utf8')!==prior[index]))throw new Error('Generated assets are stale. Run npm run build and commit both assets.');
