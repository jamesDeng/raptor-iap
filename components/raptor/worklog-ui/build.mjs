import postcss from 'postcss';
import {build} from 'esbuild';
import {execFileSync} from 'node:child_process';
import {readFileSync,writeFileSync} from 'node:fs';
const paths=['../web/poc/worklog.js','../web/poc/worklog.css'];
const prior=process.argv.includes('--check')?paths.map(path=>readFileSync(path,'utf8')):null;
await build({entryPoints:['src/index.tsx'],bundle:true,format:'esm',minify:true,jsx:'automatic',target:'es2022',outfile:'../web/poc/worklog.js',define:{'process.env.NODE_ENV':'"production"'},legalComments:'eof'});
execFileSync('node',['node_modules/@tailwindcss/cli/dist/index.mjs','-i','src/styles.css','-o','../web/poc/worklog.css','--minify']);
// Keep upstream attribution in the distributed assets as well as source.
const license=readFileSync('src/vendor/LICENSE','utf8');
writeFileSync('../web/poc/worklog.js','/* T3 WorkLog, MIT license:\n'+license+'*/\n'+readFileSync('../web/poc/worklog.js','utf8'));

const cssPath='../web/poc/worklog.css';
const css=postcss.parse(readFileSync(cssPath,'utf8'));
css.walkRules(rule=>{
 if(rule.parent.type==='atrule'&&/keyframes$/.test(rule.parent.name))return;
 rule.selectors=rule.selectors.map(selector=>[':root',':host'].includes(selector)?'.t3-worklog':selector.startsWith('.t3-worklog')?selector:`.t3-worklog ${selector}`);
});
writeFileSync(cssPath,css.toString());

if(prior&&paths.some((path,index)=>readFileSync(path,'utf8')!==prior[index]))throw new Error('Generated assets are stale. Run npm run build and commit both assets.');
