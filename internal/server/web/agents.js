'use strict';
let agentSessions=[];
let selectedAgent='';
let agentMode=false;
let agentPolling=false;
let agentTimer=null;
let agentPreview=null;
let forwardedThread='';
let agentSending=false;
let renderedRequestSignature="";
let renderedEventSignature="";
const agentExpandedEvents=new Map();
const agentEventCache=new Map();
const requestDrafts=new Map();
const pendingAnswers=new Set();
const agentStateLabels={starting:'正在连接',idle:'空闲',running:'执行中',waiting:'等待你的响应',disconnected:'已断开',closed:'已关闭'};
const getAgent=()=>agentSessions.find(session=>session.id===selectedAgent);
function switchWorkbench(mode){
  if(!project)return false;
  if(mode==='agents'&&!leaveEditor())return false;
  if(mode==='agents'&&selectedKind==='document')renderMarkdown();
  agentMode=mode==='agents';$('workspace').hidden=agentMode;$('agent-workspace').hidden=!agentMode;
  $('show-documents').setAttribute('aria-pressed',String(!agentMode));$('show-agents').setAttribute('aria-pressed',String(agentMode));
  resetDiscussionSelection();updateAgentInputContext();return true;
}
$('show-documents').onclick=()=>switchWorkbench('documents');
$('show-agents').onclick=()=>{if(switchWorkbench('agents'))refreshAgents();};
async function loadAgentProject(){
  try{await refreshAgents();if(fragment.get('view')==='agents')switchWorkbench('agents');}
  catch(error){$('agent-log-status').textContent=error.message;}
  if(!agentTimer)agentTimer=setInterval(()=>{if(project)refreshAgents().catch(error=>{$('agent-log-status').textContent='连接中断：'+error.message;});},1000);
}
async function refreshAgents(){
  if(agentPolling)return;agentPolling=true;
  try{
    agentSessions=await api('agents');
    if(!agentSessions.some(s=>s.id===selectedAgent))selectAgent(agentSessions.at(-1)?.id||'');
    const pollingAgent=selectedAgent;
    if(pollingAgent){const cache=agentEventCache.get(pollingAgent)||{after:0,events:[]};const events=await api(`agents/${encodeURIComponent(pollingAgent)}/events?after=${cache.after}`);
      for(const event of events){if(event.sequence>cache.after){cache.events.push(event);cache.after=event.sequence;}}
      if(cache.events.length>3000)cache.events=cache.events.slice(-3000);agentEventCache.set(pollingAgent,cache);
      if(selectedAgent===pollingAgent)$('agent-log-status').textContent=events.length===1000?'正在补齐历史…':'事件已同步';
    }
    renderAgentSessions();renderAgent();
  }finally{agentPolling=false;}
}
function renderAgentSessions(){
  $('agent-list').replaceChildren();
  for(const session of agentSessions){const button=document.createElement('button');button.className='file-button';button.classList.toggle('active',session.id===selectedAgent);button.dataset.agent=session.id;const name=document.createElement('strong');name.textContent=session.name;const detail=document.createElement('small');detail.textContent=`${agentStateLabels[session.state]||session.state} · ${session.model||'Codex'}`;button.append(name,detail);button.onclick=()=>{selectAgent(session.id);agentPreview=null;$('agent-preview-panel').hidden=true;renderAgentSessions();renderAgent();refreshAgents();};$('agent-list').append(button);}
  if(!agentSessions.length){const text=document.createElement('p');text.textContent='还没有接入的会话。点击上方启动 Codex。';$('agent-list').append(text);}
}
function renderAgent(){
  const session=getAgent();$('agent-title').textContent=session?.name||'选择或创建会话';
  $('agent-state').textContent=session?`${agentStateLabels[session.state]||session.state}${session.lastTurnStatus?' · 上次回合 '+({completed:'已完成',failed:'失败',interrupted:'已中断'}[session.lastTurnStatus]||session.lastTurnStatus):''}${session.lastError?' · '+session.lastError:''}`:'';
  const sandbox=session?.sandbox?.type;
  $('agent-config').textContent=session?`${session.model||'模型未返回'} · ${sandbox==='readOnly'?'只读沙箱':sandbox==='workspaceWrite'?'项目写入沙箱':JSON.stringify(session.sandbox)} · 审批 ${typeof session.approvalPolicy==='string'?session.approvalPolicy:JSON.stringify(session.approvalPolicy)} · ${session.approvalsReviewer==='user'?'由你审批':session.approvalsReviewer} · ${session.threadId||'连接中'}`:'';
  $('resume-agent').hidden=!session?.threadId||!['disconnected','closed'].includes(session.state);
  $('interrupt-agent').disabled=!session?.activeTurnId||!['running','waiting'].includes(session.state);
  $('close-agent').disabled=!session||session.state==='closed';
  $('agent-queue').hidden=!session?.queue.length;
  $('agent-queue-count').textContent=session?`${session.queue.length} 条排队输入${session.queuePaused?' · 已暂停':''}`:'';
  $('continue-agent-queue').disabled=session?.state!=='idle'||!session?.queuePaused;
  renderNativeRequests(session);renderAgentEvents();updateAgentSendButton();
}
function updateAgentInputContext(){
  const enabled=selectedKind==='document'&&!!selected;
  $('include-current-document').disabled=!enabled;
  if(!enabled)$('include-current-document').checked=false;
  $('agent-document-label').textContent=enabled?selected:'';
  const thread=collaboration?.threads.find(t=>t.id===forwardedThread);
  $('agent-thread-context').textContent=thread?`讨论：${thread.documentId} · ${thread.messages.length} 条消息`:'';
  $('clear-agent-thread').hidden=!forwardedThread;
}
function previewSignature(){return JSON.stringify({text:$('agent-message').value,documents:$('include-current-document').checked?[selected]:[],threadId:forwardedThread});}
function updateAgentSendButton(){const session=getAgent();$('send-agent-input').disabled=agentSending||!session||!['idle','running','waiting'].includes(session.state)||!agentPreview||agentPreview.signature!==previewSignature();$('send-agent-input').textContent=session&&(['running','waiting'].includes(session.state)||session.queue.length||session.queuePaused)?'加入队列':'发送';}
function invalidateAgentPreview(){agentPreview=null;$('agent-preview-panel').hidden=true;updateAgentSendButton();}
$('agent-message').oninput=invalidateAgentPreview;$('include-current-document').onchange=invalidateAgentPreview;
$('clear-agent-thread').onclick=()=>{forwardedThread='';updateAgentInputContext();invalidateAgentPreview();};
async function previewAgentInput(){
  if(!$('agent-message').value.trim())throw new Error('请输入任务或消息');
  const signature=previewSignature(),input=JSON.parse(signature);const preview=await api('agents/preview',input);
  if(signature!==previewSignature())return;
  agentPreview={signature,text:preview.text};$('agent-preview').textContent=preview.text;$('agent-preview-panel').hidden=false;updateAgentSendButton();
}
$('preview-agent-input').onclick=async()=>{try{await previewAgentInput();showError(null);}catch(error){showError(error);}};
$('agent-input-form').onsubmit=async event=>{
  event.preventDefault();if(agentSending||!agentPreview||agentPreview.signature!==previewSignature())return;
  const id=selectedAgent,text=agentPreview.text,submittedSignature=agentPreview.signature;agentSending=true;updateAgentSendButton();
  try{const result=await api(`agents/${encodeURIComponent(id)}/input`,{text});$('agent-input-status').textContent=result.queued?'已加入队列':'已发送到 Codex';if(previewSignature()===submittedSignature){$('agent-message').value='';agentPreview=null;$('agent-preview-panel').hidden=true;forwardedThread='';updateAgentInputContext();}showError(null);await refreshAgents();}
  catch(error){$('agent-input-status').textContent='输入已保留';showError(error);}
  finally{agentSending=false;updateAgentSendButton();}
};
function forwardThreadToAgent(id){
  if(!switchWorkbench('agents'))return;
  forwardedThread=id;$('agent-message').value='请阅读这条讨论，给出回应或 Rebuttal，并说明依据。';$('include-current-document').checked=false;updateAgentInputContext();invalidateAgentPreview();previewAgentInput().catch(showError);refreshAgents();
}
$('new-agent').onclick=()=>{$('new-agent-error').hidden=true;$('agent-dialog').showModal();};
$('cancel-new-agent').onclick=()=>$('agent-dialog').close();
let creatingAgent=false;
$('agent-dialog').addEventListener('cancel',event=>{if(creatingAgent)event.preventDefault();});
$('new-agent-form').onsubmit=async event=>{
  event.preventDefault();if(creatingAgent)return;creatingAgent=true;$('create-agent').disabled=true;$('cancel-new-agent').disabled=true;
  try{
    const session=await api('agents',{name:$('new-agent-name').value.trim(),model:$('new-agent-model').value.trim(),sandbox:$('new-agent-sandbox').value,approvalPolicy:$('new-agent-approval').value});
    selectAgent(session.id);$('agent-dialog').close();await refreshAgents();
  }catch(error){$('new-agent-error').textContent=error.message;$('new-agent-error').hidden=false;}
  finally{creatingAgent=false;$('create-agent').disabled=false;$('cancel-new-agent').disabled=false;}
};
async function agentAction(action,body={}){if(!selectedAgent)return;try{await api(`agents/${encodeURIComponent(selectedAgent)}/${action}`,body);await refreshAgents();showError(null);}catch(error){showError(error);}}
$('interrupt-agent').onclick=()=>agentAction('interrupt');$('close-agent').onclick=()=>agentAction('close');$('resume-agent').onclick=()=>agentAction('resume');$('continue-agent-queue').onclick=()=>agentAction('queue',{action:'continue'});$('clear-agent-queue').onclick=()=>agentAction('queue',{action:'clear'});

function renderNativeRequests(session){
  const signature=JSON.stringify({id:session?.id,requests:session?.requests||[],pending:Array.from(pendingAnswers)});
  if(signature===renderedRequestSignature)return;renderedRequestSignature=signature;
  $('agent-requests').replaceChildren();
  for(const request of session?.requests||[]){
    const section=document.createElement('section');section.className='native-request';section.dataset.request=request.id;
    const heading=document.createElement('h3');const params=request.params||{};
    heading.textContent={'item/commandExecution/requestApproval':'Codex 请求执行审批','item/fileChange/requestApproval':'Codex 请求文件修改审批','item/permissions/requestApproval':'Codex 请求额外权限','item/tool/requestUserInput':'Codex 需要你的回答','mcpServer/elicitation/request':'MCP 需要响应'}[request.method]||'暂不支持的原生请求';section.append(heading);
    const description=document.createElement('pre');description.textContent=JSON.stringify(params,null,2);section.append(description);
    const actions=document.createElement('div');actions.className='request-actions';
    function addDecision(label,result){const button=document.createElement('button');button.type='button';button.className='small-button';button.textContent=label;button.disabled=pendingAnswers.has(session.id+request.id);button.onclick=()=>answerNativeRequest(session.id,request.id,result);actions.append(button);}
    if(['item/commandExecution/requestApproval','item/fileChange/requestApproval'].includes(request.method)){addDecision('本次允许',{decision:'accept'});addDecision('拒绝',{decision:'decline'});addDecision('取消',{decision:'cancel'});}
    else if(request.method==='item/permissions/requestApproval'){addDecision('本回合授予所列权限',{permissions:params.permissions,scope:'turn'});addDecision('拒绝授权',{permissions:{},scope:'turn'});}
    else if(request.method==='mcpServer/elicitation/request'){addDecision('取消此请求',{action:'cancel'});}
    else if(request.method==='item/tool/requestUserInput'){
      const form=document.createElement('form');
      for(const question of params.questions||[]){const label=document.createElement('label');label.textContent=question.question;const input=document.createElement('input');input.type=question.isSecret?'password':'text';input.required=true;input.dataset.question=question.id;const key=session.id+request.id+question.id;input.value=requestDrafts.get(key)||'';input.oninput=()=>requestDrafts.set(key,input.value);label.append(input);
        if(question.options?.length){const choices=document.createElement('select');const blank=document.createElement('option');blank.value='';blank.textContent='选择答案或自行输入';choices.append(blank);for(const option of question.options){const item=document.createElement('option');item.value=option.label;item.textContent=option.label+' · '+option.description;choices.append(item);}choices.onchange=()=>{input.value=choices.value;requestDrafts.set(key,input.value);};label.append(choices);}
        form.append(label);
      }
      const button=document.createElement('button');button.type='submit';button.className='small-button save-button';button.textContent='提交回答';button.disabled=pendingAnswers.has(session.id+request.id);form.append(button);form.onsubmit=event=>{event.preventDefault();const answers={};for(const input of form.querySelectorAll('[data-question]'))answers[input.dataset.question]={answers:[input.value]};answerNativeRequest(session.id,request.id,{answers});};section.append(form);
    }else{const text=document.createElement('p');text.textContent='此请求无法在当前版本回应；可停止回合或关闭会话，不会自动批准。';section.append(text);}
    section.append(actions);$('agent-requests').append(section);
  }
}
async function answerNativeRequest(sessionID,requestID,result){const key=sessionID+requestID;if(pendingAnswers.has(key))return;pendingAnswers.add(key);renderNativeRequests(getAgent());try{await api(`agents/${encodeURIComponent(sessionID)}/requests/${encodeURIComponent(requestID)}`,{result});for(const key of requestDrafts.keys())if(key.startsWith(sessionID+requestID))requestDrafts.delete(key);await refreshAgents();showError(null);}catch(error){showError(error);}finally{pendingAnswers.delete(key);renderNativeRequests(getAgent());}}
function eventSummary(event){
  const params=event.params||{},item=params.item||{};
  if(event.method==='item/completed'){
    if(item.type==='agentMessage')return {title:'Agent 回复',body:item.text||'',itemID:item.id,reply:true};
    if(item.type==='userMessage')return {title:'用户输入',body:(item.content||[]).map(part=>part.text||'').join('\n')};
    if(item.type==='commandExecution')return {title:`执行命令 · ${{completed:'已完成',failed:'失败',declined:'已拒绝',inProgress:'执行中'}[item.status]||item.status||''}`,body:`${item.command||''}\n${item.cwd||''}\n\n${item.aggregatedOutput||''}${item.exitCode!=null?'\n退出码：'+item.exitCode:''}`};
    if(item.type==='fileChange')return {title:`文件修改 · ${{completed:'已完成',failed:'失败',declined:'已拒绝',inProgress:'执行中'}[item.status]||item.status||''}`,body:JSON.stringify(item.changes||[],null,2)};
    return {title:'工具事件 · '+(item.type||event.method),body:''};
  }
  if(event.method==='workbench/inputAccepted')return {title:params.queued?'输入已排队':'任务输入',body:params.input?.text||''};
  if(event.method==='turn/completed')return {title:'回合结束 · '+({completed:'已完成',failed:'失败',interrupted:'已中断'}[params.turn?.status]||params.turn?.status||''),body:params.turn?.error?JSON.stringify(params.turn.error):''};
  if(event.method==='workbench/runtimeDiagnostic')return {title:'运行时诊断',body:params.text||''};
  if(event.method==='workbench/runtimeExited')return {title:'原生进程退出',body:params.error||''};
  if(event.method==='workbench/emptySessionRecreated')return {title:'空会话已重新连接',body:`${params.reason}\n原 thread：${params.previousThreadId}\n新 thread：${params.threadId}`};
  return {title:{'workbench/sessionConnected':'Codex 会话已连接','workbench/requestAnswerSubmitted':'已提交原生请求回应','workbench/protocolError':'原生接口出错'}[event.method]||event.method,body:''};
}
function renderAgentEvents(){
  const container=$('agent-events'),nearBottom=container.scrollHeight-container.scrollTop-container.clientHeight<80;
  const cache=agentEventCache.get(selectedAgent),events=cache?.events||[];
  const rawEvents=$('agent-raw-events').checked;
  const signature=JSON.stringify([selectedAgent,cache?.after,rawEvents,getAgent()?.state]);if(signature===renderedEventSignature)return;renderedEventSignature=signature;
  if(container.dataset.agent)agentExpandedEvents.set(container.dataset.agent,new Set(Array.from(container.querySelectorAll('.agent-event details[open]')).map(el=>el.closest('.agent-event').dataset.sequence)));container.dataset.agent=selectedAgent;
  const expanded=agentExpandedEvents.get(selectedAgent)||new Set();
  const visible=events.filter(event=>rawEvents||event.method==='item/completed'||event.method==='turn/completed'||event.method.startsWith('workbench/')||event.requestId).slice(-100);
  container.replaceChildren();
  const session=getAgent();
  for(const event of visible){
    const summary=eventSummary(event),card=document.createElement('section');card.className='agent-event';card.dataset.sequence=String(event.sequence);
    const time=document.createElement('time');time.textContent=new Date(event.createdAt).toLocaleTimeString('zh-CN');const title=document.createElement('h3');title.textContent=summary.title;card.append(time,title);
    if(summary.body){const body=document.createElement('pre');body.textContent=summary.body;card.append(body);}
    const details=document.createElement('details'),caption=document.createElement('summary'),payload=document.createElement('pre');caption.textContent='原始事件 #'+event.sequence;payload.textContent=JSON.stringify(event,null,2);details.append(caption,payload);details.open=expanded.has(String(event.sequence));card.append(details);
    if(summary.reply&&session){const button=document.createElement('button');button.className='small-button';button.textContent='加入文档讨论';button.onclick=()=>addAgentReplyToDiscussion(session,summary);card.append(button);}
    container.append(card);
  }
  if(session&&['running','waiting'].includes(session.state)){
    const live=new Map();for(const event of events){if(event.method==='item/agentMessage/delta'){const id=event.params?.itemId;if(id)live.set(id,(live.get(id)||'')+(event.params.delta||''));}else if(event.method==='item/completed')live.delete(event.params?.item?.id);}
    for(const text of live.values()){const card=document.createElement('section');card.className='agent-event';const title=document.createElement('h3');title.textContent='Agent 正在回复…';const body=document.createElement('pre');body.textContent=text;card.append(title,body);container.append(card);}
  }
  if(!events.length){const empty=document.createElement('p');empty.className='empty-list';empty.textContent=session?'会话已建立。预览并发送第一项任务。':'还没有接入的会话。';container.append(empty);}
  if(nearBottom)container.scrollTop=container.scrollHeight;
}
$('agent-raw-events').onchange=renderAgentEvents;
function addAgentReplyToDiscussion(session,summary){
  const threads=collaboration?.threads||[];
  if(!threads.length){showError(new Error('请先在文档中建立一条讨论'));return;}
  openDiscussionDialog('将原生回复加入讨论',`<label>目标讨论<select id="reply-target-thread">${threads.map(t=>`<option value="${esc(t.id)}">${esc(t.documentId+' · '+t.messages[0].body.slice(0,50))}</option>`).join('')}</select></label><label>回复内容<textarea id="agent-reply-body" required></textarea></label><p>来源：${esc(session.name)} · ${esc(session.threadId)} · ${esc(summary.itemID)}</p>`,async()=>{
    const body=$('agent-reply-body').value.trim();if(!body)throw new Error('请填写回复');
    const state=await api('collaboration'),thread=state.threads.find(t=>t.id===$('reply-target-thread').value);if(!thread)throw new Error('目标讨论已删除');
    thread.messages.push({id:uid(),author:session.name,role:'agent',intent:'comment',body:`${body}\n\n[手动转发来源：Codex ${session.threadId} · item ${summary.itemID}]`,createdAt:new Date().toISOString()});
    collaboration=await api('collaboration',{generation:state.generation,threads:state.threads});renderThreads();$('discussion-status').textContent='原生回复已加入讨论';
  },'加入讨论');$('agent-reply-body').value=summary.body;
}
window.addEventListener('beforeunload',event=>{if(agentSending||creatingAgent||$('agent-message').value.trim()){event.preventDefault();event.returnValue='';}});

let fullLogID='';let fullLogStarts=[0];let fullLogPage=0;let fullLogLast=0;let fullLogBusy=false;let fullLogGeneration=0;
function selectAgent(id){
 if(id===selectedAgent)return;
 selectedAgent=id;fullLogGeneration++;fullLogID='';fullLogStarts=[0];fullLogPage=0;fullLogLast=0;fullLogBusy=false;
 $('agent-history-content').textContent='';$('agent-history-range').textContent='';$('agent-history-next').disabled=true;$('agent-history-previous').disabled=true;
 if($('agent-history').open&&id)readFullLog(0);
}
async function readFullLog(after){
 if(!selectedAgent||fullLogBusy)return;fullLogBusy=true;
 const id=selectedAgent,generation=fullLogGeneration;$('agent-history-next').disabled=true;$('agent-history-previous').disabled=true;
 try{
  const events=await api(`agents/${encodeURIComponent(id)}/events?after=${after}`);
  if(id!==selectedAgent||generation!==fullLogGeneration)return;
  fullLogID=id;fullLogLast=events.at(-1)?.sequence||after;
  $('agent-history-content').textContent=events.map(event=>JSON.stringify(event,null,2)).join('\n\n');
  $('agent-history-range').textContent=events.length?`事件 #${events[0].sequence}–#${fullLogLast} · ${events.length} 条`:'没有更多事件';
 }catch(error){if(generation===fullLogGeneration)showError(error);}finally{if(generation===fullLogGeneration){fullLogBusy=false;$('agent-history-previous').disabled=fullLogPage===0;$('agent-history-next').disabled=fullLogLast<=after;}}
}
$('agent-history').addEventListener('toggle',()=>{if($('agent-history').open){if(fullLogID!==selectedAgent){fullLogStarts=[0];fullLogPage=0;fullLogLast=0;}readFullLog(fullLogStarts[fullLogPage]);}});
$('agent-history-next').onclick=()=>{if(fullLogBusy)return;fullLogStarts=fullLogStarts.slice(0,fullLogPage+1);fullLogStarts.push(fullLogLast);fullLogPage++;readFullLog(fullLogLast);};
$('agent-history-previous').onclick=()=>{if(fullLogBusy||fullLogPage===0)return;fullLogPage--;readFullLog(fullLogStarts[fullLogPage]);};
