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
  visibleDiscussionIDs=list.map(t=>t.id);updateBatchSelection();
  $('thread-list').innerHTML=list.length?list.map(t=>{
    const draft=replyDrafts.get(t.id),warning=locateAnchor(t.anchor).reason;
    const overdue=t.dueDate&&t.dueDate<new Date().toLocaleDateString('sv-SE')&&t.status==='open';
    return `<section class="thread ${t.status==='resolved'?'resolved':''}" data-thread="${esc(t.id)}"><div class="thread-top"><label class="thread-select"><input type="checkbox" data-select-thread="${esc(t.id)}" aria-label="选择这条讨论" ${selectedDiscussionIDs.has(t.id)?'checked':''} ${batchBusy?'disabled':''}><span>${t.kind==='memo'?'◷ 备忘':'◌ 批注'} · ${t.messages.length} 条消息</span></label><span>${t.status==='resolved'?'✓ 已解决':'待处理'}</span></div>${t.anchor?`<button class="thread-quote" data-locate="${esc(t.id)}">“${esc(t.anchor.quote)}”</button>`:''}${warning?`<p class="anchor-warning">${esc(warning)}</p>`:''}${t.dueDate?`<p class="due">${esc(t.dueDate)}${overdue?' · 已过期':''}</p>`:''}${t.messages.map(m=>`<div class="message"><div class="message-meta"><strong>${esc(m.author)}</strong><span>${m.role==='agent'?'AGENT':'用户'}</span>${m.intent!=='comment'?`<span class="${m.intent}">${m.intent==='rebuttal'?'Rebuttal · 反驳':'结论'}</span>`:''}</div><div class="message-body markdown-message" data-message-id="${esc(m.id)}"></div><time>${esc(new Date(m.createdAt).toLocaleString('zh-CN'))}</time></div>`).join('')}<div class="thread-agent-status" data-discussion-sessions="${esc(t.id)}"></div><div class="thread-bottom"><button data-reply="${esc(t.id)}">↳ 回复</button><button data-resolve="${esc(t.id)}">${t.status==='resolved'?'重新打开':'✓ 标记解决'}</button><button data-delete="${esc(t.id)}">删除</button><button data-forward="${esc(t.id)}">让 Agent 参与讨论</button></div><div class="reply-form" data-form="${esc(t.id)}" ${draft?'':'hidden'}><textarea aria-label="回复内容" data-draft="${esc(t.id)}" placeholder="补充意见、证据或修改建议…">${esc(draft?.body||'')}</textarea><div class="reply-actions"><select aria-label="回复类型"><option value="comment">普通回复</option><option value="rebuttal" ${draft?.intent==='rebuttal'?'selected':''}>Rebuttal · 反驳</option><option value="decision" ${draft?.intent==='decision'?'selected':''}>结论</option></select><button class="small-button save-button" data-send="${esc(t.id)}">发送回复</button></div></div></section>`;
  }).join(''):`<p class="empty-list">${selectedKind==='diagram'?'交互图在中间操作；选择文档可添加讨论。':selectedKind==='file'?'代码与其他文件只读显示，不支持批注；选择 Markdown 文档可添加讨论。':collaboration?'选中原文添加批注，或留下整篇意见与备忘。':'讨论尚未载入，请刷新重试。'}</p>`;
  for(const element of $('thread-list').querySelectorAll('[data-message-id]')){const message=all.flatMap(t=>t.messages).find(m=>m.id===element.dataset.messageId);if(message)renderProjectMarkdown(element,message.body,selected);}
  if(typeof renderAgentStatus==='function')renderAgentStatus();
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
$('thread-list').addEventListener('change',event=>{if(event.target.dataset.selectThread){const id=event.target.dataset.selectThread;if(event.target.checked)selectedDiscussionIDs.add(id);else selectedDiscussionIDs.delete(id);updateBatchSelection();}});
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

window.addEventListener('beforeunload',event=>{
  const compose=$('discussion-dialog').open&&$('compose-body')?.value.trim();
  if(discussionBusy||compose||Array.from(replyDrafts.values()).some(draft=>draft.body.trim())){event.preventDefault();event.returnValue='';}
});
