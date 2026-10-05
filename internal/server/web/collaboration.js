'use strict';
let collaboration = null;
let threadFilter = 'all';
let pendingAnchor = null;
let discussionBusy = false;
let dialogAction = null;
const replyDrafts = new Map();
const esc = value => escapeHTML(String(value)).replaceAll('"','&quot;').replaceAll("'",'&#39;');
const clone = value => structuredClone(value);
const uid = () => crypto.randomUUID();
const currentDocument = () => collaboration?.documents.find(d => d.id === selected);
const identity = () => ({author:$('author-name').value.trim()||'我',role:$('author-role').value});
const makeMessage = (body,intent='comment') => ({id:uid(),...identity(),body:body.trim(),intent,createdAt:new Date().toISOString()});

async function loadCollaboration() {
  try{collaboration = await api('collaboration');renderThreads();}
  catch(error){$('discussion-status').textContent='讨论读取失败：'+error.message;throw error;}
}
function resetDiscussionSelection() {
  pendingAnchor=null;$('selection-tool').hidden=true;
}
function locateAnchor(anchor) {
  if(!anchor)return {element:null,reason:''};
  if(selectedKind!=='document'||!markdown)return {element:null,reason:'当前未载入正文'};
  const matches=Array.from($('article').children).filter(el=>el.textContent.includes(anchor.quote));
  if(matches.length===1)return {element:matches[0],reason:''};
  if(matches.length>1){const contextual=matches.filter(el=>el.textContent.includes(anchor.before+anchor.quote+anchor.after));if(contextual.length===1)return {element:contextual[0],reason:''};return {element:null,reason:'原文有多处匹配，请结合引用确认位置'};}
  return {element:null,reason:'原文已变更 · 引用与讨论已保留'};
}
function renderThreads() {
  const ready=!!collaboration&&selectedKind==='document'&&!!currentDocument();
  $('add-comment').disabled=!ready||discussionBusy;$('add-memo').disabled=!ready||discussionBusy;
  const all=collaboration?.threads.filter(t=>t.documentId===selected)||[];
  $('thread-total').textContent=String(all.length);
  $('pending-count').textContent=`${all.filter(t=>t.status==='open').length} 条待处理`;
  const status=$('status-filter').value;
  const list=all.filter(t=>(threadFilter==='all'||t.kind===threadFilter)&&(status==='all'||t.status===status));
  $('thread-list').innerHTML=list.length?list.map(t=>{
    const draft=replyDrafts.get(t.id),warning=locateAnchor(t.anchor).reason;
    const overdue=t.dueDate&&t.dueDate<new Date().toLocaleDateString('sv-SE')&&t.status==='open';
    return `<section class="thread ${t.status==='resolved'?'resolved':''}" data-thread="${esc(t.id)}"><div class="thread-top"><span>${t.kind==='memo'?'◷ 备忘':'◌ 批注'} · ${t.messages.length} 条消息</span><span>${t.status==='resolved'?'✓ 已解决':'待处理'}</span></div>${t.anchor?`<button class="thread-quote" data-locate="${esc(t.id)}">“${esc(t.anchor.quote)}”</button>`:''}${warning?`<p class="anchor-warning">${esc(warning)}</p>`:''}${t.dueDate?`<p class="due">${esc(t.dueDate)}${overdue?' · 已过期':''}</p>`:''}${t.messages.map(m=>`<div class="message"><div class="message-meta"><strong>${esc(m.author)}</strong><span>${m.role==='agent'?'AGENT':'用户'}</span>${m.intent!=='comment'?`<span class="${m.intent}">${m.intent==='rebuttal'?'Rebuttal · 反驳':'结论'}</span>`:''}</div><p class="message-body">${esc(m.body)}</p><time>${esc(new Date(m.createdAt).toLocaleString('zh-CN'))}</time></div>`).join('')}<div class="thread-bottom"><button data-reply="${esc(t.id)}">↳ 回复</button><button data-resolve="${esc(t.id)}">${t.status==='resolved'?'重新打开':'✓ 标记解决'}</button><button data-delete="${esc(t.id)}">删除</button><button data-forward="${esc(t.id)}">转发给 Agent</button></div><div class="reply-form" data-form="${esc(t.id)}" ${draft?'':'hidden'}><textarea aria-label="回复内容" data-draft="${esc(t.id)}" placeholder="补充意见、证据或修改建议…">${esc(draft?.body||'')}</textarea><div class="reply-actions"><select aria-label="回复类型"><option value="comment">普通回复</option><option value="rebuttal" ${draft?.intent==='rebuttal'?'selected':''}>Rebuttal · 反驳</option><option value="decision" ${draft?.intent==='decision'?'selected':''}>结论</option></select><button class="small-button save-button" data-send="${esc(t.id)}">发送回复</button></div></div></section>`;
  }).join(''):`<p class="empty-list">${selectedKind==='diagram'?'交互图在中间操作；选择文档可添加讨论。':collaboration?'选中原文添加批注，或留下整篇意见与备忘。':'讨论尚未载入，请刷新重试。'}</p>`;
  if(discussionBusy)for(const control of $('thread-list').querySelectorAll('button'))control.disabled=true;
}
async function persistThreads(next) {
  if(discussionBusy)throw new Error('正在保存讨论，请稍候');
  if(!collaboration)throw new Error('请先载入讨论');
  discussionBusy=true;renderThreads();$('discussion-status').textContent='正在保存到项目…';
  try {
    collaboration=await api('collaboration',{generation:collaboration.generation,threads:next});
    $('discussion-status').textContent='已保存到项目 · 刷新或重启后可继续';
  }catch(error){
    if(error.status===409){try{collaboration=await api('collaboration');error.message='讨论有新变化，已载入最新内容。当前输入已保留，请核对后再次保存。';}catch{}}
    $('discussion-status').textContent='保存失败，输入已保留';throw error;
  }
  finally{discussionBusy=false;renderThreads();}
}
function openDiscussionDialog(title,body,action,submit='保存') {
  $('discussion-dialog-title').textContent=title;$('discussion-dialog-body').innerHTML=body;
  $('discussion-dialog-submit').textContent=submit;$('discussion-dialog-error').hidden=true;
  dialogAction=action;$('discussion-dialog').showModal();
}
$('discussion-dialog-cancel').onclick=()=>{if(!discussionBusy)$('discussion-dialog').close();};
$('discussion-dialog').addEventListener('cancel',event=>{if(discussionBusy)event.preventDefault();});
$('discussion-form').addEventListener('submit',async event=>{
  event.preventDefault();if(discussionBusy||!dialogAction)return;
  $('discussion-dialog-submit').disabled=true;$('discussion-dialog-cancel').disabled=true;
  try{await dialogAction();$('discussion-dialog').close();resetDiscussionSelection();}
  catch(error){$('discussion-dialog-error').textContent=error.message;$('discussion-dialog-error').hidden=false;}
  finally{$('discussion-dialog-submit').disabled=false;$('discussion-dialog-cancel').disabled=false;}
});
function composeThread(kind,anchor=null) {
  const documentId=selected;
  if(!currentDocument()||selectedKind!=='document')return;
  const quote=anchor?clone(anchor):null;
  $('selection-tool').hidden=true;
  openDiscussionDialog(kind==='memo'?'添加备忘':'添加批注',`${quote?`<blockquote>${esc(quote.quote)}</blockquote>`:`<p>关联文档：${esc(documentId)}</p>`}<label>内容<textarea id="compose-body" required maxlength="20000" placeholder="写下问题、建议或下一步…"></textarea></label><label>消息意图<select id="compose-intent"><option value="comment">普通意见</option><option value="rebuttal">Rebuttal · 反驳</option><option value="decision">结论</option></select></label>${kind==='memo'?'<label>备忘日期（可选）<input type="date" id="compose-date"></label><p>日期只在页面提示，不推送通知。</p>':''}`,async()=>{
    const body=$('compose-body').value.trim();if(!body)throw new Error('请填写内容');
    const next=clone(collaboration.threads);
    next.push({id:uid(),documentId,kind,status:'open',dueDate:kind==='memo'?$('compose-date').value:'',anchor:quote,messages:[makeMessage(body,$('compose-intent').value)]});
    await persistThreads(next);
    threadFilter='all';$('status-filter').value='all';updateThreadFilters();renderThreads();
    window.getSelection()?.removeAllRanges();
  });$('compose-body').focus();
}
$('add-comment').onclick=()=>composeThread('comment');$('add-memo').onclick=()=>composeThread('memo');
function captureSelection() {
  if(editing||raw||selectedKind!=='document'||$('discussion-dialog').open)return resetDiscussionSelection();
  const selection=window.getSelection();
  if(!selection||selection.isCollapsed||!selection.rangeCount)return resetDiscussionSelection();
  const range=selection.getRangeAt(0);
  if(!$('article').contains(range.startContainer)||!$('article').contains(range.endContainer))return resetDiscussionSelection();
  function block(node){let el=node.nodeType===1?node:node.parentElement;while(el&&el.parentElement!==$('article'))el=el.parentElement;return el;}
  const start=block(range.startContainer),end=block(range.endContainer);
  const quote=selection.toString();
  if(start!==end||!quote.trim()||quote.length>20000)return resetDiscussionSelection();
  // Range 定位实际选中位置，避免重复文字取到第一个匹配。
  const prefix=document.createRange();prefix.selectNodeContents(start);prefix.setEnd(range.startContainer,range.startOffset);
  const index=prefix.toString().length,text=start.textContent;
  pendingAnchor={quote,before:text.slice(Math.max(0,index-48),index),after:text.slice(index+quote.length,index+quote.length+48),revision:currentDocument()?.revision||1};
  const rect=range.getBoundingClientRect();$('selection-tool').style.left=Math.min(Math.max(12,rect.left),window.innerWidth-170)+'px';$('selection-tool').style.top=Math.max(8,rect.top-42)+'px';$('selection-tool').hidden=false;
}
$('article').addEventListener('mouseup',()=>setTimeout(captureSelection,10));$('article').addEventListener('keyup',captureSelection);
document.querySelector('.reader').addEventListener('scroll',()=>$('selection-tool').hidden=true);
$('selection-tool').onpointerdown=event=>event.preventDefault();$('selection-tool').onclick=()=>composeThread('comment',pendingAnchor);
document.addEventListener('pointerdown',event=>{if(!event.target.closest('#article')&&!event.target.closest('#selection-tool'))$('selection-tool').hidden=true;});
function updateThreadFilters(){for(const button of document.querySelectorAll('[data-thread-filter]'))button.setAttribute('aria-pressed',String(button.dataset.threadFilter===threadFilter));}
for(const button of document.querySelectorAll('[data-thread-filter]'))button.onclick=()=>{threadFilter=button.dataset.threadFilter;updateThreadFilters();renderThreads();};
$('status-filter').onchange=renderThreads;
$('thread-list').addEventListener('input',event=>{
  const form=event.target.closest('[data-form]');if(form)replyDrafts.set(form.dataset.form,{body:form.querySelector('textarea').value,intent:form.querySelector('select').value});
});
$('thread-list').addEventListener('click',async event=>{
  const button=event.target.closest('button');if(!button||discussionBusy||!collaboration)return;
  const id=button.dataset.reply||button.dataset.resolve||button.dataset.send||button.dataset.delete||button.dataset.locate||button.dataset.forward;
  const thread=collaboration.threads.find(t=>t.id===id);if(!thread)return;
  if(button.dataset.forward){forwardThreadToAgent(id);return;}
  if(button.dataset.locate){
    if(editing){$('discussion-status').textContent='请保存或取消原文编辑后定位';return;}
    if(raw){raw=false;renderMarkdown();}
    $('article').querySelectorAll('.anchored').forEach(el=>el.classList.remove('anchored'));
    const found=locateAnchor(thread.anchor);if(found.element){found.element.classList.add('anchored');found.element.scrollIntoView({behavior:'smooth',block:'center'});}else $('discussion-status').textContent=found.reason;return;
  }
  if(button.dataset.reply){replyDrafts.set(id,replyDrafts.get(id)||{body:'',intent:'comment'});renderThreads();Array.from($('thread-list').querySelectorAll('[data-form]')).find(el=>el.dataset.form===id)?.querySelector('textarea').focus();return;}
  try {
    const next=clone(collaboration.threads),target=next.find(t=>t.id===id);
    if(button.dataset.resolve)target.status=target.status==='open'?'resolved':'open';
    if(button.dataset.delete){if(!confirm('删除这条讨论及全部回复？'))return;next.splice(next.indexOf(target),1);}
    if(button.dataset.send){const form=button.closest('[data-form]'),body=form.querySelector('textarea').value.trim();if(!body)throw new Error('请填写回复');replyDrafts.set(id,{body,intent:form.querySelector('select').value});target.messages.push(makeMessage(body,form.querySelector('select').value));}
    await persistThreads(next);
    if(button.dataset.send||button.dataset.delete){replyDrafts.delete(id);renderThreads();}
    showError(null);
  }catch(error){showError(error);}
});

function downloadJSON(value){const url=URL.createObjectURL(new Blob([JSON.stringify(value,null,2)],{type:'application/json'}));const link=document.createElement('a');link.href=url;link.download='agent-workbench-handoff.json';document.body.append(link);link.click();link.remove();setTimeout(()=>URL.revokeObjectURL(url),10000);}
$('export-discussion').onclick=async()=>{
  if(dirty()){showError(new Error('请先保存或取消原文编辑，再导出项目的最新正文'));return;}
  $('export-discussion').disabled=true;
  try{const bundle=await api('collaboration?export=1');bundle.exportedAt=new Date().toISOString();downloadJSON(bundle);$('discussion-status').textContent='已导出正文、引用、批注与完整讨论';}
  catch(error){showError(error);}finally{$('export-discussion').disabled=false;}
};
function validateMessage(m){if(!m||typeof m.id!=='string'||!m.id.trim()||typeof m.author!=='string'||!m.author.trim()||!['human','agent'].includes(m.role)||!['comment','rebuttal','decision'].includes(m.intent)||typeof m.body!=='string'||!m.body.trim()||!Number.isFinite(Date.parse(m.createdAt)))throw new Error('消息格式无效');return {id:m.id,author:m.author,role:m.role,intent:m.intent,body:m.body,createdAt:m.createdAt};}
function normalizeThread(t){if(!t||typeof t.id!=='string'||!t.id.trim()||!collaboration.documents.some(d=>d.id===t.documentId)||!['comment','memo'].includes(t.kind)||!['open','resolved'].includes(t.status)||!Array.isArray(t.messages)||!t.messages.length)throw new Error('讨论格式或文档引用无效');const anchor=t.anchor==null?null:{quote:t.anchor.quote,before:t.anchor.before||'',after:t.anchor.after||'',revision:t.anchor.revision};return {id:t.id,documentId:t.documentId,kind:t.kind,status:t.status,dueDate:t.dueDate||'',anchor,messages:t.messages.map(validateMessage)};}
function prepareImport(data){
  if(!collaboration)throw new Error('请先载入项目讨论');
  const full=data?.format==='agent-docs-workspace';
  if(!data||data.version!==1||(!full&&data.format!=='agent-docs-feedback')||(full?data.id:data.workspaceId)!==collaboration.id)throw new Error('交接文件不属于当前工作区，或格式版本不匹配');
  for(const key of ['threads','replies','changes'])if(data[key]!==undefined&&!Array.isArray(data[key]))throw new Error(key+' 必须是数组');
  if(full&&!Array.isArray(data.threads))throw new Error('交接包缺少讨论');
  const next=clone(collaboration.threads),threadMap=new Map(next.map(t=>[t.id,t]));
  const messages=new Map(next.flatMap(t=>t.messages.map(m=>[m.id,{message:m,threadId:t.id}])));
  let added=0,skipped=0,proposals=0;
  function mergeMessage(m,target){const old=messages.get(m.id);if(old){if(old.threadId!==target.id||JSON.stringify(old.message)!==JSON.stringify(m))throw new Error('消息 ID 已存在且内容不同');skipped++;return;}target.messages.push(m);messages.set(m.id,{message:m,threadId:target.id});added++;}
  for(const rawThread of data.threads||[]){
    const incoming=normalizeThread(rawThread),old=threadMap.get(incoming.id);
    if(old){if(old.documentId!==incoming.documentId||old.kind!==incoming.kind||JSON.stringify(old.anchor)!==JSON.stringify(incoming.anchor))throw new Error('讨论 ID 已存在且关联不同');for(const m of incoming.messages)mergeMessage(m,old);}
    else {const target={...incoming,messages:[]};for(const m of incoming.messages)mergeMessage(m,target);next.push(target);threadMap.set(target.id,target);}
  }
  for(const reply of data.replies||[]){const target=threadMap.get(reply.threadId);if(!target)throw new Error('回复的 threadId 不存在');mergeMessage(validateMessage(reply),target);}
  // 正文建议先成为可讨论的证据，避免导入 JSON 隐式覆盖磁盘文件。
  for(const change of data.changes||[]){
    if(!change||typeof change.id!=='string'||!change.id.trim()||!collaboration.documents.some(d=>d.id===change.documentId)||typeof change.markdown!=='string'||!Number.isSafeInteger(change.baseRevision)||change.baseRevision<1||typeof change.author!=='string'||!change.author.trim()||change.summary!==undefined&&typeof change.summary!=='string')throw new Error('正文修改建议格式无效');
    const threadId='proposal-'+change.id,body=`【正文修改建议 · 尚未写入文件】\n基于 rev. ${change.baseRevision}，请核对当前原文后通过编辑器采纳。\n${change.summary||''}\n\n建议正文：\n${change.markdown}`;
    const prior=threadMap.get(threadId);
    if(prior){if(prior.documentId!==change.documentId||prior.messages[0].body!==body||prior.messages[0].author!==change.author)throw new Error('修改建议 ID 已存在且内容不同');skipped++;continue;}
    const target={id:threadId,documentId:change.documentId,kind:'comment',status:'open',dueDate:'',anchor:null,messages:[{id:'proposal-message-'+change.id,author:change.author,role:'agent',intent:'rebuttal',body,createdAt:new Date().toISOString()}]};
    if(messages.has(target.messages[0].id))throw new Error('修改建议消息 ID 重复');
    next.push(target);threadMap.set(threadId,target);messages.set(target.messages[0].id,{message:target.messages[0],threadId});proposals++;
  }
  return {next,added,skipped,proposals,generation:collaboration.generation};
}
$('import-discussion').onclick=()=>$('discussion-file').click();
$('discussion-file').onchange=async event=>{
  const file=event.target.files[0];event.target.value='';if(!file)return;
  try {
    if(file.size>16*1024*1024)throw new Error('JSON 文件超过 16 MiB');
    const preview=prepareImport(JSON.parse(await file.text()));
    openDiscussionDialog('载入讨论与回复',`<p>新增 ${preview.added} 条消息，跳过 ${preview.skipped} 项重复内容，保留 ${preview.proposals} 项正文修改建议。</p><p>讨论将合并到当前项目。正文建议留在讨论中，由你核对后通过“编辑原文”采纳；完整交接包也不会替换项目文件。</p>`,async()=>{
      if(collaboration.generation!==preview.generation)throw new Error('预览后讨论已变化，请重新载入文件');
      await persistThreads(preview.next);
    },'确认载入');
  }catch(error){showError(error);}
};

window.addEventListener('beforeunload',event=>{
  const compose=$('discussion-dialog').open&&$('compose-body')?.value.trim();
  if(discussionBusy||compose||Array.from(replyDrafts.values()).some(draft=>draft.body.trim())){event.preventDefault();event.returnValue='';}
});
