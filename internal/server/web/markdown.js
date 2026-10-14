'use strict';
const markdownEscape=text=>String(text).replaceAll('&','&amp;').replaceAll('<','&lt;').replaceAll('>','&gt;');
marked.use({renderer:{html:item=>markdownEscape(item.text)}});
// 公式由 marked-katex-extension 解析，KaTeX 渲染失败时按红色文本显示原始公式；组件缺失时保持原有纯文本行为。
if(typeof markedKatex==='function'&&typeof katex!=='undefined')marked.use(markedKatex({throwOnError:false}));
// KaTeX 输出的标签与属性；原始 HTML 已在上方渲染器转义，不会借此进入文档。
const katexTags=['span','math','semantics','annotation','mrow','mi','mo','mn','mtext','mspace','msup','msub','msubsup','mfrac','msqrt','mroot','mover','munder','munderover','mtable','mtr','mtd','mlabeledtr','mstyle','mpadded','mphantom','menclose','svg','path','line'];
const katexAttributes=['class','style','aria-hidden','xmlns','encoding','mathvariant','displaystyle','scriptlevel','stretchy','separator','fence','accent','mathbackground','mathcolor','minsize','maxsize','width','height','depth','voffset','lspace','rspace','columnalign','columnspacing','columnlines','rowspacing','rowlines','viewBox','preserveAspectRatio','d','x1','y1','x2','y2','stroke-width'];
// 文档与消息共用安全渲染；导航由调用方提供，不读取当前文档全局变量。
function renderSafeMarkdown(container,text,{basePath='',paths=[],onNavigate=()=>{}}={}){
 try{
  container.innerHTML=DOMPurify.sanitize(marked.parse(String(text),{gfm:true}),{
   ALLOWED_TAGS:['p','h1','h2','h3','h4','h5','h6','blockquote','ul','ol','li','pre','code','strong','em','del','a','hr','br','table','thead','tbody','tr','th','td','sup','sub','details','summary',...katexTags],
   ALLOWED_ATTR:['href','title','start','align',...katexAttributes],ALLOW_DATA_ATTR:false
  });
  for(const heading of container.querySelectorAll('h1,h2,h3,h4,h5,h6'))heading.id=heading.textContent.trim().toLowerCase().replace(/\s+/g,'-').replace(/[^\p{L}\p{N}_-]/gu,'');
  for(const link of container.querySelectorAll('a[href]')){
   const href=link.getAttribute('href');
   try{
    if(href.startsWith('#')){link.addEventListener('click',event=>{event.preventDefault();const id=decodeURIComponent(href.slice(1));Array.from(container.querySelectorAll('[id]')).find(el=>el.id===id)?.scrollIntoView();});continue;}
    const destination=new URL(href,'https://project.invalid/'+basePath);
    if(!['http:','https:'].includes(destination.protocol))throw Error('unsafe protocol');
    if(destination.origin!=='https://project.invalid'){link.href=destination.href;link.target='_blank';link.rel='noopener noreferrer';continue;}
    let path=decodeURIComponent(destination.pathname.slice(1));
    if(href.startsWith('/')&&!paths.includes(path))path=paths.find(candidate=>href.endsWith('/'+candidate))||path;
    if(paths.includes(path)){link.addEventListener('click',event=>{event.preventDefault();onNavigate(path);});}
    else{link.removeAttribute('href');link.title='此链接不在当前阅读范围内';}
   }catch{link.removeAttribute('href');}
  }
 }catch{container.textContent=String(text);}
}
