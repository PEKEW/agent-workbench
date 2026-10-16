'use strict';
let collaboration = null;
let threadFilter = 'all';
let pendingAnchor = null;
let pendingBlock = null;
let discussionBusy = false;
let dialogAction = null;
// 正文内展开的讨论：同一时刻只展开一条，批注栏中对应条目改为简要显示。
let openThreadID = '';
// 编辑框状态：引用批注放在所选段落下方，整篇意见与备忘放在批注栏顶部；切换文档时保留内容。
let composer = null;
const replyDrafts = new Map();
const esc = value => escapeHTML(String(value)).replaceAll('"','&quot;').replaceAll("'",'&#39;');
const clone = value => structuredClone(value);
const uid = () => crypto.randomUUID();
const currentDocument = () => collaboration?.documents.find(d => d.id === selected);
const identity = () => ({author:$('author-name').value.trim()||'我',role:$('author-role').value});
const makeMessage = (body,intent='comment') => ({id:uid(),...identity(),body:body.trim(),intent,createdAt:new Date().toISOString()});
const articleBlocks = () => Array.from($('article').children).filter(el=>!el.hasAttribute('data-inline'));

async function loadCollaboration() {
  try{collaboration = await api('collaboration');renderThreads();}
  catch(error){$('discussion-status').textContent='讨论读取失败：'+error.message;throw error;}
}
function resetDiscussionSelection() {
  pendingAnchor=null;pendingBlock=null;$('selection-tool').hidden=true;
}
function locateAnchor(anchor) {
  if(!anchor)return {element:null,reason:''};
  if(selectedKind!=='document'||!markdown)return {element:null,reason:'当前未载入正文'};
  const matches=articleBlocks().filter(el=>el.textContent.includes(anchor.quote));
  if(matches.length===1)return {element:matches[0],reason:''};
  if(matches.length>1){const contextual=matches.filter(el=>el.textContent.includes(anchor.before+anchor.quote+anchor.after));if(contextual.length===1)return {element:contextual[0],reason:''};return {element:null,reason:'原文有多处匹配，请结合引用确认位置'};}
  return {element:null,reason:'原文已变更 · 引用与讨论已保留'};
}
const inlineReady = () => selectedKind==='document'&&!!markdown&&!raw&&!!collaboration;
function threadCardHTML(t,place) {
  const inline=place==='inline',draft=replyDrafts.get(t.id),warning=inline?'':locateAnchor(t.anchor).reason;
  const overdue=t.dueDate&&t.dueDate<new Date().toLocaleDateString('sv-SE')&&t.status==='open';
  const label=`${t.kind==='memo'?'◷ 备忘':'◌ 批注'} · ${t.messages.length} 条消息`;
  const top=`<div class="thread-top">${inline?`<span>${label}</span>`:`<label class="thread-select"><input type="checkbox" data-select-thread="${esc(t.id)}" aria-label="选择这条讨论" ${selectedDiscussionIDs.has(t.id)?'checked':''} ${batchBusy?'disabled':''}><span>${label}</span></label>`}<span>${t.status==='resolved'?'✓ 已解决':'待处理'}</span></div>`;
  const status=`<div class="thread-agent-status" data-discussion-sessions="${esc(t.id)}"></div>`;
  if(!inline&&t.id===openThreadID)return `<section class="thread compact ${t.status==='resolved'?'resolved':''}" data-thread="${esc(t.id)}">${top}<button class="thread-quote" data-locate="${esc(t.id)}">“${esc(t.anchor.quote)}”</button><p class="inline-note">已在正文中展开 <button type="button" data-collapse="${esc(t.id)}">收起</button></p>${status}</section>`;
  const quote=!t.anchor?'':inline?`<blockquote class="thread-quote-text">${esc(t.anchor.quote)}</blockquote>`:`<button class="thread-quote" data-locate="${esc(t.id)}">“${esc(t.anchor.quote)}”</button>`;
  return `<section class="thread ${inline?'inline-card':''} ${t.status==='resolved'?'resolved':''}" data-thread="${esc(t.id)}">${top}${quote}${warning?`<p class="anchor-warning">${esc(warning)}</p>`:''}${t.dueDate?`<p class="due">${esc(t.dueDate)}${overdue?' · 已过期':''}</p>`:''}${t.messages.map(m=>`<div class="message"><div class="message-meta"><strong>${esc(m.author)}</strong><span>${m.role==='agent'?'AGENT':'用户'}</span>${m.intent!=='comment'?`<span class="${m.intent}">${m.intent==='rebuttal'?'Rebuttal · 反驳':'结论'}</span>`:''}</div><div class="message-body markdown-message" data-message-id="${esc(m.id)}"></div><time>${esc(new Date(m.createdAt).toLocaleString('zh-CN'))}</time></div>`).join('')}${status}<div class="thread-bottom"><button data-reply="${esc(t.id)}">↳ 回复</button><button data-ask="${esc(t.id)}">请 Agent 回复</button><button data-resolve="${esc(t.id)}">${t.status==='resolved'?'重新打开':'✓ 标记解决'}</button><button data-delete="${esc(t.id)}">删除</button><button data-forward="${esc(t.id)}">在 Agent 页处理</button>${inline?`<button data-collapse="${esc(t.id)}">收起</button>`:''}</div><div class="reply-form" data-form="${esc(t.id)}" ${draft?'':'hidden'}><textarea aria-label="回复内容" data-draft="${esc(t.id)}" placeholder="补充意见、证据或修改建议…">${esc(draft?.body||'')}</textarea><div class="reply-actions"><select aria-label="回复类型"><option value="comment">普通回复</option><option value="rebuttal" ${draft?.intent==='rebuttal'?'selected':''}>Rebuttal · 反驳</option><option value="decision" ${draft?.intent==='decision'?'selected':''}>结论</option></select><span class="reply-buttons"><button class="small-button" data-send="${esc(t.id)}">发送回复</button><button class="small-button save-button" data-send-ask="${esc(t.id)}">保存并请 Agent 回复</button></span></div></div></section>`;
}
function renderMessageBodies(container,threads) {
  for(const element of container.querySelectorAll('[data-message-id]')){const message=threads.flatMap(t=>t.messages).find(m=>m.id===element.dataset.messageId);if(message)renderProjectMarkdown(element,message.body,selected);}
}
function renderThreads() {
  const ready=!!collaboration&&selectedKind==='document'&&!!currentDocument();
  $('add-comment').disabled=!ready||discussionBusy;$('add-memo').disabled=!ready||discussionBusy;
  const all=collaboration?.threads.filter(t=>t.documentId===selected)||[];
  if(openThreadID&&!(inlineReady()&&all.some(t=>t.id===openThreadID&&locateAnchor(t.anchor).element)))openThreadID='';
  // 重新渲染会替换回复框；记录焦点与光标，渲染后恢复，避免轮询打断输入。
  const active=document.activeElement,focusDraft=active?.dataset?.draft,caret=focusDraft?[active.selectionStart,active.selectionEnd]:null;
  $('thread-total').textContent=String(all.length);
  $('pending-count').textContent=`${all.filter(t=>t.status==='open').length} 条待处理`;
  const status=$('status-filter').value;
  const list=all.filter(t=>(threadFilter==='all'||t.kind===threadFilter)&&(status==='all'||t.status===status));
  visibleDiscussionIDs=list.map(t=>t.id);updateBatchSelection();
  $('thread-list').innerHTML=list.length?list.map(t=>threadCardHTML(t,'panel')).join(''):`<p class="empty-list">${selectedKind==='diagram'?'交互图在中间操作；选择文档可添加讨论。':selectedKind==='file'?'代码与其他文件只读显示，不支持批注；选择 Markdown 文档可添加讨论。':collaboration?'选中原文添加批注，或留下整篇意见与备忘。':'讨论尚未载入，请刷新重试。'}</p>`;
  renderMessageBodies($('thread-list'),all);
  renderInlineDiscussion();
  if(focusDraft){const target=document.querySelector(`[data-draft="${CSS.escape(focusDraft)}"]`);if(target){target.focus();target.setSelectionRange(...caret);}}
  if(typeof renderAgentStatus==='function')renderAgentStatus();
  if(discussionBusy)for(const control of document.querySelectorAll('#thread-list button,#article [data-inline] button'))control.disabled=true;
}
// 正文内显示：带引用批注的段落下方列出批注标记，展开的讨论显示在标记下方。插入的元素带 data-inline，不参与引用定位与选区计算。
function renderInlineDiscussion() {
  for(const el of $('article').querySelectorAll('[data-inline]:not(#inline-composer)'))el.remove();
  for(const el of $('article').querySelectorAll('.has-thread'))el.classList.remove('has-thread');
  if(inlineReady()){
    const groups=new Map();
    for(const t of collaboration.threads.filter(t=>t.documentId===selected&&t.anchor)){const {element}=locateAnchor(t.anchor);if(!element)continue;if(!groups.has(element))groups.set(element,[]);groups.get(element).push(t);}
    for(const [element,threads] of groups){
      element.classList.add('has-thread');
      const bar=document.createElement('div');bar.className='thread-markers';bar.dataset.inline='';
      for(const t of threads){const chip=document.createElement('button');chip.type='button';chip.className='thread-marker';chip.dataset.openThread=t.id;chip.classList.toggle('resolved',t.status==='resolved');chip.setAttribute('aria-expanded',String(t.id===openThreadID));const quote=t.anchor.quote.trim();chip.textContent=`◌ ${quote.length>18?quote.slice(0,18)+'…':quote} · ${t.messages.length}`;chip.title='在此展开讨论';bar.append(chip);}
      element.after(bar);
      const open=threads.find(t=>t.id===openThreadID);
      if(open){const box=document.createElement('div');box.className='inline-thread';box.dataset.inline='';box.innerHTML=threadCardHTML(open,'inline');renderMessageBodies(box,[open]);bar.after(box);}
    }
  }
  placeComposer();
}
function openThreadInline(id) {
  const thread=collaboration?.threads.find(t=>t.id===id);if(!thread)return;
  if(editing){$('discussion-status').textContent='请保存或取消原文编辑后定位';return;}
  if(raw){raw=false;renderMarkdown();}
  for(const el of $('article').querySelectorAll('.anchored'))el.classList.remove('anchored');
  const found=locateAnchor(thread.anchor);
  if(!found.element){$('discussion-status').textContent=found.reason;return;}
  openThreadID=id;found.element.classList.add('anchored');renderThreads();
  $('article').querySelector('.inline-thread')?.scrollIntoView({behavior:'smooth',block:'nearest'});
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

// 编辑框为常驻表单，移动位置时保留输入内容。
const composerForm=document.createElement('form');
composerForm.id='inline-composer';composerForm.className='inline-composer';composerForm.dataset.inline='';
const cf=id=>composerForm.querySelector('#'+id);
composerForm.innerHTML='<p id="compose-title" class="compose-title"></p><blockquote id="compose-quote" hidden></blockquote><textarea id="compose-body" required maxlength="20000" aria-label="批注内容" placeholder="写下问题、建议或下一步…"></textarea><div class="compose-options"><select id="compose-intent" aria-label="消息意图"><option value="comment">普通意见</option><option value="rebuttal">Rebuttal · 反驳</option><option value="decision">结论</option></select><label id="compose-date-label" hidden>备忘日期（可选）<input type="date" id="compose-date"></label></div><p id="compose-error" role="alert" hidden></p><div class="reply-actions"><button type="button" id="compose-cancel" class="small-button">取消</button><span class="reply-buttons"><button type="submit" id="compose-save" class="small-button">保存</button><button type="button" id="compose-ask" class="small-button save-button">保存并请 Agent 回复</button></span></div>';
function placeComposer() {
  if(!composer||composer.documentId!==selected||selectedKind!=='document'||composer.anchor&&!markdown){composerForm.remove();return;}
  const found=composer.anchor&&inlineReady()?locateAnchor(composer.anchor).element:null;
  if(found){if(found.nextElementSibling!==composerForm)found.after(composerForm);}
  else if(composerForm.parentElement!==$('panel-composer'))$('panel-composer').append(composerForm);
}
function composeThread(kind,anchor=null,block=null) {
  if(!currentDocument()||selectedKind!=='document')return;
  if(composer&&cf('compose-body').value.trim()&&!confirm('放弃当前未保存的批注？'))return;
  composer={kind,documentId:selected,anchor:anchor?clone(anchor):null};
  cf('compose-title').textContent=kind==='memo'?'添加备忘':anchor?'添加批注':'添加整篇意见';
  cf('compose-quote').textContent=anchor?anchor.quote:'';cf('compose-quote').hidden=!anchor;
  cf('compose-body').value='';cf('compose-intent').value='comment';cf('compose-date').value='';cf('compose-date-label').hidden=kind!=='memo';cf('compose-error').hidden=true;
  $('selection-tool').hidden=true;
  if(block&&inlineReady())block.after(composerForm);else placeComposer();
  composerForm.scrollIntoView({block:'nearest'});cf('compose-body').focus({preventScroll:true});
}
function closeComposer() {composer=null;cf('compose-body').value='';composerForm.remove();}
async function submitComposer(ask) {
  if(!composer||discussionBusy)return;
  const body=cf('compose-body').value.trim();
  for(const id of ['compose-save','compose-ask','compose-cancel'])cf(id).disabled=true;cf('compose-error').hidden=true;
  try{
    if(!body)throw new Error('请填写内容');
    const id=uid(),next=clone(collaboration.threads);
    next.push({id,documentId:composer.documentId,kind:composer.kind,status:'open',dueDate:composer.kind==='memo'?cf('compose-date').value:'',anchor:composer.anchor,messages:[makeMessage(body,cf('compose-intent').value)]});
    await persistThreads(next);
    const anchored=!!composer.anchor;closeComposer();window.getSelection()?.removeAllRanges();
    threadFilter='all';$('status-filter').value='all';updateThreadFilters();
    if(anchored)openThreadInline(id);else renderThreads();
    if(ask)await requestAgentReply(id);
  }catch(error){cf('compose-error').textContent=error.message;cf('compose-error').hidden=false;}
  finally{for(const id of ['compose-save','compose-ask','compose-cancel'])cf(id).disabled=false;}
}
composerForm.addEventListener('submit',event=>{event.preventDefault();submitComposer(false);});
cf('compose-ask').onclick=()=>submitComposer(true);
cf('compose-cancel').onclick=()=>{if(!discussionBusy)closeComposer();};
composerForm.addEventListener('keydown',event=>{if(event.key==='Escape'&&!discussionBusy&&!cf('compose-body').value.trim())closeComposer();});
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
  if(start!==end||start?.hasAttribute('data-inline')||!quote.trim()||quote.length>20000)return resetDiscussionSelection();
  // Range 定位实际选中位置，避免重复文字取到第一个匹配。
  const prefix=document.createRange();prefix.selectNodeContents(start);prefix.setEnd(range.startContainer,range.startOffset);
  const index=prefix.toString().length,text=start.textContent;
  pendingAnchor={quote,before:text.slice(Math.max(0,index-48),index),after:text.slice(index+quote.length,index+quote.length+48),revision:currentDocument()?.revision||1};pendingBlock=start;
  const rect=range.getBoundingClientRect();$('selection-tool').style.left=Math.min(Math.max(12,rect.left),window.innerWidth-170)+'px';$('selection-tool').style.top=Math.max(8,rect.top-42)+'px';$('selection-tool').hidden=false;
}
$('article').addEventListener('mouseup',()=>setTimeout(captureSelection,10));$('article').addEventListener('keyup',event=>{if(!event.target.closest('[data-inline]'))captureSelection();});
document.querySelector('.reader').addEventListener('scroll',()=>$('selection-tool').hidden=true);
$('selection-tool').onpointerdown=event=>event.preventDefault();$('selection-tool').onclick=()=>composeThread('comment',pendingAnchor,pendingBlock);
document.addEventListener('pointerdown',event=>{if(!event.target.closest('#article')&&!event.target.closest('#selection-tool'))$('selection-tool').hidden=true;});
function updateThreadFilters(){for(const button of document.querySelectorAll('[data-thread-filter]'))button.setAttribute('aria-pressed',String(button.dataset.threadFilter===threadFilter));}
for(const button of document.querySelectorAll('[data-thread-filter]'))button.onclick=()=>{threadFilter=button.dataset.threadFilter;updateThreadFilters();renderThreads();};
$('status-filter').onchange=renderThreads;
$('thread-list').addEventListener('change',event=>{if(event.target.dataset.selectThread){const id=event.target.dataset.selectThread;if(event.target.checked)selectedDiscussionIDs.add(id);else selectedDiscussionIDs.delete(id);updateBatchSelection();}});
function trackReplyDraft(event){
  const form=event.target.closest('[data-form]');if(form)replyDrafts.set(form.dataset.form,{body:form.querySelector('textarea').value,intent:form.querySelector('select').value});
}
async function handleThreadClick(event){
  const button=event.target.closest('button');if(!button||discussionBusy||!collaboration)return;
  const data=button.dataset,id=data.reply||data.resolve||data.send||data.sendAsk||data.ask||data.delete||data.locate||data.forward||data.collapse||data.openThread;
  const thread=collaboration.threads.find(t=>t.id===id);if(!thread)return;
  if(data.forward){forwardThreadToAgent(id);return;}
  if(data.locate||data.openThread){if(data.openThread&&openThreadID===id){openThreadID='';renderThreads();}else openThreadInline(id);return;}
  if(data.collapse){openThreadID='';renderThreads();return;}
  if(data.reply){replyDrafts.set(id,replyDrafts.get(id)||{body:'',intent:'comment'});renderThreads();document.querySelector(`[data-form="${CSS.escape(id)}"] textarea`)?.focus();return;}
  if(data.ask){try{await requestAgentReply(id);showError(null);}catch(error){showError(error);}return;}
  try {
    const next=clone(collaboration.threads),target=next.find(t=>t.id===id);
    if(data.resolve)target.status=target.status==='open'?'resolved':'open';
    if(data.delete){if(!confirm('删除这条讨论及全部回复？'))return;next.splice(next.indexOf(target),1);}
    if(data.send||data.sendAsk){const form=button.closest('[data-form]'),body=form.querySelector('textarea').value.trim();if(!body)throw new Error('请填写回复');replyDrafts.set(id,{body,intent:form.querySelector('select').value});target.messages.push(makeMessage(body,form.querySelector('select').value));}
    await persistThreads(next);
    if(data.send||data.sendAsk||data.delete){replyDrafts.delete(id);renderThreads();}
    if(data.sendAsk)await requestAgentReply(id);
    showError(null);
  }catch(error){showError(error);}
}
for(const container of [$('thread-list'),$('article')]){container.addEventListener('click',handleThreadClick);container.addEventListener('input',trackReplyDraft);container.addEventListener('change',trackReplyDraft);}

window.addEventListener('beforeunload',event=>{
  const compose=composer&&cf('compose-body').value.trim();
  if(discussionBusy||compose||Array.from(replyDrafts.values()).some(draft=>draft.body.trim())){event.preventDefault();event.returnValue='';}
});
