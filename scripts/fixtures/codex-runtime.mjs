// 浏览器集成测试专用协议进程。不是 Codex，不执行真实模型请求。
import readline from 'node:readline';
import {randomUUID} from 'node:crypto';
const lines=readline.createInterface({input:process.stdin});let cwd='',turn='',pending=null,timer=null,inputText='';
let threadId='fixture-thread-'+process.pid;let flood=false,edit=false;let hookTrusted=false;let threadActive=false;let threadRound=0;
const send=value=>process.stdout.write(JSON.stringify(value)+'\n');
function finish(status='completed'){
  if(timer)clearTimeout(timer);timer=null;
  if(status==='completed'){
    if(edit)send({method:'item/completed',params:{threadId,turnId:turn,item:{id:'patch-'+turn,type:'fileChange',status:'completed',changes:[{path:cwd+'/docs/fixture.md',kind:{type:'update',move_path:null},diff:'@@ -1,2 +1,2 @@\n 标题\n-旧行 <script>window.diffExecuted=true</script>\n+新行\n'},{path:cwd+'/docs/new.md',kind:{type:'add'},diff:'新文件内容\n'}]}}});
    send({method:'item/completed',params:{threadId,turnId:turn,item:{id:'command-'+turn,type:'commandExecution',command:'fixture command',cwd,aggregatedOutput:'协议测试输出',status:'completed',exitCode:0}}});
    send({method:'item/agentMessage/delta',params:{threadId,turnId:turn,itemId:'message-'+turn,delta:'协议测试回复'}});
    const discussions=[...inputText.matchAll(/^=== 讨论 (\S+) /gm)].map(match=>match[1]);
    if(discussions.length>1){send({method:'item/completed',params:{threadId,turnId:turn,item:{id:'message-'+turn,type:'agentMessage',text:'逐条回应如下。\n\n'+discussions.map(id=>'### 讨论 '+id+'\n协议测试回复 '+id).join('\n\n'),phase:'final_answer'}}});}
    else send({method:'item/completed',params:{threadId,turnId:turn,item:{id:'message-'+turn,type:'agentMessage',text:'**协议测试回复**\n\n- 已读取材料\n\n`代码片段`\n\n<img src=x onerror="window.agentExecuted=true">\n\n[危险](javascript:alert(1))\n\n![远程图片](https://example.invalid/pixel.png)',phase:'final_answer'}}});
  }
  if(flood){for(let i=0;i<3100;i++)send({method:'item/completed',params:{threadId,turnId:turn,item:{id:'flood-'+i,type:'commandExecution',command:'fixture flood '+i,status:'completed',aggregatedOutput:'',exitCode:0}}});flood=false;}
  send({method:'turn/completed',params:{threadId,turn:{id:turn,status}}});pending=null;
}
lines.on('line',line=>{
  const m=JSON.parse(line),p=m.params||{};
  if(m.method==='initialize'){send({id:m.id,result:{userAgent:'fixture'}});return;}
  if(m.method==='thread/list'){send({id:m.id,result:{data:[{id:'fixture-thread-old',name:'旧原生会话',updatedAt:100},{id:'fixture-thread-'+process.pid,name:'Fixture 会话',updatedAt:200}],nextCursor:null}});return;}
  if(m.method==='initialized')return;
 if(m.method==='hooks/list'){send({id:m.id,result:{data:[{hooks:[{key:'fixture-memory-hook',currentHash:'fixture-hash',pluginId:'claude-mem@claude-mem-local',command:'fixture memory hook',enabled:true,eventName:'postToolUse',trustStatus:hookTrusted?'trusted':'untrusted',sourcePath:'/fixture/hooks.json'}],warnings:[]}]}});return;}
 if(m.method==='config/read'){send({id:m.id,result:{layers:[{name:{type:'user'},version:hookTrusted?'V2':'V1'}]}});return;}
 if(m.method==='config/batchWrite'){hookTrusted=true;send({id:m.id,result:{status:'ok'}});return;}
  if(m.method==='thread/start'||m.method==='thread/resume'){
    if(m.method==='thread/start'&&threadActive)threadId='fixture-thread-'+process.pid+'-round-'+(++threadRound);
    threadActive=true;
    if(p.threadId)threadId=p.threadId;cwd=p.cwd;send({id:m.id,result:{thread:{id:threadId,status:{type:'idle'}},model:'fixture-native',approvalPolicy:p.approvalPolicy,approvalsReviewer:'user',sandbox:{type:p.sandbox==='read-only'?'readOnly':'workspaceWrite'},cwd}});return;
  }
  if(m.method==='turn/start'){
    turn='turn-'+randomUUID();inputText=p.input[0].text;const text=inputText.split('\n\n--- ')[0];flood=text==='FLOOD';edit=text==='EDIT';
    send({method:'turn/started',params:{threadId,turn:{id:turn,status:'inProgress'}}});send({id:m.id,result:{turn:{id:turn,status:'inProgress'}}});
    if(text==='WAIT')return;
    if(text==='APPROVE'){
      pending='command-approval';send({id:pending,method:'item/commandExecution/requestApproval',params:{threadId,turnId:turn,itemId:'command-'+turn,command:'fixture approved command',cwd,reason:'测试原生请求路由'}});return;
    }
    if(text==='QUESTION'){
      pending='user-question';send({id:pending,method:'item/tool/requestUserInput',params:{threadId,turnId:turn,itemId:'question-'+turn,isBlocking:true,questions:[{id:'choice',header:'测试问题',question:'你选择哪一个？',options:[{label:'A',description:'测试选项'}]}]}});return;
    }
    if(text==='PERMISSIONS'){
      pending='permission-approval';send({id:pending,method:'item/permissions/requestApproval',params:{threadId,turnId:turn,itemId:'permission-'+turn,cwd,reason:'测试权限请求',permissions:{network:{enabled:true}}}});return;
    }
    timer=setTimeout(()=>finish(text==='FAIL'?'failed':'completed'),250);return;
  }
  if(m.method==='turn/interrupt'){send({id:m.id,result:{}});finish('interrupted');return;}
  if(m.method===undefined&&m.id===pending&&m.result){send({method:'serverRequest/resolved',params:{threadId,requestId:pending}});finish();return;}
  send({id:m.id,error:{code:-32601,message:'fixture method not found'}});
});
