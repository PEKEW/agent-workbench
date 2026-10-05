// 浏览器集成测试专用协议进程。不是 Codex，不执行真实模型请求。
import readline from 'node:readline';
const lines=readline.createInterface({input:process.stdin});let cwd='',turn='',count=0,pending=null,timer=null;
let threadId='fixture-thread-'+process.pid;
const send=value=>process.stdout.write(JSON.stringify(value)+'\n');
function finish(status='completed'){
  if(timer)clearTimeout(timer);timer=null;
  if(status==='completed'){
    send({method:'item/completed',params:{threadId,turnId:turn,item:{id:'command-'+turn,type:'commandExecution',command:'fixture command',cwd,aggregatedOutput:'协议测试输出',status:'completed',exitCode:0}}});
    send({method:'item/agentMessage/delta',params:{threadId,turnId:turn,itemId:'message-'+turn,delta:'协议测试回复'}});
    send({method:'item/completed',params:{threadId,turnId:turn,item:{id:'message-'+turn,type:'agentMessage',text:'协议测试回复 <img src=x onerror="window.agentExecuted=true">'}}});
  }
  send({method:'turn/completed',params:{threadId,turn:{id:turn,status}}});pending=null;
}
lines.on('line',line=>{
  const m=JSON.parse(line),p=m.params||{};
  if(m.method==='initialize'){send({id:m.id,result:{userAgent:'fixture'}});return;}
  if(m.method==='initialized')return;
  if(m.method==='thread/start'||m.method==='thread/resume'){
    if(p.threadId)threadId=p.threadId;cwd=p.cwd;send({id:m.id,result:{thread:{id:threadId,status:{type:'idle'}},model:'fixture-native',approvalPolicy:p.approvalPolicy,approvalsReviewer:'user',sandbox:{type:p.sandbox==='read-only'?'readOnly':'workspaceWrite'},cwd}});return;
  }
  if(m.method==='turn/start'){
    turn='turn-'+(++count);const text=p.input[0].text;
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
