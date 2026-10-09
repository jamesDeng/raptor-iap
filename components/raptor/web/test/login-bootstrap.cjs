const {chromium}=require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const http=require('http'),fs=require('fs'),path=require('path');
(async()=>{
const root=process.argv[2] || path.resolve(__dirname,'../poc');let release;const gate=new Promise(r=>release=r);let leaked=false,posts=0;
const server=http.createServer(async(req,res)=>{const u=new URL(req.url,'http://local');if(u.searchParams.has('password'))leaked=true;
if(u.pathname==='/worklog.js'){await gate;res.setHeader('Content-Type','text/javascript');res.end('export function updateWorklog(){};export function resetWorklog(){}');return}
if(u.pathname==='/api/v1/session'){res.writeHead(401,{'Content-Type':'application/json'}).end(JSON.stringify({error:{code:'Unauthorized'}}));return}
if(u.pathname==='/api/v1/login'){posts++;res.setHeader('Content-Type','application/json');res.end(JSON.stringify({data:{csrf:'fixture',user:{username:'fixture'}}}));return}
if(u.pathname.startsWith('/api/v1/')){res.setHeader('Content-Type','application/json');res.end(JSON.stringify({data:u.pathname==='/api/v1/operation-schemas'?{}:[]}));return}
const file=path.join(root,u.pathname==='/'?'index.html':u.pathname);if(!file.startsWith(root)||!fs.existsSync(file)){res.writeHead(404).end();return}res.setHeader('Content-Type',file.endsWith('.js')?'text/javascript':file.endsWith('.css')?'text/css':'text/html');res.end(fs.readFileSync(file))});
await new Promise(r=>server.listen(55442,'127.0.0.1',r));const browser=await chromium.launch({headless:true,executablePath:process.env.BROWSER_EXECUTABLE});
try{const page=await browser.newPage();await page.goto('http://127.0.0.1:55442',{waitUntil:'domcontentloaded'});
const submit=page.getByRole('button',{name:'Sign in',exact:true});if(await submit.isEnabled())throw Error('Login submit is active before its handler loads');
await page.getByLabel('Username',{exact:true}).fill('fixture');await page.getByLabel('Password',{exact:true}).fill('synthetic-fixture-secret');await page.getByLabel('Password',{exact:true}).press('Enter');if(leaked||posts)throw Error('Early form submission escaped the initialization gate');
release();await page.waitForResponse(r=>new URL(r.url()).pathname==='/api/v1/session');await submit.click();await page.locator('#identity').waitFor({state:'visible'});if(leaked||posts!==1)throw Error('Ready login did not use exactly one protected POST');
console.log('Delayed-module login: blocked early submission, no credentials in URL, ready JSON POST passed');
}finally{release();await browser.close();await new Promise(r=>server.close(r))}
})().catch(e=>{console.error(e.message);process.exit(1)});
