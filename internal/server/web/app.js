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
  return [...(project?.documents || []).map(d => ({...d, kind:'document'})), ...(project?.diagrams || []).map(d => ({...d, kind:'diagram'})), ...(project?.files || []).map(d => ({...d, kind:'file'}))];
}
function isEngineering(item) {
  const path=item.path;
  return item.kind!=='file' && (['AGENTS.md','CLAUDE.md','THIRD_PARTY.md','docs/project_access_plan.md'].includes(path)
    || path.startsWith('docs/standards/') || path.endsWith('/spec-template.md') || path.endsWith('/README.md')
    || /^docs\/reviews\/(stage-one|initialization|discussion)/.test(path));
}
const treeOf=item=>item.kind==='file'?'code':isEngineering(item)?'engineering':'documents';
// 目录展开状态只是浏览便利，按项目与所在列表保存在本机浏览器；存储不可用时退回默认展开文档列表的 docs。
const openFolders=new Set();
let folderStateKey='';
function loadFolderState(){
  const key='workbench-folders:'+(project?.root||'');if(key===folderStateKey)return;
  folderStateKey=key;openFolders.clear();
  try{for(const path of JSON.parse(localStorage.getItem(key)||'["documents:docs"]'))openFolders.add(String(path));}catch{openFolders.add('documents:docs');}
}
function saveFolderState(){try{localStorage.setItem(folderStateKey,JSON.stringify([...openFolders]));}catch{}}
function revealEntry(item){const tree=treeOf(item),parts=item.path.split('/');for(let i=1;i<parts.length;i++)openFolders.add(tree+':'+parts.slice(0,i).join('/'));saveFolderState();}
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
  const lists={documents:[],engineering:[],code:[]};for(const item of all)lists[treeOf(item)].push(item);
  $('document-count').textContent=String(lists.documents.length);$('engineering-count').textContent=String(lists.engineering.length);$('engineering-files').hidden=!lists.engineering.length;
  $('code-count').textContent=String(lists.code.length);$('code-files').hidden=!lists.code.length;
  const byName=(a,b)=>a.localeCompare(b,'zh-CN',{numeric:true});
  function drawNode(container,node,tree){
    for(const folder of [...node.folders.values()].sort((a,b)=>byName(a.name,b.name))){
      const details=document.createElement('details'),summary=document.createElement('summary'),name=document.createElement('span'),count=document.createElement('small'),children=document.createElement('div');
      details.className='tree-folder';details.dataset.folder=folder.path;details.open=!!query||openFolders.has(tree+':'+folder.path);
      name.textContent=folder.name;count.textContent=String(folder.count);summary.title=folder.path;summary.append(name,count);
      const key=tree+':'+folder.path;summary.addEventListener('click',()=>{if(details.open)openFolders.delete(key);else openFolders.add(key);saveFolderState();});
      children.className='tree-children';drawNode(children,folder,tree);details.append(summary,children);container.append(details);
    }
    for(const item of node.files.sort((a,b)=>byName(a.path,b.path))){
      const button=document.createElement('button');button.type='button';button.className='file-button';button.dataset.path=item.path;button.title=item.path;
      button.classList.toggle('active',item.path===selected);button.setAttribute('aria-current',item.path===selected?'page':'false');
      const name=document.createElement('strong');name.textContent=item.path.split('/').at(-1);button.append(name);
      if(item.kind==='diagram'){const detail=document.createElement('small');detail.textContent='交互式 HTML';button.append(detail);}
      button.addEventListener('click',()=>loadEntry(item));container.append(button);
    }
  }
  for(const [tree,container] of [['documents',$('document-list')],['engineering',$('engineering-list')],['code',$('code-list')]]){
    const items=lists[tree];container.replaceChildren();drawNode(container,buildTree(items),tree);
    if(!items.length){const empty=document.createElement('p');empty.className='empty-list';empty.textContent=query?'没有匹配的文件':'暂无文件';container.append(empty);}
  }
}
function dirty(){return editing&&$('markdown-editor').value!==markdown;}
function leaveEditor(){
  if(saving)return false;
  if(dirty()&&!window.confirm('有未保存的修改，是否放弃？'))return false;
  editing=false;$('editor-panel').hidden=true;$('save-button').hidden=true;$('cancel-button').hidden=true;$('edit-button').hidden=selectedKind!=='document';$('raw-button').hidden=selectedKind!=='document';return true;
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
  resetDiscussionSelection();const sequence=++readSequence;selected=item.path;selectedKind=item.kind;loadFolderState();revealEntry(item);markdown='';$('article').replaceChildren();$('raw-content').textContent='';$('raw-button').disabled=true;$('edit-button').disabled=true;editable=false;revision='';$('save-status').textContent='';renderList();renderThreads();if(isEngineering(item))$('engineering-files').open=true;if(item.kind==='file')$('code-files').open=true;showError(null);
  $('current-path').textContent=item.path;$('view-kind').textContent=item.kind==='diagram'?'项目交互图':item.kind==='file'?'项目文件 · 只读':'项目文档';$('loading').hidden=false;$('empty').hidden=true;
  $('article').hidden=true;$('raw-content').hidden=true;$('diagram-frame').hidden=true;$('diagram-frame').removeAttribute('srcdoc');$('raw-button').hidden=item.kind!=='document';$('edit-button').hidden=item.kind!=='document';$('diagram-help').hidden=item.kind!=='diagram';
  try {
    const data=await api(`${item.kind}?path=${encodeURIComponent(item.path)}`);
    if(sequence!==readSequence)return;
    if(item.kind==='diagram') {
      // sandbox 隔离父页面；独立策略禁止图连接网络和本地 API。
      const policy="default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: blob:; font-src data:; connect-src 'none'; frame-src 'none'; base-uri 'none'; form-action 'none'";
      $('diagram-frame').srcdoc=`<!doctype html><meta http-equiv="Content-Security-Policy" content="${policy}">`+data.html;
      $('diagram-frame').hidden=false;
    } else if(item.kind==='file') {$('raw-content').textContent=data.text;$('raw-content').hidden=false;
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

// 文件操作：新建、重命名与删除仅针对可写 Markdown；外部编辑器由本地启动参数决定，网页只提交项目内路径。
function updateFileActions(){
  const writable=selectedKind==='document'&&editable&&!editing&&!saving;
  $('rename-button').disabled=!writable;$('delete-button').disabled=!writable;$('open-external').disabled=!selected;
  $('rename-button').title=$('delete-button').title=writable?'':editing?'请先保存或取消编辑':'仅可写的 Markdown 文档支持此操作';
}
$('file-menu').addEventListener('toggle',updateFileActions);
document.addEventListener('pointerdown',event=>{if(!event.target.closest('#file-menu'))$('file-menu').open=false;});
async function reloadProjectAt(path){selected=path;await showProject(await api('project'));}
$('open-external').addEventListener('click',async()=>{
  $('file-menu').open=false;showError(null);
  try{await api('file/open',{path:selected});$('save-status').textContent='已交给外部编辑器；保存后点击刷新';}catch(error){showError(error);}
});
$('new-document').addEventListener('click',()=>{
  if(!leaveEditor())return;
  const folder=selected.includes('/')?selected.slice(0,selected.lastIndexOf('/')+1):'docs/';
  openDiscussionDialog('新建文档',`<label>文档路径<input id="new-document-path" value="${esc(folder.startsWith('docs/')?folder:'docs/')}" maxlength="400" required autocomplete="off"></label><p>须在项目根目录或 docs 下，扩展名为 .md；缺少的父目录会自动创建，已有文件不会被覆盖。不自动提交 Git。</p>`,async()=>{
    let path=$('new-document-path').value.trim();if(!path)throw new Error('请填写路径');if(!/\.md$/i.test(path))path+='.md';
    const created=await api('document/create',{path});await reloadProjectAt(created.path);$('save-status').textContent='已新建';
  },'新建');
  const input=$('new-document-path');input.focus();input.setSelectionRange(input.value.length,input.value.length);
});
$('rename-button').addEventListener('click',()=>{
  $('file-menu').open=false;if(selectedKind!=='document'||!editable||!leaveEditor())return;
  const from=selected,fromRevision=revision;
  openDiscussionDialog('重命名或移动文档',`<label>新路径<input id="rename-target" value="${esc(from)}" maxlength="400" required autocomplete="off"></label><p>文档上的讨论随文档迁移；目标已存在时不会覆盖。不自动提交 Git。</p>`,async()=>{
    const target=$('rename-target').value.trim();if(!target||target===from)throw new Error('请填写不同的新路径');
    await api('document/rename',{path:from,target,revision:fromRevision});await reloadProjectAt(target);$('save-status').textContent='已重命名';
  },'重命名');
  const input=$('rename-target');input.focus();input.setSelectionRange(input.value.lastIndexOf('/')+1,input.value.length-3);
});
$('delete-button').addEventListener('click',()=>{
  $('file-menu').open=false;if(selectedKind!=='document'||!editable||!leaveEditor())return;
  const path=selected,pathRevision=revision;
  openDiscussionDialog('删除文档',`<p>从项目目录删除 <strong>${esc(path)}</strong>？</p><p>文件不会进入废纸篓；如需恢复，请通过 Git 或备份。文档上的讨论保留，可在需要时手动删除。</p>`,async()=>{
    await api('document/delete',{path,revision:pathRevision});await reloadProjectAt('');$('save-status').textContent='已删除 '+path;
  },'删除');
});

// 侧栏收起只影响本机阅读布局，状态保存在浏览器；存储不可用时两栏默认展开。
const panelState={sidebar:true,discussion:true};
try{Object.assign(panelState,JSON.parse(localStorage.getItem('workbench-panels')||'{}'));}catch{}
function setPanel(name,visible){
  panelState[name]=!!visible;$('workspace').classList.toggle(name+'-collapsed',!panelState[name]);
  const button=$('toggle-'+name);button.setAttribute('aria-pressed',String(panelState[name]));button.title=(panelState[name]?'收起':'展开')+(name==='sidebar'?'文件栏':'批注与讨论');
  try{localStorage.setItem('workbench-panels',JSON.stringify(panelState));}catch{}
}
for(const name of ['sidebar','discussion']){setPanel(name,panelState[name]);$('toggle-'+name).addEventListener('click',()=>setPanel(name,!panelState[name]));}
for(const id of ['selection-tool','add-comment','add-memo'])$(id).addEventListener('click',()=>setPanel('discussion',true));
