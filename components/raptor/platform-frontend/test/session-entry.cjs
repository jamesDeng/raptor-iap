const {chromium}=require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const http=require('node:http'),fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
(async()=>{
 const root=path.resolve(__dirname,'../poc');
 const server=http.createServer((req,res)=>{
  const name=new URL(req.url,'http://fixture').pathname;
  if(name==='/worklog.js'){res.setHeader('Content-Type','text/javascript');res.end('export function updateWorklog(){};export function resetWorklog(){}');return;}
  const file=path.join(root,name==='/'?'index.html':name);
  if(!file.startsWith(root+path.sep)||!fs.existsSync(file)){res.writeHead(404).end();return;}
  res.setHeader('Content-Type',file.endsWith('.js')?'text/javascript':file.endsWith('.css')?'text/css':'text/html');res.end(fs.readFileSync(file));
 });
 await new Promise(r=>server.listen(0,'127.0.0.1',r));
 const browser=await chromium.launch({headless:true,executablePath:process.env.BROWSER_EXECUTABLE});
 try{
  for(const mode of ['valid','expired','session-unavailable','catalog-unavailable']){
   const page=await browser.newPage();let release,seen;const gate=new Promise(r=>release=r),sessionSeen=new Promise(r=>seen=r);let sessionCalls=0,loginCalls=0;
   await page.route('**/api/v1/**',async route=>{
    const endpoint=new URL(route.request().url()).pathname;let status=200,data=[];
    if(endpoint==='/api/v1/session'){sessionCalls++;if(sessionCalls===1){seen();await gate;}status=mode==='expired'?401:mode==='session-unavailable'&&sessionCalls===1?503:200;data={csrf:'fixture',user:{username:'fixture'}};}
    if(endpoint==='/api/v1/login')loginCalls++;
    if(endpoint==='/api/v1/objects'&&mode==='catalog-unavailable'&&sessionCalls===1)status=503;
    if(endpoint==='/api/v1/operation-schemas')data={};
    if(endpoint==='/api/v1/model-providers')data={models:[]};
    await route.fulfill({status,json:status===200?{data}:{error:{code:status===401?'Unauthenticated':'Unavailable'}}});
   });
   await page.goto(`http://127.0.0.1:${server.address().port}/#catalog=database`,{waitUntil:'domcontentloaded'});await sessionSeen;
   try{
    assert.equal(await page.locator('#login-page').isVisible(),false,`${mode}: login must stay hidden while checking session`);
    assert.equal(await page.locator('#workspace').isVisible(),false);
    release();
    if(mode==='expired')await page.locator('#login-page').waitFor({state:'visible'});
    else if(mode==='valid'){
     await page.locator('#workspace').waitFor({state:'visible'});
     assert.equal(await page.locator('#catalog-heading').textContent(),'Database list');
     assert.equal(await page.locator('#login-page').isVisible(),false);
    }else{
     await page.getByRole('button',{name:'Retry',exact:true}).waitFor({state:'visible'});
     assert.equal(await page.locator('#login-page').isVisible(),false,`${mode}: service failure must not sign out`);
     assert.equal(new URL(page.url()).hash,'#catalog=database');
     await page.getByRole('button',{name:'Retry',exact:true}).click();
     await page.locator('#workspace').waitFor({state:'visible'});
     assert.equal(await page.locator('#catalog-heading').textContent(),'Database list');
    }
    assert.equal(loginCalls,0,'existing session entry must never submit credentials');
    console.log(`${mode}: passed`);
   }finally{release();await page.close();}
  }
 }finally{await browser.close();await new Promise(r=>server.close(r));}
})().catch(error=>{console.error(error);process.exitCode=1;});
