import fs from 'node:fs';import vm from 'node:vm';import assert from 'node:assert/strict';
const source=fs.readFileSync(new URL('../internal/server/web/agent-timeline.js',import.meta.url),'utf8');
const context=vm.createContext({});vm.runInContext(source,context);
const event=(sequence,method,params)=>({sequence,method,params,createdAt:'2026-10-05T12:00:00Z'});
const events=[event(1,'workbench/inputAccepted',{input:{id:'C1',text:'task\n\n--- 选定文档 D ---\nbody'},queued:false}),event(2,'turn/started',{threadId:'T',turn:{id:'R1'}}),event(3,'item/completed',{threadId:'T',turnId:'R1',item:{id:'C1',type:'userMessage',content:[{text:'task'}]}}),event(4,'item/agentMessage/delta',{threadId:'T',turnId:'R1',itemId:'A1',delta:'hello'}),event(5,'item/completed',{threadId:'T',turnId:'R1',item:{id:'A1',type:'agentMessage',text:'**hello**'}}),event(6,'item/completed',{threadId:'T',turnId:'R1',item:{id:'cmd',type:'commandExecution',command:'ls'}}),event(7,'item/completed',{threadId:'foreign',turnId:'R1',item:{id:'wrong',type:'agentMessage',text:'wrong'}})];
for(let i=0;i<3100;i++)events.push(event(8+i,'item/completed',{threadId:'T',turnId:'R1',item:{id:'cmd'+i,type:'commandExecution',command:'ls'}}));
context.events=events;context.thread='T';const result=vm.runInContext('projectAgentTimeline(events,thread,false)',context);
assert.equal(result.conversation.length,2,'execution displaced conversation or user/live messages duplicated');
assert.equal(result.conversation[1].body,'**hello**');assert.equal(result.conversation[0].turnID,'R1');assert.equal(result.execution.length,100);
context.events=[event(1,'item/agentMessage/delta',{threadId:'T',turnId:'R',itemId:'A',delta:'first'}),event(2,'item/agentMessage/delta',{threadId:'T',turnId:'R',itemId:'A',delta:' second'})];
const live=vm.runInContext('projectAgentTimeline(events,thread,false)',context);assert.equal(live.conversation.length,1);assert.equal(live.conversation[0].body,'first second');
{
 const timeline=context.createAgentTimeline('T');
 timeline.append([event(900,'workbench/inputAccepted',{input:{id:'D1',text:'请回应\n\n--- 增量文档讨论 · PLAN.md · 2 条 ---\n=== 讨论 A · 批注 · 待处理 ==='}}),
  event(901,'item/completed',{threadId:'T',turnId:'X',item:{id:'P',type:'fileChange',status:'completed',changes:[{path:'/p/a.md',kind:{type:'update',move_path:null},diff:'@@ -1 +1 @@\n-a\n+b\n'},{path:'/p/b.md',kind:{type:'add'},diff:'new\n'}]}})]);
 const view=timeline.view(false),input=view.conversation.find(e=>e.inputID==='D1'),change=view.execution.find(e=>e.title.startsWith('文件修改'));
 assert.equal(input.body,'请回应','document discussion materials not folded into context');assert.match(input.context,/增量文档讨论/);
 assert.equal(change.title,'文件修改 · 已完成');assert.equal(change.body,'修改 /p/a.md\n新建 /p/b.md');assert.equal(change.changes.length,2);
 const diff=vm.createContext({});diff.self=diff;
 for(const file of ['diff.min.js','diff2html.min.js','diff-view.js'])vm.runInContext(fs.readFileSync(new URL('../internal/server/web/'+file,import.meta.url),'utf8'),diff);
 diff.changes=change.changes;const patch=vm.runInContext('fileChangePatch(changes,"/p")',diff);
 diff.patch=patch;const files=JSON.parse(vm.runInContext('JSON.stringify(Diff2Html.parse(patch).map(f=>({name:f.newName,added:f.addedLines,deleted:f.deletedLines,isNew:!!f.isNew})))',diff));
 assert.deepEqual(files,[{name:'a.md',added:1,deleted:1,isNew:false},{name:'b.md',added:1,deleted:0,isNew:true}],'file change patch not parsed as two files: '+patch);
}
console.log('PASS isolated timelines, input/live deduplication, per-timeline limits, foreign-thread filtering, file change diffs, document discussion folding');
