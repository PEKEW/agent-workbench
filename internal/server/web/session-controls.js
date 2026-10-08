'use strict';
let commandPolicy={generation:0,rules:[]};
const interactionSnapshots=new Map();
function sessionDeletable(s){return !!s&&!['starting','running','waiting'].includes(s.state)&&(!s.activeInput||['closed','disconnected'].includes(s.state))&&!s.activeTurnId&&!s.queue?.length&&!s.replies?.some(r=>!r.synced&&!r.discarded);}
function interactionNotice(session,text){
 const row=document.createElement('div');row.className='interaction-notice';const open=document.createElement('button');open.type='button';open.textContent=`${session.name} · ${text}`;open.onclick=()=>{if(!leaveEditor())return;selectAgent(session.id);switchWorkbench('agents');renderAgentSessions();renderAgent();};const close=document.createElement('button');close.type='button';close.textContent='×';close.setAttribute('aria-label','关闭提醒');close.onclick=()=>row.remove();row.append(open,close);$('agent-notices').append(row);if(text!=='需要你回应')setTimeout(()=>row.remove(),10000);while($('agent-notices').children.length>3)$('agent-notices').firstElementChild.remove();
}
function renderSessionInteractions(){
 $('open-command-authorizations').hidden=!project;
 const s=getAgent();$('delete-agent').disabled=!sessionDeletable(s);$('configure-agent-authorizations').disabled=!s;
 const activity=$('agent-activity');activity.hidden=!s;activity.dataset.state=s?.state||'idle';
 activity.textContent=agentStatusError?'状态未同步':s?.state==='running'?'执行中 · 等待 Agent 回复':s?.state==='starting'?'正在连接 Agent…':s?.state==='waiting'?(s.requests?.some(r=>r.method==='item/tool/requestUserInput')?'等待你的回答 · 请在上方回应问题':'需要你批准 · 请在上方处理请求'):s?agentStatusText(s):'';
 const waiting=agentSessions.filter(s=>s.state==='waiting').length,running=agentSessions.filter(s=>s.state==='running').length;
 document.title=`${waiting?`需要回应 ${waiting} · `:running?`执行中 ${running} · `:''}Agent Workbench`;
 for(const session of agentSessions){const previous=interactionSnapshots.get(session.id),current={turn:session.lastTurnId,status:session.lastTurnStatus,requests:(session.requests||[]).map(r=>r.id)};
 if(previous&&!agentStatusError){if(current.requests.some(id=>!previous.requests.includes(id)))interactionNotice(session,'需要你回应');if(current.turn&&(current.turn!==previous.turn||current.status!==previous.status)&&['completed','failed','interrupted'].includes(current.status))interactionNotice(session,current.status==='completed'?'回复已完成':current.status==='failed'?'执行失败':'回合已停止');}
 if(!agentStatusError)interactionSnapshots.set(session.id,current);
 }
 for(const id of interactionSnapshots.keys())if(!agentSessions.some(s=>s.id===id))interactionSnapshots.delete(id);
}
function renderCommandPolicy(){
 const list=$('authorization-rules');list.replaceChildren();if(!commandPolicy.rules.length){const p=document.createElement('p');p.textContent='尚未配置命令授权；原生审批继续由你处理。';list.append(p);}
 for(const rule of commandPolicy.rules){const row=document.createElement('div');row.className='authorization-rule';const text=document.createElement('span');text.textContent=`${rule.prefix.join(' ')} · ${rule.sessionId?(agentSessions.find(s=>s.id===rule.sessionId)?.name||'已移除会话'):'当前项目全部会话'}`;const remove=document.createElement('button');remove.type='button';remove.className='small-button';remove.textContent='撤销';remove.onclick=async()=>{try{commandPolicy=await api('authorizations',{generation:commandPolicy.generation,rules:commandPolicy.rules.filter(r=>r.id!==rule.id)});renderCommandPolicy();$('authorization-status').textContent='授权已撤销，后续原生审批重新检查。';}catch(error){$('authorization-status').textContent=error.message;}};row.append(text,remove);list.append(row);}
}
async function openCommandAuthorizations(){
 try{commandPolicy=await api('authorizations');const scope=$('authorization-scope');scope.replaceChildren();const projectOption=document.createElement('option');projectOption.value='';projectOption.textContent='当前项目全部工作台会话';scope.append(projectOption);for(const s of agentSessions){const option=document.createElement('option');option.value=s.id;option.textContent=s.name+(s.discussionId?` · ${s.documentId}`:'');scope.append(option);}scope.value=selectedAgent||'';$('authorization-status').textContent='';renderCommandPolicy();if(!$('authorization-dialog').open)$('authorization-dialog').showModal();}catch(error){showError(error);}
}
$('open-command-authorizations').onclick=openCommandAuthorizations;
$('configure-agent-authorizations').onclick=openCommandAuthorizations;
$('authorization-close').onclick=()=>$('authorization-dialog').close();
$('authorization-form').onsubmit=async event=>{event.preventDefault();const value=$('authorization-prefix').value.trim();if(!value||/["']/.test(value)){$('authorization-status').textContent='请按空格填写命令和参数，不使用引号，例如 go test';return;}const button=$('authorization-save');button.disabled=true;try{commandPolicy=await api('authorizations',{generation:commandPolicy.generation,rules:[...commandPolicy.rules,{id:crypto.randomUUID(),prefix:value.split(/\s+/),sessionId:$('authorization-scope').value}]});$('authorization-prefix').value='';renderCommandPolicy();$('authorization-status').textContent='已保存；匹配的待处理和后续原生命令审批自动允许本次执行。';}catch(error){$('authorization-status').textContent=error.message+'；输入已保留。';}finally{button.disabled=false;}};
$('delete-agent').onclick=async()=>{const s=getAgent();if(!sessionDeletable(s)||!confirm(`删除「${s.name}」的工作台会话和本地日志？${s.activeInput?'未确认的旧任务也会被放弃。':''}项目文档、讨论及 Codex 原生历史保留。`))return;try{await api(`agents/${encodeURIComponent(s.id)}/delete`,{abandonUnknown:!!s.activeInput});agentEventCache.delete(s.id);agentDrafts.delete(s.id);agentReplyDrafts.delete(s.id);await refreshAgents();showError(null);}catch(error){showError(error);}};
