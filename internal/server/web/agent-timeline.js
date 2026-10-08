'use strict';
// 增量处理完整事件源；两条时间线各自保留 100 条，不依赖原始事件缓存长度。
function createAgentTimeline(threadID){
 const conversation=new Map(),execution=new Map(),raw=[],completedTurns=new Set();let sequence=0,activeTurn='',pending=[],current=null;
 const put=(map,key,value)=>{if(!map.has(key))map.set(key,value);return map.get(key);};
 const trim=map=>{while(map.size>100)map.delete(map.keys().next().value);};
 const split=text=>{const index=text.search(/\n\n--- (?:选定文档|完整讨论|增量讨论|手动转发讨论)/);return index<0?{body:text,context:''}:{body:text.slice(0,index),context:text.slice(index)};};
 function append(events){for(const event of events){
  if(event.sequence<=sequence)continue;sequence=event.sequence;
  const p=event.params||{},item=p.item||{},method=event.method,turnID=p.turnId||p.turn?.id||activeTurn;
  if(p.threadId&&p.threadId!==threadID)continue;
  raw.push({key:'raw:'+event.sequence,title:method,body:JSON.stringify(event,null,2),turnID,sequence:event.sequence,createdAt:event.createdAt});if(raw.length>100)raw.shift();
  const base={turnID,sequence:event.sequence,createdAt:event.createdAt,event};
  if(method==='workbench/inputAccepted'){
   const input=p.input||{},entry={...base,key:'input:'+input.id,title:'用户',role:'human',...split(input.text||''),inputID:input.id,turnID:'',status:p.queued?'queued':'sending'};
   put(conversation,entry.key,entry);pending.push(entry);continue;
  }
  if(method==='workbench/turnSubmitted'){current=pending.find(entry=>entry.inputID===p.inputId)||null;pending=pending.filter(entry=>entry!==current);continue;}
  if(method==='workbench/queueCleared'){for(const entry of pending)entry.status='cleared';pending=[];}
  if(method==='turn/started'&&(completedTurns.has(p.turn?.id)||activeTurn&&activeTurn!==p.turn?.id))continue;
  if(method==='turn/started'){
   activeTurn=p.turn?.id||'';current=current||pending.shift()||null;
   if(current){current.turnID=activeTurn;current.status='running';put(conversation,current.key,current);}
  }
  if(method==='item/completed'&&item.type==='userMessage'){
   const text=(item.content||[]).map(part=>part.text||'').join('\n');
   if(!Array.from(conversation.values()).some(entry=>entry.role==='human'&&entry.turnID===turnID))put(conversation,'native-user:'+turnID+':'+item.id,{...base,key:'native-user:'+turnID+':'+item.id,title:'用户',role:'human',...split(text)});
   continue;
  }
  if(method==='item/agentMessage/delta'||(method==='item/started'||method==='item/completed')&&item.type==='agentMessage'){
   const id=p.itemId||item.id;if(!id)continue;const key='agent:'+turnID+':'+id;
   const entry=put(conversation,key,{...base,key,title:'Agent',role:'agent',body:'',itemID:id,complete:false});
   if(method==='item/agentMessage/delta')entry.body+=p.delta||'';
   else if(method==='item/completed'){entry.body=item.text||'';entry.complete=true;entry.phase=item.phase||'';}
   else if(item.text)entry.body=item.text;
   continue;
  }
  if((method==='item/started'||method==='item/completed')&&item.type==='commandExecution'){
   const key='command:'+turnID+':'+item.id,entry=put(execution,key,{...base,key,title:'执行命令',body:''});
   entry.body=`${item.command||''}\n${item.cwd||''}\n\n${item.aggregatedOutput||''}${item.exitCode!=null?'\n退出码：'+item.exitCode:''}`;entry.status=item.status||'';entry.event=event;continue;
  }
  if(method==='item/commandExecution/outputDelta'){
   const key='command:'+turnID+':'+p.itemId,entry=put(execution,key,{...base,key,title:'命令输出',body:''});entry.body=(entry.body+(p.delta||'')).slice(-40000);continue;
  }
  if(method==='hook/started'||method==='hook/completed'){
   const run=p.run||{},key='hook:'+turnID+':'+run.id;const entry=put(execution,key,{...base,key,title:'原生钩子 · '+(run.eventName||''),body:''});
   entry.status=run.status||'running';entry.body=[run.statusMessage||'',`状态：${run.status||'执行中'}`,run.durationMs!=null?`用时：${run.durationMs} ms`:'',(run.entries||[]).length?`返回 ${(run.entries||[]).length} 条原生输出；详情见完整原始日志。`:''].filter(Boolean).join('\n');entry.event=event;continue;
  }
  if(method==='turn/completed'){
   if(turnID)completedTurns.add(turnID);
   if(current&&current.turnID===turnID)current.status=p.turn?.status||'completed';
   if(turnID===activeTurn){current=null;activeTurn='';}
  }
  if(method==='item/completed'||method==='turn/started'||method==='turn/completed'||method.startsWith('workbench/')||event.requestId){
   const key='event:'+event.sequence;
   let title=method,body='';
   if(method==='item/completed'){title=item.type==='fileChange'?'文件修改':'工具事件 · '+(item.type||method);body=JSON.stringify(item.changes||item,null,2);}
   if(method==='turn/started')title='回合开始';
   if(method==='turn/completed'){
   if(turnID)completedTurns.add(turnID);title='回合结束';body=({completed:'已完成',failed:'失败',interrupted:'已中断'}[p.turn?.status]||p.turn?.status||'');}
   if(method==='workbench/runtimeDiagnostic'){title='运行时诊断';body=p.text||'';}
   if(method==='workbench/runtimeExited'){title='原生进程退出';body=p.error||'';}
   if(method==='workbench/sessionConnected')title='Codex 会话已连接';
   if(method==='workbench/emptySessionRecreated'){title='空会话已重新连接';body=`${p.reason}\n原 thread：${p.previousThreadId}\n新 thread：${p.threadId}`;}
   if(method==='workbench/requestAnswerSubmitted')title='已提交原生请求回应';
   if(method==='workbench/inputAccepted')continue;
   put(execution,key,{...base,key,title,body});
  }
 }
 trim(conversation);trim(execution);
 }
 return {append,view:showRaw=>({conversation:Array.from(conversation.values()),execution:showRaw?raw.slice():Array.from(execution.values())})};
}
function projectAgentTimeline(events,threadID,showRaw){const timeline=createAgentTimeline(threadID);timeline.append(events);return timeline.view(showRaw);}
