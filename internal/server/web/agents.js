'use strict';
let agentSessions=[];
let selectedAgent='';
let agentMode=false;
let agentPolling=false;
let agentTimer=null;
let agentPreview=null;
let selectedThreads=[];
let agentSending=false;
let renderedRequestSignature="";
let renderedEventSignature="";
const agentExpandedEvents=new Map();
const agentEventCache=new Map();
const requestDrafts=new Map();
const pendingAnswers=new Set();
const agentDrafts=new Map();
const agentReplyDrafts=new Map();
let renderedDiscussionSignature="";
const agentStateLabels={starting:'正在连接',idle:'空闲',running:'执行中',waiting:'等待你的响应',disconnected:'已断开',closed:'已关闭'};
const getAgent=()=>agentSessions.find(session=>session.id===selectedAgent);
let agentStatusError='';
function agentStatusText(session){
 if(session.state!=='idle')return agentStateLabels[session.state]||session.state;
 if(session.contextUncertain)return '送达结果不明';
 if(session.queuePaused)return `队列已暂停 · ${session.queue.length} 条`;
 if(session.queue.length)return `排队 ${session.queue.length} 条`;
 return {completed:'回合已结束',failed:'失败',interrupted:'已中断'}[session.lastTurnStatus]||'空闲 · 未发送任务';
}
function renderAgentStatus(){
 const indicator=$('agent-status-overview');indicator.hidden=!project;
 const running=agentSessions.filter(s=>s.state==='running').length,waiting=agentSessions.filter(s=>s.state==='waiting').length;
 indicator.textContent=agentStatusError?'状态未同步':running||waiting?`执行中 ${running} · 等待响应 ${waiting}`:`${agentSessions.length} 个会话 · 无执行中任务`;
 indicator.dataset.state=agentStatusError?'disconnected':waiting?'waiting':running?'running':'idle';indicator.title=agentStatusError||'点击查看会话执行状态';
 for(const container of document.querySelectorAll('[data-discussion-sessions]')){
  const documentId=collaboration?.threads.find(t=>t.id===container.dataset.discussionSessions)?.documentId;const sessions=agentSessions.filter(s=>s.parentSessionId&&s.documentId===documentId);container.replaceChildren();
  for(const session of sessions){const button=document.createElement('button');button.type='button';button.className='session-status-link';button.dataset.state=agentStatusError?'disconnected':session.state;button.textContent=`${session.name} · ${agentStatusError?'状态未同步':agentStatusText(session)}`;button.onclick=()=>{selectAgent(session.id);if(switchWorkbench('agents')){renderAgentSessions();renderAgent();refreshAgents().catch(showError);}};container.append(button);}
 }
 if(typeof renderSessionInteractions==='function')renderSessionInteractions();
}
$('agent-status-overview').onclick=()=>{if(!leaveEditor())return;const active=agentSessions.find(s=>s.state==='waiting')||agentSessions.find(s=>s.state==='running');if(active)selectAgent(active.id);if(switchWorkbench('agents')){renderAgentSessions();renderAgent();refreshAgents().catch(showError);}};

function switchWorkbench(mode){
  if(!project)return false;
  if(mode!=='documents'&&!leaveEditor())return false;
  if(mode==='agents'&&selectedKind==='document')renderMarkdown();
  agentMode=mode==='agents';$('workspace').hidden=mode!=='documents';$('agent-workspace').hidden=!agentMode;if($('memory-workspace'))$('memory-workspace').hidden=mode!=='memory';
  $('show-documents').setAttribute('aria-pressed',String(mode==='documents'));if($('show-memory'))$('show-memory').setAttribute('aria-pressed',String(mode==='memory'));$('show-agents').setAttribute('aria-pressed',String(agentMode));
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
    agentSessions=await api('agents');agentStatusError='';renderAgentStatus();
    if(!agentSessions.some(s=>s.id===selectedAgent))selectAgent(agentSessions.at(-1)?.id||'');
    const pollingAgent=selectedAgent;
    if(pollingAgent){try{const cache=agentEventCache.get(pollingAgent)||{after:0,events:[]};const events=await api(`agents/${encodeURIComponent(pollingAgent)}/events?after=${cache.after}`);
      for(const event of events){if(event.sequence>cache.after){cache.events.push(event);cache.after=event.sequence;}}
      const nativeThread=agentSessions.find(s=>s.id===pollingAgent)?.threadId||'';if(!cache.timeline||cache.threadID!==nativeThread){cache.threadID=nativeThread;cache.timeline=createAgentTimeline(nativeThread);cache.timeline.append(cache.events);}else cache.timeline.append(events);if(cache.events.length>3000)cache.events=cache.events.slice(-3000);agentEventCache.set(pollingAgent,cache);
      if(selectedAgent===pollingAgent)$('agent-log-status').textContent=events.length===1000?'正在补齐历史…':'事件已同步';
    }catch(error){$('agent-log-status').textContent='日志读取失败：'+error.message;}}
    if(!discussionBusy&&!$('discussion-dialog').open){try{const latest=await api('collaboration');if(!discussionBusy&&latest.generation>(collaboration?.generation||0)){collaboration=latest;renderThreads();}}catch(error){$('discussion-status').textContent='讨论读取失败：'+error.message;}}
    renderAgentSessions();renderAgent();
    if(typeof renderBatchRows==='function'&&$('batch-dialog').open&&discussionBatch?.rows.length&&!batchBusy)renderBatchRows(discussionBatch.rows);
  }catch(error){agentStatusError=error.message;renderAgentStatus();throw error;}finally{agentPolling=false;}
}
function renderAgentSessions(){
  $('agent-list').replaceChildren();
  for(const session of agentSessions){const button=document.createElement('button');button.className='file-button';button.classList.toggle('active',session.id===selectedAgent);button.dataset.agent=session.id;const name=document.createElement('strong');name.textContent=session.name;const detail=document.createElement('small');detail.textContent=session.parentSessionId?`${session.documentId} · 文档讨论 ${(collaboration?.threads||[]).filter(t=>t.documentId===session.documentId).length} 条`:session.model||'Codex';const status=document.createElement('span');status.className='agent-state-badge';status.dataset.state=agentStatusError?'disconnected':session.state;status.textContent=agentStatusError?'状态未同步':agentStatusText(session);button.append(name,status,detail);button.onclick=()=>{selectAgent(session.id);agentPreview=null;$('agent-preview-panel').hidden=true;renderAgentSessions();renderAgent();refreshAgents();};$('agent-list').append(button);}
  if(!agentSessions.length){const text=document.createElement('p');text.textContent='还没有接入的会话。点击上方启动 Codex。';$('agent-list').append(text);}
}
function renderAgent(){
  const session=getAgent();$('agent-title').textContent=session?.name||'选择或创建会话';
  $('agent-state').textContent=session?`${agentStatusError?'状态未同步':agentStatusText(session)}${session.lastError?' · '+session.lastError:''}`:''; $('agent-state').dataset.state=agentStatusError?'disconnected':session?.state||'idle';
  const sandbox=session?.sandbox?.type;
  $('agent-config').textContent=session?`${session.model||'模型未返回'} · ${sandbox==='readOnly'?'只读沙箱':sandbox==='workspaceWrite'?'项目写入沙箱':JSON.stringify(session.sandbox)} · 审批 ${typeof session.approvalPolicy==='string'?session.approvalPolicy:JSON.stringify(session.approvalPolicy)} · ${session.approvalsReviewer==='user'?'由你审批':session.approvalsReviewer} · ${session.threadId||'连接中'}`:'';
  $('resume-agent').hidden=!session?.threadId||!['disconnected','closed'].includes(session.state);
  $('interrupt-agent').disabled=!session?.activeTurnId||!['running','waiting'].includes(session.state);
  $('close-agent').disabled=!session||session.state==='closed';
  $('compact-agent').disabled=!session||session.state!=='idle'||!!session.queue.length;
  $('agent-queue').hidden=!session?.queue.length;
  $('agent-queue-count').textContent=session?`${session.queue.length} 条排队输入${session.queuePaused?' · 已暂停':''}`:'';
  $('continue-agent-queue').disabled=session?.state!=='idle'||!session?.queuePaused||!!session?.contextUncertain;
  renderNativeRequests(session);renderAgentEvents();renderAgentDiscussion(session);updateAgentInputContext();updateAgentSendButton();if(typeof renderSessionInteractions==='function')renderSessionInteractions();
}
function documentThreads(session){return (collaboration?.threads||[]).filter(t=>t.documentId===session?.documentId);}
function updateAgentInputContext(){
 const session=getAgent(),bound=!!session?.parentSessionId,enabled=bound||selectedKind==='document'&&!!selected;
 $('include-current-document').disabled=!enabled;if(!enabled)$('include-current-document').checked=false;
 $('agent-document-label').textContent=bound?session.documentId:enabled?selected:'';
 const threads=bound?documentThreads(session):[];selectedThreads=selectedThreads.filter(id=>threads.some(t=>t.id===id));
 $('agent-thread-context').textContent=!bound?'':!threads.length?'该文档的讨论已全部删除':selectedThreads.length?`回应 ${selectedThreads.length} 条指定讨论`:`回应全部待处理讨论（${threads.filter(t=>t.status==='open').length} 条）`;
 $('clear-agent-thread').hidden=!bound||!selectedThreads.length;$('full-context-option').hidden=!bound;
}
function previewSignature(){const session=getAgent(),bound=!!session?.parentSessionId;return JSON.stringify({text:$('agent-message').value,documents:$('include-current-document').checked?[bound?session.documentId:selected]:[],threadIds:bound?selectedThreads:[],sessionId:selectedAgent,full:!!$('full-agent-context').checked});}
function updateAgentSendButton(){
 const session=getAgent(),connected=session&&['idle','running','waiting'].includes(session.state),previewed=agentPreview&&agentPreview.signature===previewSignature();
 $('send-agent-input').disabled=agentSending||!connected||!previewed;
 $('send-agent-input').textContent=session&&(['running','waiting'].includes(session.state)||session.queue.length||session.queuePaused)?'加入队列':'发送';
 $('agent-send-hint').textContent=agentSending?'正在发送…':!session?'请先选择或启动一个会话。':!connected?'会话尚未连接，请先恢复会话或启动新会话。':!$('agent-message').value.trim()?'输入消息后，先点击「预览任务」，再发送。':!previewed?'请先点击「预览任务」；修改内容后需要重新预览。':'内容已预览，可以发送。';
}
function invalidateAgentPreview(){agentPreview=null;$('agent-preview-panel').hidden=true;updateAgentSendButton();}
$('agent-message').oninput=invalidateAgentPreview;$('include-current-document').onchange=invalidateAgentPreview;$('full-agent-context').onchange=invalidateAgentPreview;
$('clear-agent-thread').onclick=()=>{selectedThreads=[];renderedDiscussionSignature='';renderAgentDiscussion(getAgent());updateAgentInputContext();invalidateAgentPreview();};
async function previewAgentInput(){
  if(!$('agent-message').value.trim())throw new Error('请输入任务或消息');
  const signature=previewSignature(),input=JSON.parse(signature);const preview=await api('agents/preview',input);
  if(signature!==previewSignature())return;
  agentPreview={signature,text:preview.text,checksum:preview.checksum,mode:preview.mode};$('agent-preview-panel').querySelector('summary').textContent=`将发送的完整内容 · ${preview.mode||'普通消息'}`;$('agent-preview').textContent=preview.text;$('agent-preview-panel').hidden=false;$('agent-preview-panel').open=false;updateAgentSendButton();
}
$('preview-agent-input').onclick=async()=>{try{await previewAgentInput();showError(null);}catch(error){showError(error);}};
$('agent-input-form').onsubmit=async event=>{
  event.preventDefault();if(agentSending||!agentPreview||agentPreview.signature!==previewSignature())return;
  const id=selectedAgent,text=agentPreview.text,submittedSignature=agentPreview.signature,checksum=agentPreview.checksum;agentSending=true;updateAgentSendButton();
  try{const result=await api(`agents/${encodeURIComponent(id)}/input`,checksum?{preview:JSON.parse(submittedSignature),checksum}:{text});$('agent-input-status').textContent=result.queued?'已加入队列':'已发送到 Codex';if(selectedAgent===id&&previewSignature()===submittedSignature){$('agent-message').value='';agentDrafts.delete(id);agentPreview=null;$('agent-preview-panel').hidden=true;selectedThreads=[];renderedDiscussionSignature='';updateAgentInputContext();}else if(agentDrafts.get(id)?.signature===submittedSignature){agentDrafts.delete(id);}showError(null);await refreshAgents();}
  catch(error){$('agent-input-status').textContent='输入已保留';showError(error);}
  finally{agentSending=false;updateAgentSendButton();}
};
// 回复可读性要求：附加在各任务指令末尾，约束 Agent 在工作台阅读场景下的回复结构与长度；未来配置化模板按项目替换此块。
const replyReadabilityRules='回复要求：\n- 结论先行：首句直接给出结果或答复，再给依据和过程。\n- 结构分块：用 Markdown 标题、列表或表格分段，一段只讲一个内容。\n- 控制长度：每条讨论回应默认 300 字以内；用户明确要求展开时再给完整细节。\n- 代码规范：代码放入带语言的代码块，命令给完整可复制的命令行。\n- 实施说明：完成实现或修改时列出改动文件与验证方式；仅完成分析时明确写「尚未实施」。\n- 只写有信息量的内容：不写客套话、过渡寒暄和表情符号。';
function discussionTaskInstruction(thread){
 const latest=thread?.messages.filter(m=>m.role==='human').at(-1)?.body||'请处理这条讨论中用户的最新要求。';
 return latest+'\n\n请结合引用和讨论背景处理以上用户要求。若要求实现或修改，请实际实施并验证；若要求讨论，请回应并说明依据。仅完成分析时应明确说明尚未实施。\n\n'+replyReadabilityRules;
}
function forwardThreadToAgent(id){
 const parents=agentSessions.filter(s=>!s.parentSessionId);
 if(!parents.length){switchWorkbench('agents');showError(new Error('请先启动一个工作会话，再选择它参与讨论'));return;}
 if(!leaveEditor())return;
 openDiscussionDialog('选择参与 Agent',`<label>工作会话<select id="discussion-parent-agent">${parents.map(s=>`<option value="${esc(s.id)}">${esc(s.name)}</option>`).join('')}</select></label><p>为这个 Agent 创建或打开该文档的讨论会话；同一文档的批注共用该会话，不继承工作会话的完整历史，也不会自动发送任务。</p>`,async()=>{
  const thread=collaboration?.threads.find(t=>t.id===id);if(!thread)throw new Error('讨论已删除');
  const session=await api(`agents/${encodeURIComponent($('discussion-parent-agent').value)}/discussion`,{documentId:thread.documentId});
  agentSessions=agentSessions.filter(s=>s.id!==session.id).concat(session);selectAgent(session.id);switchWorkbench('agents');
  if(!$('agent-message').value.trim())$('agent-message').value=discussionTaskInstruction(thread);
  selectedThreads=[id];renderedDiscussionSignature='';invalidateAgentPreview();renderAgentSessions();renderAgent();await previewAgentInput();
 },'打开讨论会话');
 const preferred=getAgent()?.parentSessionId||selectedAgent;if(parents.some(s=>s.id===preferred))$('discussion-parent-agent').value=preferred;
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
$('compact-agent').onclick=()=>{const s=getAgent();if(!s)return;if(!confirm(`清空「${s.name}」的原生上下文？\n将重建 Codex thread；工作台日志、选项与讨论记录保留。`))return;agentAction('compact');};

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
  if(event.method==='workbench/contextCleared')return {title:'上下文已清空',body:`${params.reason}\n原 thread：${params.previousThreadId}\n新 thread：${params.threadId}`};
  return {title:{'workbench/sessionConnected':'Codex 会话已连接','workbench/requestAnswerSubmitted':'已提交原生请求回应','workbench/protocolError':'原生接口出错'}[event.method]||event.method,body:''};
}
function renderAgentEvents(){
 const cache=agentEventCache.get(selectedAgent),rawEvents=$('agent-raw-events').checked;
 const signature=JSON.stringify([selectedAgent,cache?.after,rawEvents,getAgent()?.state]);if(signature===renderedEventSignature)return;renderedEventSignature=signature;
 const result=cache?.timeline?.view(rawEvents)||{conversation:[],execution:[]};
 for(const [id,records,dialogue] of [['agent-conversation',result.conversation,true],['agent-execution',result.execution,false]]){
  const container=$(id),scroll=container.scrollTop,nearBottom=container.scrollHeight-scroll-container.clientHeight<80;
  const expanded=new Set(Array.from(container.querySelectorAll('details[open]')).map(el=>el.dataset.detailKey));container.replaceChildren();
  for(const entry of records){
   const card=document.createElement('section');card.className='agent-event';card.dataset.entry=entry.key;card.dataset.turn=entry.turnID||'';
   const time=document.createElement('time');time.textContent=new Date(entry.createdAt).toLocaleTimeString('zh-CN');const title=document.createElement('h3');
   title.textContent=dialogue?(entry.role==='human'?'用户':'Agent')+(entry.phase==='commentary'?' · 中间消息':'')+(entry.status==='queued'?' · 排队':entry.status==='cleared'?' · 已移出队列':''):entry.title;card.append(time,title);
   if(dialogue){const body=document.createElement('div');body.className='markdown-message';renderProjectMarkdown(body,entry.body,getAgent()?.documentId||'PLAN.md');card.append(body);}
   else if(entry.body){const body=document.createElement('pre');body.textContent=entry.body;card.append(body);}
   if(!dialogue&&entry.changes?.length){const view=document.createElement('div');renderUnifiedDiff(view,fileChangePatch(entry.changes,project?.root));card.append(view);}
   const details=(label,body,key)=>{const detail=document.createElement('details'),summary=document.createElement('summary'),content=document.createElement('pre');detail.dataset.detailKey=key;detail.open=expanded.has(key);summary.textContent=label;content.textContent=body;detail.append(summary,content);card.append(detail);};
   if(entry.context)details('附带材料 · 原文',entry.context,entry.key+':context');
   if(!dialogue&&entry.event)details('原始事件 #'+entry.sequence,JSON.stringify(entry.event,null,2),entry.key+':raw');
   if(entry.turnID){const locate=document.createElement('button');locate.className='small-button';locate.textContent=dialogue?'定位执行回合':'定位对话回合';locate.onclick=()=>{const other=$(dialogue?'agent-execution':'agent-conversation');for(const candidate of other.querySelectorAll('[data-turn]'))candidate.classList.toggle('turn-highlight',candidate.dataset.turn===entry.turnID);Array.from(other.querySelectorAll('[data-turn]')).find(el=>el.dataset.turn===entry.turnID)?.scrollIntoView({block:'nearest'});};card.append(locate);}
   if(dialogue){const copy=document.createElement('button');copy.className='small-button';copy.textContent='复制原文';copy.onclick=()=>navigator.clipboard.writeText(entry.body+(entry.context||'')).catch(showError);card.append(copy);}
   if(dialogue&&entry.role==='agent'&&entry.complete){const session=getAgent(),button=document.createElement('button');button.className='small-button';button.dataset.addReply='';button.textContent='加入文档讨论';button.onclick=()=>addAgentReplyToDiscussion(session,entry);card.append(button);}
   container.append(card);
  }
  if(!records.length){const empty=document.createElement('p');empty.className='empty-list';empty.textContent=dialogue?'发送任务后，对话在这里显示。':'命令、文件修改和回合状态在这里显示。';container.append(empty);}
  container.scrollTop=nearBottom?container.scrollHeight:scroll;
 }
}
$('agent-raw-events').onchange=renderAgentEvents;
function addAgentReplyToDiscussion(session,summary){
  const threads=collaboration?.threads||[];
  if(!threads.length){showError(new Error('请先在文档中建立一条讨论'));return;}
  openDiscussionDialog('将原生回复加入讨论',`<label>目标讨论<select id="reply-target-thread">${threads.map(t=>`<option value="${esc(t.id)}">${esc(t.documentId+' · '+t.messages[0].body.slice(0,50))}</option>`).join('')}</select></label><label>回复内容<textarea id="agent-reply-body" required></textarea></label><p>来源：${esc(session.name)} · ${esc(session.threadId)} · ${esc(summary.itemID)}</p>`,async()=>{
    const body=$('agent-reply-body').value.trim();if(!body)throw new Error('请填写回复');
    const state=await api('collaboration'),thread=state.threads.find(t=>t.id===$('reply-target-thread').value);if(!thread)throw new Error('目标讨论已删除');
    const source={sessionId:session.id,threadId:session.threadId,turnId:summary.turnID,itemId:summary.itemID};
    const previous=thread.messages.find(m=>m.source&&['sessionId','threadId','turnId','itemId'].every(key=>m.source[key]===source[key]));
    if(!previous||previous.body!==body)thread.messages.push({id:uid(),author:session.name,role:'agent',intent:'comment',body,createdAt:new Date().toISOString(),...(!previous&&summary.turnID?{source}:{})});
    collaboration=await api('collaboration',{generation:state.generation,threads:state.threads});renderThreads();$('discussion-status').textContent='原生回复已加入讨论';
  },'加入讨论');$('agent-reply-body').value=summary.body;
}
window.addEventListener('beforeunload',event=>{if(agentSending||creatingAgent||$('agent-message').value.trim()||$('agent-discussion-reply').value.trim()||Array.from(agentDrafts.values()).some(d=>d.text.trim())||Array.from(agentReplyDrafts.values()).some(text=>text.trim())){event.preventDefault();event.returnValue='';}});

let fullLogID='';let fullLogStarts=[0];let fullLogPage=0;let fullLogLast=0;let fullLogBusy=false;let fullLogGeneration=0;
function selectAgent(id){
 if(id===selectedAgent)return;
 if(selectedAgent){agentDrafts.set(selectedAgent,{text:$('agent-message').value,include:$('include-current-document').checked,full:$('full-agent-context').checked,threads:[...selectedThreads],signature:previewSignature()});agentReplyDrafts.set(selectedAgent,$('agent-discussion-reply').value);}const draft=agentDrafts.get(id);$('agent-message').value=draft?.text||'';$('include-current-document').checked=!!draft?.include;$('full-agent-context').checked=!!draft?.full;selectedThreads=[...(draft?.threads||[])];renderedDiscussionSignature='';$('agent-discussion-reply').value=agentReplyDrafts.get(id)||'';agentPreview=null;$('agent-preview-panel').hidden=true;selectedAgent=id;fullLogGeneration++;fullLogID='';fullLogStarts=[0];fullLogPage=0;fullLogLast=0;fullLogBusy=false;
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

function renderAgentDiscussion(session){
 const bound=!!session?.parentSessionId;$('agent-bound-discussion').hidden=!bound;if(!bound){renderedDiscussionSignature='';return;}
 const threads=documentThreads(session),parent=agentSessions.find(s=>s.id===session.parentSessionId);
 $('agent-discussion-title').textContent='文档讨论';$('agent-discussion-origin').textContent=`${session.documentId} · ${threads.length} 条讨论 · 参与者 ${parent?.name||session.parentSessionId}`;
 $('save-agent-discussion-reply').disabled=!threads.length||discussionBusy;$('return-to-work').disabled=!threads.length||!parent;
 const failed=(session.replies||[]).filter(reply=>reply.error);const pending=(session.replies||[]).filter(reply=>!reply.synced&&!reply.discarded);
 $('agent-discussion-sync').textContent=!threads.length?'该文档的讨论已全部删除；会话回复仍保留。':failed.length?failed.map(reply=>reply.error).join('\n'):pending.length?'最终回复正在写回讨论…':'最终回复按「讨论 <ID>」标题自动写回各条讨论。';
 const signature=JSON.stringify([session.id,threads,selectedThreads]);if(signature===renderedDiscussionSignature)return;renderedDiscussionSignature=signature;
 const target=$('agent-discussion-thread'),previous=target.value;target.replaceChildren();
 for(const thread of threads){const option=document.createElement('option');option.value=thread.id;option.textContent=(thread.anchor?.quote||thread.messages[0]?.body||thread.id).slice(0,40);target.append(option);}
 if(threads.some(t=>t.id===previous))target.value=previous;else if(selectedThreads.length)target.value=selectedThreads[0];
 const container=$('agent-discussion-messages'),scroll=container.scrollTop;container.replaceChildren();
 for(const thread of threads){
  const section=document.createElement('section');section.className='bound-thread';section.dataset.thread=thread.id;
  const pick=document.createElement('label');pick.className='bound-thread-pick';const box=document.createElement('input');box.type='checkbox';box.checked=selectedThreads.includes(thread.id);box.dataset.pickThread=thread.id;
  pick.append(box,` ${thread.kind==='memo'?'备忘':'批注'} · ${thread.status==='resolved'?'已解决':'待处理'} · 本次回应`);section.append(pick);
  if(thread.anchor){const quote=document.createElement('blockquote');quote.textContent=thread.anchor.quote;section.append(quote);}
  for(const message of thread.messages){const card=document.createElement('div');card.className='message';card.dataset.messageId=message.id;const label=document.createElement('div');label.className='message-meta';label.textContent=`${message.author} · ${message.intent==='decision'?'结论':message.intent==='rebuttal'?'Rebuttal':message.role==='agent'?'Agent':'用户'}`;const body=document.createElement('div');body.className='markdown-message';renderProjectMarkdown(body,message.body,session.documentId);card.append(label,body);section.append(card);}
  container.append(section);
 }
 container.scrollTop=scroll;
}
$('agent-discussion-messages').addEventListener('change',event=>{
 const id=event.target.dataset.pickThread;if(!id)return;
 selectedThreads=event.target.checked?[...new Set([...selectedThreads,id])]:selectedThreads.filter(t=>t!==id);
 renderedDiscussionSignature='';renderAgentDiscussion(getAgent());updateAgentInputContext();invalidateAgentPreview();
});
$('open-agent-document').onclick=()=>{const session=getAgent(),target=entries().find(item=>item.path===session?.documentId);if(target&&switchWorkbench('documents'))loadEntry(target);};
$('agent-discussion-reply').oninput=()=>agentReplyDrafts.set(selectedAgent,$('agent-discussion-reply').value);
$('agent-discussion-form').onsubmit=async event=>{
 event.preventDefault();const session=getAgent(),body=$('agent-discussion-reply').value.trim(),submitted=$('agent-discussion-reply').value,threadID=$('agent-discussion-thread').value;
 if(!session?.parentSessionId||!body||discussionBusy)return;
 try{const next=clone(collaboration.threads),thread=next.find(t=>t.id===threadID&&t.documentId===session.documentId);if(!thread)throw Error('讨论已删除');thread.messages.push(makeMessage(body));await persistThreads(next);if(selectedAgent===session.id&&$('agent-discussion-reply').value===submitted){$('agent-discussion-reply').value='';agentReplyDrafts.delete(session.id);}else if(agentReplyDrafts.get(session.id)===submitted){agentReplyDrafts.delete(session.id);}renderAgentDiscussion(getAgent());invalidateAgentPreview();showError(null);}
 catch(error){showError(error);}
};
$('return-to-work').onclick=()=>{
 const session=getAgent(),threads=documentThreads(session);if(!threads.length||!session.parentSessionId)return;
 const messages=threads.flatMap(thread=>thread.messages.map(message=>({thread,message})));
 const defaultIDs=new Set(messages.filter(({message})=>message.intent==='decision').map(({message})=>message.id));if(!defaultIDs.size)for(const thread of threads)if(thread.messages.length)defaultIDs.add(thread.messages.at(-1).id);
 openDiscussionDialog('选择送回工作会话的结论与依据',threads.map(thread=>`<fieldset><legend>${esc((thread.anchor?.quote||thread.messages[0]?.body||thread.id).slice(0,60))}</legend>`+thread.messages.map(m=>`<label><input type="checkbox" data-return-message="${esc(m.id)}" ${defaultIDs.has(m.id)?'checked':''}> ${esc(m.author+' · '+m.body.slice(0,180))}</label>`).join('')+'</fieldset>').join('')+'<p>仅预填所选材料，不自动发送。</p>',async()=>{
  const ids=new Set(Array.from($('discussion-dialog-body').querySelectorAll('[data-return-message]:checked')).map(input=>input.dataset.returnMessage));if(!ids.size)throw Error('至少选择一条结论或依据');
  const text=`请结合以下讨论结论与依据处理项目任务。\n来源：${session.documentId}\n\n`+threads.filter(thread=>thread.messages.some(m=>ids.has(m.id))).map(thread=>`## 讨论 ${thread.id}${thread.anchor?`\n引用：${thread.anchor.quote}`:''}\n\n`+thread.messages.filter(m=>ids.has(m.id)).map(m=>`[${m.author} · ${m.intent}]\n${m.body}`).join('\n\n')).join('\n\n')+`\n\n`+replyReadabilityRules;
  selectAgent(session.parentSessionId);$('agent-message').value=($('agent-message').value.trim()?$('agent-message').value+'\n\n':'')+text;selectedThreads=[];$('include-current-document').checked=false;invalidateAgentPreview();renderAgentSessions();renderAgent();await previewAgentInput();
 },'预填到工作会话');
};
