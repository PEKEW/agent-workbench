'use strict';
const selectedDiscussionIDs=new Set();
let visibleDiscussionIDs=[];
let discussionBatch=null;
let batchBusy=false;
async function prepareDiscussionBatch(ids,parentId,text,onChange){
 const rows=ids.map(threadId=>({threadId,status:'pending'}));onChange(rows);
 for(const row of rows){
  row.status='preparing';onChange(rows);
  try{
   const session=await api(`agents/${encodeURIComponent(parentId)}/discussion`,{discussionId:row.threadId});row.sessionId=session.id;
   if(!['idle','running','waiting'].includes(session.state))throw Error('会话已断开，请到 Agent 会话中恢复后重新预览');
   row.input={sessionId:session.id,threadId:row.threadId,text,documents:[],full:false};
   row.preview=await api('agents/preview',row.input);row.status='ready';
  }catch(error){row.status='preview-error';row.error=error.message;}
  onChange(rows);
 }
 return rows;
}
async function sendDiscussionBatch(rows,onChange){
 for(const row of rows){
  if(row.status!=='ready')continue;
  row.status='sending';onChange(rows);
  try{row.result=await api(`agents/${encodeURIComponent(row.sessionId)}/input`,{preview:row.input,checksum:row.preview.checksum});row.status='sent';}
  catch(error){row.status='send-error';row.error=error.message;}
  onChange(rows);
 }
}
function updateBatchSelection(){
 const available=new Set(collaboration?.threads.map(t=>t.id)||[]);
 for(const id of selectedDiscussionIDs)if(!available.has(id))selectedDiscussionIDs.delete(id);
 $('batch-selection-count').textContent=`已选 ${selectedDiscussionIDs.size} 条（可跨文档）`;
 $('batch-open').disabled=discussionBusy||batchBusy||!selectedDiscussionIDs.size;
 $('batch-select-visible').disabled=discussionBusy||!visibleDiscussionIDs.length;
 $('batch-clear-selection').disabled=batchBusy||!selectedDiscussionIDs.size;
}
function batchRowStatus(row){
 if(row.status==='sent'){
  const session=agentSessions.find(s=>s.id===row.sessionId),context=session?.discussionContext;
  if(context?.inputId===row.result?.inputId&&session.replies?.some(r=>r.turnId===context.turnId&&r.synced))return '回复已写回';
  if(session)return `${row.result?.queued?'已排队':'已交付'} · ${agentStateLabels[session.state]||session.state}`;
 }
 const labels={pending:'等待预览',preparing:'正在准备',ready:'已预览',sending:'正在交付',sent:row.result?.queued?'已排队':'已交付', 'preview-error':'预览失败','send-error':'发送报错，请检查会话；本批次不会重发'};
 return labels[row.status]||row.status;
}
function renderBatchRows(rows){
 const container=$('batch-results'),expanded=new Set(Array.from(container.querySelectorAll('details[open]')).map(el=>el.dataset.thread));
 container.replaceChildren();
 for(const row of rows){
  const thread=collaboration?.threads.find(t=>t.id===row.threadId),section=document.createElement('section');section.className='batch-item';section.dataset.thread=row.threadId;
  const heading=document.createElement('h3');heading.textContent=thread?`${thread.documentId} · ${(thread.anchor?.quote||thread.messages[0]?.body||'讨论').slice(0,70)}`:row.threadId;
  const status=document.createElement('p');status.textContent=batchRowStatus(row);section.append(heading,status);
  if(row.error){const error=document.createElement('p');error.className='batch-error';error.textContent=row.error;section.append(error);}
  if(row.preview){const details=document.createElement('details'),summary=document.createElement('summary'),pre=document.createElement('pre');details.dataset.thread=row.threadId;details.open=expanded.has(row.threadId);summary.textContent=`查看实际输入 · ${row.preview.mode}讨论`;pre.textContent=row.preview.text;details.append(summary,pre);section.append(details);}
  if(row.sessionId){const button=document.createElement('button');button.type='button';button.className='small-button';button.textContent='查看会话与回复';button.disabled=batchBusy;button.onclick=()=>{if(batchBusy)return;$('batch-dialog').close();selectAgent(row.sessionId);switchWorkbench('agents');renderAgentSessions();renderAgent();refreshAgents().catch(showError);};section.append(button);}
  container.append(section);
 }
 $('batch-send').disabled=batchBusy||!rows.some(row=>row.status==='ready');
 $('batch-preview').disabled=batchBusy||rows.some(row=>['sent','sending','send-error'].includes(row.status));
 $('batch-summary').textContent=`${rows.length} 条 · ${rows.filter(r=>r.status==='ready').length} 条可发送 · ${rows.filter(r=>r.status==='sent').length} 条已交付`;
}
$('batch-select-visible').onclick=()=>{for(const id of visibleDiscussionIDs)selectedDiscussionIDs.add(id);renderThreads();};
$('batch-clear-selection').onclick=()=>{selectedDiscussionIDs.clear();renderThreads();};
$('batch-open').onclick=async()=>{
 try{
  if(!leaveEditor())return;
  await refreshAgents();const parents=agentSessions.filter(s=>!s.discussionId);
  if(!parents.length)throw Error('请先启动一个工作会话');
  const select=$('batch-parent');select.replaceChildren();for(const parent of parents){const option=document.createElement('option');option.value=parent.id;option.textContent=parent.name;select.append(option);}
  const preferred=getAgent()?.parentSessionId||selectedAgent;if(parents.some(s=>s.id===preferred))select.value=preferred;
  $('batch-task').value='逐条独立处理讨论中用户的最新要求。若要求实现或修改，请实际实施并验证；若要求讨论，请回应并说明依据。仅完成分析时应明确说明尚未实施。';
  discussionBatch={ids:Array.from(selectedDiscussionIDs),rows:[]};$('batch-error').hidden=true;$('batch-parent').disabled=false;$('batch-task').disabled=false;
  renderBatchRows(discussionBatch.ids.map(threadId=>({threadId,status:'pending'})));$('batch-dialog').showModal();
 }catch(error){showError(error);}
};
$('batch-preview').onclick=async()=>{
 if(batchBusy||!discussionBatch)return;
 const text=$('batch-task').value.trim(),parent=$('batch-parent').value;if(!text){$('batch-error').textContent='请输入任务要求';$('batch-error').hidden=false;return;}
 batchBusy=true;$('batch-close').disabled=true;$('batch-parent').disabled=true;$('batch-task').disabled=true;$('batch-error').hidden=true;
 try{discussionBatch.rows=await prepareDiscussionBatch(discussionBatch.ids,parent,text,renderBatchRows);}
 finally{batchBusy=false;$('batch-close').disabled=false;renderBatchRows(discussionBatch.rows);updateBatchSelection();}
};
$('batch-form').onsubmit=async event=>{
 event.preventDefault();if(batchBusy||!discussionBatch)return;
 batchBusy=true;$('batch-close').disabled=true;
 try{await sendDiscussionBatch(discussionBatch.rows,renderBatchRows);for(const row of discussionBatch.rows)if(row.status==='sent')selectedDiscussionIDs.delete(row.threadId);}
 finally{batchBusy=false;$('batch-close').disabled=false;renderBatchRows(discussionBatch.rows);renderThreads();refreshAgents().catch(showError);}
};
$('batch-close').onclick=()=>{if(!batchBusy)$('batch-dialog').close();};
$('batch-dialog').addEventListener('cancel',event=>{if(batchBusy)event.preventDefault();});
window.addEventListener('beforeunload',event=>{if(batchBusy){event.preventDefault();event.returnValue='';}});
