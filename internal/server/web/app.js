'use strict';
const $ = id => document.getElementById(id);
const fragment = new URLSearchParams(location.hash.slice(1));
let token = fragment.get('token') || sessionStorage.getItem('workbench-token') || '';
if (fragment.has('token')) {
  sessionStorage.setItem('workbench-token', token);
  history.replaceState(null, '', location.pathname);
}
let project = null;
let selected = '';
let selectedKind = 'document';
let markdown = '';
let raw = false;
let readSequence = 0;
let revision = "";
let editable = false;
let editing = false;
let saving = false;
const escapeHTML = text => text.replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;');


function showError(error) {
  $('error').textContent = error ? String(error.message || error) : '';
  $('error').hidden = !error;
}
async function api(path, body) {
  const response = await fetch('/api/' + path, {
    method: body ? 'POST' : 'GET',
    headers: {'X-Workbench-Token': token, ...(body ? {'Content-Type': 'application/json'} : {})},
    ...(body ? {body: JSON.stringify(body)} : {})
  });
  const result = await response.json();
  if (!response.ok) {const error=new Error(result.error || '读取失败');error.status=response.status;throw error;}
  return result;
}
function setBusy(form, busy) {
  for (const element of form.elements) element.disabled = busy;
}
for (const action of ['open', 'new']) {
  const form = $(action + '-form');
  form.addEventListener('submit', async event => {
    event.preventDefault();
    const body = {action, path: $(action + '-path').value.trim()};
    if (action === 'new') body.name = $('new-name').value.trim();
    showError(null);setBusy(form, true);
    try {await showProject(await api('project', body));}
    catch (error) {showError(error);}
    finally {setBusy(form, false);}
  });
}
function entries() {
  return [...(project?.documents || []).map(d => ({...d, kind:'document'})), ...(project?.diagrams || []).map(d => ({...d, kind:'diagram'}))];
}
function isEngineering(item) {
  const path=item.path;
  return ['AGENTS.md','CLAUDE.md','THIRD_PARTY.md','docs/project_access_plan.md'].includes(path)
    || path.startsWith('docs/standards/') || path.endsWith('/spec-template.md') || path.endsWith('/README.md')
    || /^docs\/reviews\/(stage-one|initialization|discussion)/.test(path);
}
// 目录展开状态只是浏览便利，按项目保存在本机浏览器；存储不可用时退回默认展开 docs。
const openFolders=new Set();
let folderStateKey='';
function loadFolderState(){
  const key='workbench-folders:'+(project?.root||'');if(key===folderStateKey)return;
  folderStateKey=key;openFolders.clear();
  try{for(const path of JSON.parse(localStorage.getItem(key)||'["docs"]'))openFolders.add(String(path));}catch{openFolders.add('docs');}
}
function saveFolderState(){try{localStorage.setItem(folderStateKey,JSON.stringify([...openFolders]));}catch{}}
function revealPath(path){const parts=path.split('/');for(let i=1;i<parts.length;i++)openFolders.add(parts.slice(0,i).join('/'));saveFolderState();}
function buildTree(items){
  const root={path:'',folders:new Map(),files:[],count:0};
  for(const item of items){
    const parts=item.path.split('/');let node=root;node.count++;
    for(let i=0;i<parts.length-1;i++){
      if(!node.folders.has(parts[i]))node.folders.set(parts[i],{name:parts[i],path:parts.slice(0,i+1).join('/'),folders:new Map(),files:[],count:0});
      node=node.folders.get(parts[i]);node.count++;
    }
    node.files.push(item);
  }
  return root;
}
function renderList() {
  loadFolderState();
  const query=$('filter').value.trim().toLocaleLowerCase();
  const all=entries().filter(d=>d.path.toLocaleLowerCase().includes(query));
  const normal=all.filter(d=>!isEngineering(d)),engineering=all.filter(isEngineering);
  $('document-count').textContent=String(normal.length);$('engineering-count').textContent=String(engineering.length);$('engineering-files').hidden=!engineering.length;
  const byName=(a,b)=>a.localeCompare(b,'zh-CN',{numeric:true});
  function drawNode(container,node){
    for(const folder of [...node.folders.values()].sort((a,b)=>byName(a.name,b.name))){
      const details=document.createElement('details'),summary=document.createElement('summary'),name=document.createElement('span'),count=document.createElement('small'),children=document.createElement('div');
      details.className='tree-folder';details.dataset.folder=folder.path;details.open=!!query||openFolders.has(folder.path);
      name.textContent=folder.name;count.textContent=String(folder.count);summary.title=folder.path;summary.append(name,count);
      summary.addEventListener('click',()=>{if(details.open)openFolders.delete(folder.path);else openFolders.add(folder.path);saveFolderState();});
      children.className='tree-children';drawNode(children,folder);details.append(summary,children);container.append(details);
    }
    for(const item of node.files.sort((a,b)=>byName(a.path,b.path))){
      const button=document.createElement('button');button.type='button';button.className='file-button';button.dataset.path=item.path;button.title=item.path;
      button.classList.toggle('active',item.path===selected);button.setAttribute('aria-current',item.path===selected?'page':'false');
      const name=document.createElement('strong');name.textContent=item.path.split('/').at(-1);button.append(name);
      if(item.kind==='diagram'){const detail=document.createElement('small');detail.textContent='交互式 HTML';button.append(detail);}
      button.addEventListener('click',()=>loadEntry(item));container.append(button);
    }
  }
  for(const [container,items] of [[$('document-list'),normal],[$('engineering-list'),engineering]]){
    container.replaceChildren();drawNode(container,buildTree(items));
    if(!items.length){const empty=document.createElement('p');empty.className='empty-list';empty.textContent=query?'没有匹配的文件':'暂无文件';container.append(empty);}
  }
}
function dirty(){return editing&&$('markdown-editor').value!==markdown;}
function leaveEditor(){
  if(saving)return false;
  if(dirty()&&!window.confirm('有未保存的修改，是否放弃？'))return false;
  editing=false;$('editor-panel').hidden=true;$('save-button').hidden=true;$('cancel-button').hidden=true;$('edit-button').hidden=selectedKind==='diagram';$('raw-button').hidden=selectedKind==='diagram';return true;
}
async function showProject(info) {
  if(!leaveEditor())return;project=info;$('welcome').hidden=!!info;$('workspace').hidden=!info;
  $('mode-switch').hidden=!info;
  if (!info) return;
  $('project-name').textContent=info.name;$('project-root').textContent=info.root;
  document.title=`${info.name} · Agent Workbench`;
  $('warning-list').replaceChildren();
  for(const warning of info.warnings){const li=document.createElement('li');li.textContent=warning;$('warning-list').append(li);}
  $('warning-summary').textContent=`${info.warnings.length} 项需要补充 · 点击查看`;$('warnings').hidden=!info.warnings.length;
  renderList();
  try {await loadCollaboration();}catch(error){showError(error);}
  const all=entries();const target=all.find(d=>d.path===selected)||all.find(d=>d.path==='PLAN.md')||all.find(d=>d.path==='README.md')||all[0];
  $('empty').hidden=!!target;
  if(target) await loadEntry(target);else {selected='';$('article').replaceChildren();$('raw-content').hidden=true;$('diagram-frame').hidden=true;$('diagram-frame').removeAttribute('srcdoc');$('current-path').textContent='暂无文档';$('raw-button').hidden=true;}
  await loadAgentProject();
}

function renderProjectMarkdown(container,text,basePath){
 renderSafeMarkdown(container,text,{basePath,paths:entries().map(item=>item.path),onNavigate:path=>{const target=entries().find(item=>item.path===path);if(target&&(!agentMode||switchWorkbench('documents')))loadEntry(target);}});
}
function renderMarkdown() {
  $('raw-button').setAttribute('aria-pressed',String(raw));
  $('raw-content').hidden=!raw;$('article').hidden=raw;$('raw-content').textContent=markdown;
  renderProjectMarkdown($('article'),markdown,selected);
}
async function loadEntry(item) {
  if(!leaveEditor())return;
  resetDiscussionSelection();const sequence=++readSequence;selected=item.path;selectedKind=item.kind;loadFolderState();revealPath(item.path);markdown='';$('article').replaceChildren();$('raw-content').textContent='';$('raw-button').disabled=true;$('edit-button').disabled=true;editable=false;revision='';$('save-status').textContent='';renderList();renderThreads();if(isEngineering(item))$('engineering-files').open=true;showError(null);
  $('current-path').textContent=item.path;$('view-kind').textContent=item.kind==='diagram'?'项目交互图':'项目文档';$('loading').hidden=false;$('empty').hidden=true;
  $('article').hidden=true;$('raw-content').hidden=true;$('diagram-frame').hidden=true;$('diagram-frame').removeAttribute('srcdoc');$('raw-button').hidden=item.kind==='diagram';$('edit-button').hidden=item.kind==='diagram';$('diagram-help').hidden=item.kind!=='diagram';
  try {
    const data=await api(`${item.kind}?path=${encodeURIComponent(item.path)}`);
    if(sequence!==readSequence)return;
    if(item.kind==='diagram') {
      // sandbox 隔离父页面；独立策略禁止图连接网络和本地 API。
      const policy="default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: blob:; font-src data:; connect-src 'none'; frame-src 'none'; base-uri 'none'; form-action 'none'";
      $('diagram-frame').srcdoc=`<!doctype html><meta http-equiv="Content-Security-Policy" content="${policy}">`+data.html;
      $('diagram-frame').hidden=false;
    } else {markdown=data.markdown;revision=data.revision;editable=data.editable;$('raw-button').disabled=false;$('edit-button').disabled=!editable;$('edit-button').title=editable?'':'此文件为只读';renderMarkdown();renderThreads();}
  } catch(error) {if(sequence===readSequence)showError(error);}
  finally {if(sequence===readSequence)$('loading').hidden=true;}
}
$('filter').addEventListener('input',renderList);
$('raw-button').addEventListener('click',()=>{if(selectedKind==='document'&&!$('raw-button').disabled){raw=!raw;renderMarkdown();}});
$('refresh-button').addEventListener('click',async()=>{
  if(!leaveEditor())return;$('refresh-button').disabled=true;showError(null);
  try{await showProject(await api('project'));}catch(error){showError(error);}finally{$('refresh-button').disabled=false;}
});
window.addEventListener('DOMContentLoaded',async()=>{try{if(!token)throw new Error('请使用终端显示的完整地址打开工作台。');await showProject(await api('project'));}catch(error){$('welcome').hidden=false;showError(error);}},{once:true});

$('edit-button').addEventListener('click',()=>{
  if(!editable||saving)return;editing=true;raw=false;$('markdown-editor').value=markdown;$('editor-panel').hidden=false;$('article').hidden=true;$('raw-content').hidden=true;$('raw-button').hidden=true;$('edit-button').hidden=true;$('save-button').hidden=false;$('cancel-button').hidden=false;$('save-status').textContent='';$('markdown-editor').focus();
});
$('markdown-editor').addEventListener('input',()=>{$('save-status').textContent=dirty()?'未保存':'';});
$('cancel-button').addEventListener('click',()=>{if(leaveEditor()){renderMarkdown();$('save-status').textContent='';}});
$('save-button').addEventListener('click',async()=>{
  if(!editing||saving)return;saving=true;$('save-button').disabled=true;$('cancel-button').disabled=true;$('markdown-editor').disabled=true;showError(null);
  try{const data=await api('document',{path:selected,markdown:$('markdown-editor').value,revision});markdown=data.markdown;revision=data.revision;saving=false;editing=false;leaveEditor();renderMarkdown();$('save-status').textContent='已保存';try{await loadCollaboration();}catch(error){showError(error);}}
  catch(error){showError(error);$('save-status').textContent='草稿已保留';}
  finally{saving=false;$('save-button').disabled=false;$('cancel-button').disabled=false;$('markdown-editor').disabled=false;}
});
window.addEventListener('beforeunload',event=>{if(dirty()||saving){event.preventDefault();event.returnValue='';}});
