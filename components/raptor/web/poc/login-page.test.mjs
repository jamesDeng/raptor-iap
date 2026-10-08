import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import * as app from './app.js';
test('signed-out view excludes platform chrome and keeps errors beside the login form',()=>{
 const nodes=Object.fromEntries(['platform-sidebar','workspace','workspace-skip','login-page','message','login-feedback','workspace-feedback'].map(id=>[id,{hidden:false,children:[],append(child){this.children.push(child)}}]));
 const document={getElementById:id=>nodes[id]};
 app.setSessionView(document,false);
 for(const id of ['platform-sidebar','workspace','workspace-skip'])assert.equal(nodes[id].hidden,true);
 assert.equal(nodes['login-page'].hidden,false);assert.equal(nodes['login-feedback'].children.at(-1),nodes.message);
 app.setSessionView(document,true);
 for(const id of ['platform-sidebar','workspace','workspace-skip'])assert.equal(nodes[id].hidden,false);
 assert.equal(nodes['login-page'].hidden,true);assert.equal(nodes['workspace-feedback'].children.at(-1),nodes.message);
});
test('login page is outside the workspace and platform starts hidden before session loading',()=>{
 const html=readFileSync(new URL('./index.html',import.meta.url),'utf8');
 assert.match(html,/<aside id="platform-sidebar" hidden>/);
 assert.match(html,/<main id="workspace"[^>]* hidden>/);
 const workspaceStart=html.indexOf('<main id="workspace"'),workspaceEnd=html.indexOf('</main>',workspaceStart);
 assert.ok(html.indexOf('id="login-panel"')>workspaceEnd);
 assert.match(html,/<main id="login-page"/);
});
test('signed-out URL changes keep the login page instead of revealing cached workspace',()=>{
 const source=readFileSync(new URL('./app.js',import.meta.url),'utf8');
 const body=source.match(/window.addEventListener\('hashchange',\(\)=>\{([^\n]+?)\}\);/)[1];
 let restored=0;
 const change=new Function('csrf','location','activeRoute','restoreRoute','showError',body);
 change('',{hash:'#catalog=application'},'',()=>{restored++;return Promise.resolve()},()=>{});
 assert.equal(restored,0);
 change('authenticated',{hash:'#catalog=application'},'',()=>{restored++;return Promise.resolve()},()=>{});
 assert.equal(restored,1);
});
