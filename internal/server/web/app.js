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
const escapeHTML = text => text.replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;');
marked.use({renderer: {html: item => escapeHTML(item.text)}});

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
  if (!response.ok) throw new Error(result.error || '读取失败');
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
function renderList() {
  const list = $('document-list');list.replaceChildren();
  const query = $('filter').value.trim().toLocaleLowerCase();
  const visible = entries().filter(d => d.path.toLocaleLowerCase().includes(query));
  $('document-count').textContent = `${visible.length}`;
  const groups = new Map();
  for (const item of visible) {
    const group = item.kind === 'diagram' ? '交互图' : item.path.includes('/') ? item.path.slice(0, item.path.lastIndexOf('/')) : '项目根目录';
    if (!groups.has(group)) groups.set(group, []);
    groups.get(group).push(item);
  }
  for (const [group, items] of groups) {
    const title = document.createElement('div');title.className = 'file-group';title.textContent = group;list.append(title);
    for (const item of items) {
      const button = document.createElement('button');button.type='button';button.className='file-button';button.dataset.path=item.path;
      button.classList.toggle('active', item.path===selected);button.setAttribute('aria-current', item.path===selected ? 'page' : 'false');
      const name=document.createElement('strong');name.textContent=item.path.split('/').at(-1);
      const detail=document.createElement('small');detail.textContent=item.kind==='diagram'?'交互式 HTML':item.path;
      button.append(name,detail);button.addEventListener('click',()=>loadEntry(item));list.append(button);
    }
  }
  if (!visible.length) {const empty=document.createElement('p');empty.className='empty-list';empty.textContent=query?'没有匹配的文件':'暂无文件';list.append(empty);}
}
async function showProject(info) {
  project=info;$('welcome').hidden=!!info;$('workspace').hidden=!info;
  if (!info) return;
  $('project-name').textContent=info.name;$('project-root').textContent=info.root;
  document.title=`${info.name} · Agent Workbench`;
  $('warning-list').replaceChildren();
  for(const warning of info.warnings){const li=document.createElement('li');li.textContent=warning;$('warning-list').append(li);}
  $('warning-summary').textContent=`${info.warnings.length} 项需要补充 · 点击查看`;$('warnings').hidden=!info.warnings.length;
  renderList();
  const all=entries();const target=all.find(d=>d.path===selected)||all.find(d=>d.path==='PLAN.md')||all.find(d=>d.path==='README.md')||all[0];
  $('empty').hidden=!!target;
  if(target) await loadEntry(target);else {selected='';$('article').replaceChildren();$('raw-content').hidden=true;$('diagram-frame').hidden=true;$('diagram-frame').removeAttribute('srcdoc');$('current-path').textContent='暂无文档';$('raw-button').hidden=true;}
}
function renderMarkdown() {
  $('raw-button').setAttribute('aria-pressed',String(raw));
  $('raw-content').hidden=!raw;$('article').hidden=raw;$('raw-content').textContent=markdown;
  $('article').innerHTML=DOMPurify.sanitize(marked.parse(markdown,{gfm:true}),{
    ALLOWED_TAGS:['p','h1','h2','h3','h4','h5','h6','blockquote','ul','ol','li','pre','code','strong','em','del','a','hr','br','table','thead','tbody','tr','th','td','sup','sub','details','summary'],
    ALLOWED_ATTR:['href','title','start','align'],ALLOW_DATA_ATTR:false
  });
  for(const heading of $('article').querySelectorAll('h1,h2,h3,h4,h5,h6')) heading.id=heading.textContent.trim().toLowerCase().replace(/\s+/g,'-').replace(/[^\p{L}\p{N}_-]/gu,'');
  for(const link of $('article').querySelectorAll('a[href]')) {
    const href=link.getAttribute('href');
    if(/^https?:\/\//i.test(href)){link.target='_blank';link.rel='noopener noreferrer';continue;}
    if(href.startsWith('#')){link.addEventListener('click',event=>{event.preventDefault();document.getElementById(decodeURIComponent(href.slice(1)))?.scrollIntoView();});continue;}
    try {
      const destination=new URL(href, 'https://project.invalid/'+selected);
      const path=decodeURIComponent(destination.pathname.slice(1));
      const target=destination.origin==='https://project.invalid'&&entries().find(d=>d.path===path);
      if(target){link.addEventListener('click',event=>{event.preventDefault();loadEntry(target);});}
      else {link.removeAttribute('href');link.title='此链接不在当前阅读范围内';}
    } catch {link.removeAttribute('href');}
  }
}
async function loadEntry(item) {
  const sequence=++readSequence;selected=item.path;selectedKind=item.kind;markdown='';$('article').replaceChildren();$('raw-content').textContent='';$('raw-button').disabled=true;renderList();showError(null);
  $('current-path').textContent=item.path;$('view-kind').textContent=item.kind==='diagram'?'项目交互图':'项目文档';$('loading').hidden=false;$('empty').hidden=true;
  $('article').hidden=true;$('raw-content').hidden=true;$('diagram-frame').hidden=true;$('diagram-frame').removeAttribute('srcdoc');$('raw-button').hidden=item.kind==='diagram';
  try {
    const data=await api(`${item.kind}?path=${encodeURIComponent(item.path)}`);
    if(sequence!==readSequence)return;
    if(item.kind==='diagram') {
      // sandbox 隔离父页面；独立策略禁止图连接网络和本地 API。
      const policy="default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: blob:; font-src data:; connect-src 'none'; frame-src 'none'; base-uri 'none'; form-action 'none'";
      $('diagram-frame').srcdoc=`<!doctype html><meta http-equiv="Content-Security-Policy" content="${policy}">`+data.html;
      $('diagram-frame').hidden=false;
    } else {markdown=data.markdown;$('raw-button').disabled=false;renderMarkdown();}
  } catch(error) {if(sequence===readSequence)showError(error);}
  finally {if(sequence===readSequence)$('loading').hidden=true;}
}
$('filter').addEventListener('input',renderList);
$('raw-button').addEventListener('click',()=>{if(selectedKind==='document'&&!$('raw-button').disabled){raw=!raw;renderMarkdown();}});
$('refresh-button').addEventListener('click',async()=>{
  $('refresh-button').disabled=true;showError(null);
  try{await showProject(await api('project'));}catch(error){showError(error);}finally{$('refresh-button').disabled=false;}
});
(async()=>{try{if(!token)throw new Error('请使用终端显示的完整地址打开工作台。');await showProject(await api('project'));}catch(error){$('welcome').hidden=false;showError(error);}})();
