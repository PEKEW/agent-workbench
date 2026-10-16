'use strict';
// 差异由 jsdiff 生成统一格式，diff2html 渲染；diff2html 已转义正文，插入前仍经 DOMPurify 清理。
function renderUnifiedDiff(container,patch){
 container.classList.add('diff-view');
 try{
  const html=Diff2Html.html(patch,{drawFileList:false,outputFormat:'line-by-line',matching:'lines',colorScheme:'light'});
  container.innerHTML=DOMPurify.sanitize(html,{USE_PROFILES:{html:true,svg:true}});
 }catch{const pre=document.createElement('pre');pre.textContent=patch;container.replaceChildren(pre);}
}
function documentConflictPatch(path,disk,draft){
 return Diff.createTwoFilesPatch(path,path,disk,draft);
}
// Codex 的 fileChange 只给出不带文件头的 hunk（修改）或完整内容（新建、删除）；补齐 git 风格文件头，diff2html 据此标注新建、删除与移动。
function fileChangePatch(changes,cwd=''){
 const relative=path=>cwd&&String(path).startsWith(cwd+'/')?String(path).slice(cwd.length+1):String(path||'');
 const hunks=(before,after)=>{const patch=Diff.createTwoFilesPatch('a','b',before,after);const start=patch.indexOf('@@');return start<0?'':patch.slice(start);};
 return (Array.isArray(changes)?changes:[]).map(change=>{
  const path=relative(change.path),type=change.kind?.type,diff=String(change.diff||'');
  if(type==='add')return `diff --git a/${path} b/${path}\nnew file mode 100644\n--- /dev/null\n+++ b/${path}\n${hunks('',diff)}`;
  if(type==='delete')return `diff --git a/${path} b/${path}\ndeleted file mode 100644\n--- a/${path}\n+++ /dev/null\n${hunks(diff,'')}`;
  const target=change.kind?.move_path?relative(change.kind.move_path):path;
  return `diff --git a/${path} b/${target}\n${target!==path?`rename from ${path}\nrename to ${target}\n`:''}--- a/${path}\n+++ b/${target}\n${diff.endsWith('\n')?diff:diff+'\n'}`;
 }).join('');
}
