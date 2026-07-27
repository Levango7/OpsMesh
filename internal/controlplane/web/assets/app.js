// ---------- 标签切换 ----------
function switchTab(name){
  ['home','ops','cmdb','deploy','flow','logs','alerts'].forEach(function(t){
    var p=document.getElementById('tab-'+t); if(p)p.classList.toggle('active', t===name);
    var b=document.getElementById('tab-'+t+'-btn'); if(b)b.classList.toggle('active', t===name);
  });
  if(name==='ops'){ pollTasks(); }
  if(name==='cmdb'){ loadCMDBTypes(); }
  if(name==='flow'){ loadFlows(); }
  if(name==='deploy'){ pollDeploys(); }
  if(name==='alerts'){ pollAlertsFull(); }
  if(name==='home'){ paintOverview(); }
}
function toggleGuide(){document.getElementById('guide-ops').classList.toggle('open');}

// ---------- 运维中枢（原有） ----------
var lastDevices={}, lastTasks=[], lastAlerts=[];
function paintStats(){
  var devs=lastDevices||{}; var total=0, managed=0;
  Object.keys(devs).forEach(function(seg){(devs[seg]||[]).forEach(function(d){total++; if(d.state==='managed'||d.agentID)managed++;});});
  var sd=document.getElementById('statDevices'); if(sd)sd.textContent=total;
  var sm=document.getElementById('statManaged'); if(sm)sm.textContent=managed;
  var st=document.getElementById('statTasks'); if(st)st.textContent=(lastTasks||[]).length;
  var sa=document.getElementById('statAlerts'); if(sa)sa.textContent=(lastAlerts||[]).length;
  paintOverview();
}
function paintOverview(){
  var devs=lastDevices||{}, total=0, managed=0;
  Object.keys(devs).forEach(function(seg){(devs[seg]||[]).forEach(function(d){total++; if(d.state==='managed'||d.agentID)managed++;});});
  setText('ovDevices', total);
  var tasks=lastTasks||[], td=0, tf=0;
  tasks.forEach(function(t){ if(t.state==='done')td++; else if(t.state==='failed')tf++; });
  setText('ovTasks', tasks.length);
  var alerts=lastAlerts||[], ac=0, aw=0;
  alerts.forEach(function(a){ if(a.severity==='critical')ac++; else if(a.severity==='warning')aw++; });
  setText('ovAlerts', alerts.length);
  drawTaskBar('ovTaskChart', td, tf, tasks.length-td-tf);
  drawAlertDonut('ovAlertChart', ac, aw);
  paintTrend();
  paintTopo();
  // 配置项：拉各类型 CI 计数汇总
  fetch('/api/v1/cmdb/types').then(function(r){return r.json();}).then(function(ts){
    var list=(ts||[]);
    if(list.length===0){ setText('ovCIs', 0); return; }
    Promise.all(list.map(function(t){
      return fetch('/api/v1/cmdb/ci?type='+encodeURIComponent(t.type)).then(function(r){return r.json();}).then(function(arr){return (arr||[]).length;});
    })).then(function(ns){ var s=ns.reduce(function(a,b){return a+b;},0); setText('ovCIs', s); }).catch(function(e){console.error(e);});
  }).catch(function(e){console.error(e);});
}
function setText(id,v){ var e=document.getElementById(id); if(e)e.textContent=v; }
function drawTaskBar(elId, done, failed, other){
  var el=document.getElementById(elId); if(!el)return;
  var total=done+failed+other;
  if(total===0){ el.innerHTML='<p class="muted">暂无任务数据。</p>'; return; }
  var pct=function(n){ return (n/total*100).toFixed(1); };
  el.innerHTML=
    '<div style="display:flex;height:22px;border-radius:6px;overflow:hidden;margin:6px 0 10px">'
    +'<div style="width:'+pct(done)+'%;background:var(--green)" title="成功 '+done+'"></div>'
    +'<div style="width:'+pct(failed)+'%;background:var(--fail)" title="失败 '+failed+'"></div>'
    +'<div style="width:'+pct(other)+'%;background:var(--border-2)" title="进行中/排队 '+other+'"></div>'
    +'</div>'
    +'<div style="display:flex;gap:16px;font-size:12px;color:var(--text-2)">'
    +'<span><span class="dot ok" style="display:inline-block;margin-right:5px"></span>成功 '+done+'</span>'
    +'<span><span class="dot fail" style="display:inline-block;margin-right:5px"></span>失败 '+failed+'</span>'
    +'<span><span class="dot" style="display:inline-block;background:var(--border-2);margin-right:5px"></span>其余 '+other+'</span>'
    +'</div>';
}
function drawAlertDonut(elId, c, w){
  var el=document.getElementById(elId); if(!el)return;
  var total=c+w;
  if(total===0){ el.innerHTML='<p class="muted">无活跃告警，一切正常 ✅</p>'; return; }
  var R=42, C=2*Math.PI*R, critLen=c/total*C, warnLen=w/total*C;
  el.innerHTML=
    '<svg width="120" height="120" viewBox="0 0 120 120">'
    +'<circle cx="60" cy="60" r="'+R+'" fill="none" stroke="var(--fail)" stroke-width="14" stroke-dasharray="'+critLen+' '+(C-critLen)+'" stroke-dashoffset="0" transform="rotate(-90 60 60)"></circle>'
    +'<circle cx="60" cy="60" r="'+R+'" fill="none" stroke="var(--warn)" stroke-width="14" stroke-dasharray="'+warnLen+' '+C+'" stroke-dashoffset="'+(-critLen)+'" transform="rotate(-90 60 60)"></circle>'
    +'<text x="60" y="58" text-anchor="middle" font-size="20" font-weight="600" fill="var(--text)">'+total+'</text>'
    +'<text x="60" y="74" text-anchor="middle" font-size="11" fill="var(--text-3)">活跃告警</text>'
    +'</svg>'
    +'<div style="font-size:12px;color:var(--text-2);margin-top:6px"><span style="color:var(--fail);font-weight:600">严重 '+c+'</span> ｜ <span style="color:var(--warn);font-weight:600">警告 '+w+'</span></div>';
}
function loadAgents(){
  fetch('/api/v1/agents').then(function(r){return r.json();}).then(function(a){
    var sel=document.getElementById('agentID');sel.innerHTML='';
    (a||[]).forEach(function(x){var o=document.createElement('option');o.value=x.agentID;o.textContent=x.agentID+' ('+x.hostname+')';sel.appendChild(o);});
  }).catch(function(e){console.error(e);});
}
function esc(s){return (s==null?'':String(s)).replace(/[&<>]/g,function(c){return {'&':'&amp;','<':'&lt;','>':'&gt;'}[c];});}
function renderDevices(snap){
  lastDevices=snap||{};
  var html='';var keys=Object.keys(snap||{});
  if(keys.length===0){document.getElementById('devices').innerHTML='<p class="muted">暂无纳管设备。设备接入网段后会被自动发现并纳管。</p>';paintStats();return;}
  keys.forEach(function(seg){
    var devs=snap[seg];
    html+='<h3>网段 '+esc(seg)+'（'+devs.length+' 台设备）</h3>';
    html+='<table><tr><th>DeviceID</th><th>IP</th><th>采集端</th><th>状态</th><th>任务态</th><th>LastResult</th></tr>';
    devs.forEach(function(d){
      var rowCls='device'+(d.lastResult==='failed'?' fail':'');
      var badge='';
      if(d.lastResult==='failed'){badge='<span class="badge fail">failed</span>';}
      else if(d.lastResult==='success'){badge='<span class="badge ok">ok</span>';}
      html+='<tr class="'+rowCls+'" onclick="setFocus(\''+esc(d.deviceID)+'\',\''+esc(d.ip)+'\',\''+esc(d.agentID)+'\',\''+esc(seg)+'\');openDevice(\''+esc(d.deviceID)+'\')"><td><code>'+esc(d.deviceID)+'</code></td><td>'+esc(d.ip)+'</td><td>'+esc(d.agentID)+'</td><td>'+esc(d.state)+'</td><td>'+esc(d.taskState)+'</td><td>'+badge+'</td></tr>';
    });
    html+='</table>';
  });
  document.getElementById('devices').innerHTML=html;
  paintStats();
}
function renderAlerts(list){
  lastAlerts=list||[];
  if(!list||list.length===0){document.getElementById('alerts').innerHTML='<p class="muted">暂无告警，一切正常 ✅</p>';paintStats();return;}
  var fl=applyFocus(lastAlerts,'alert');
  var note=focusDevice?'<p class="hint">🔗 已按设备 <code>'+esc(focusDevice.id)+'</code> 过滤（'+fl.length+' 条）</p>':'';
  if(fl.length===0){document.getElementById('alerts').innerHTML=note+'<p class="muted">该设备暂无告警。</p>';paintStats();return;}
  var html=note;
  fl.forEach(function(a){
    var cls=a.severity==='critical'?'alert':'alert warn';
    html+='<div class="'+cls+'"><b>['+esc(a.severity)+']</b> 设备 '+esc(a.deviceID)+'<br>'+esc(a.message)+'<br><small class="muted">'+esc(a.createdAt)+'</small>'
      +'<br><button class="jbtn" style="margin-top:6px" onclick="setFocus(\''+esc(a.deviceID)+'\',\'\',\'\',\'\');switchTab(\'alerts\')">🔗 上下文串联</button></div>';
  });
  document.getElementById('alerts').innerHTML=html;
  paintStats();
}
function renderTasks(tasks){
  lastTasks=tasks||[];
  if(!tasks||tasks.length===0){document.getElementById('tasks').innerHTML='<p class="muted">暂无任务。在上方「下发任务」创建一条吧。</p>';paintStats();return;}
  var list=applyFocus(lastTasks,'task');
  var note=focusDevice?'<p class="hint">🔗 已按设备 <code>'+esc(focusDevice.id)+'</code> 过滤（'+list.length+' 条）</p>':'';
  if(list.length===0){document.getElementById('tasks').innerHTML=note+'<p class="muted">该设备暂无任务。</p>';paintStats();return;}
  var html=note+'<table><tr><th>TaskID</th><th>采集端</th><th>类型</th><th>命令</th><th>状态</th></tr>';
  list.forEach(function(t){
    html+='<tr><td><code>'+esc(t.taskID)+'</code></td><td>'+esc(t.agentID)+'</td><td>'+esc(t.type)+'</td><td><code>'+esc(t.command)+'</code></td><td>'+esc(t.status)+'</td></tr>';
  });
  html+='</table>';
  document.getElementById('tasks').innerHTML=html;
  paintStats();
}
function pollDevices(){fetch('/api/v1/devices').then(function(r){return r.json();}).then(renderDevices).catch(function(e){console.error(e);});}
function pollAlerts(){fetch('/api/v1/alerts').then(function(r){return r.json();}).then(renderAlerts).catch(function(e){console.error(e);});}
function pollTasks(){
  var st=document.getElementById('statusFilter').value;
  fetch('/api/v1/tasks'+(st?'?status='+encodeURIComponent(st):'')).then(function(r){return r.json();}).then(renderTasks).catch(function(e){console.error(e);});
}
function openDevice(id){
  fetch('/api/v1/devices/'+encodeURIComponent(id)).then(function(r){return r.json();}).then(function(d){
    var dev=d.device||{};
    var h='<h3>设备 '+esc(dev.deviceID)+'</h3>';
    h+='<p>IP: '+esc(dev.ip)+' ｜ 采集端: '+esc(dev.agentID)+' ｜ 租户: '+esc(dev.tenantID)+'</p>';
    h+='<p>状态: '+esc(dev.state)+' ｜ 任务态: '+esc(dev.taskState)+'</p>';
    if(dev.lastResult){
      var c=dev.lastResult==='failed'?'warn':'ok';
      h+='<p class="msg '+c+'">LastResult: '+esc(dev.lastResult)+' @ '+esc(dev.lastResultAt)+'</p>';
    }
    if(dev.state==='discovered'){
      h+='<button onclick="provision(\''+esc(dev.deviceID)+'\')">推送 Agent 纳管（B1）</button> ';
    }
    h+='<h4>任务</h4><table><tr><th>ID</th><th>类型</th><th>状态</th></tr>';
    (d.tasks||[]).forEach(function(t){h+='<tr><td><code>'+esc(t.taskID)+'</code></td><td>'+esc(t.type)+'</td><td>'+esc(t.status)+'</td></tr>';});
    h+='</table>';
    h+='<h4>最近结果</h4><table><tr><th>任务</th><th>退出码</th><th>输出</th></tr>';
    (d.results||[]).slice(0,5).forEach(function(r){h+='<tr><td><code>'+esc(r.taskID)+'</code></td><td>'+esc(r.exitCode)+'</td><td><code>'+esc(r.stdout)+'</code></td></tr>';});
    h+='</table>';
    document.getElementById('drawerBody').innerHTML=h;
    document.getElementById('drawer').classList.add('open');
  }).catch(function(e){console.error(e);});
}
function provision(id){
  fetch('/api/v1/devices/'+encodeURIComponent(id)+'/provision',{method:'POST'}).then(function(r){return r.json().then(function(j){return {s:r.status,j:j};});}).then(function(x){document.getElementById('drawerBody').insertAdjacentHTML('beforeend','<p class="msg ok">['+x.s+'] '+esc(JSON.stringify(x.j))+'</p>');pollDevices();}).catch(function(e){console.error(e);});
}
function closeDrawer(){document.getElementById('drawer').classList.remove('open');}

// ---------- CMDB ----------
function loadCMDBTypes(){
  fetch('/api/v1/cmdb/types').then(function(r){return r.json();}).then(function(ts){
    var ft=document.getElementById('ciTypeFilter'), nt=document.getElementById('ciTypeNew'), tt=document.getElementById('tmplTypeFilter');
    [ft,nt,tt].forEach(function(sel){sel.innerHTML='<option value="">（先选一个类型）</option>';});
    (ts||[]).forEach(function(t){
      [ft,nt,tt].forEach(function(sel){var o=document.createElement('option');o.value=t.name;o.textContent=t.displayName+' ('+t.name+')';sel.appendChild(o);});
    });
    ft.addEventListener('change', pollCIs);
    tt.addEventListener('change', pollTemplates);
  }).catch(function(e){console.error(e);});
}
function pollCIs(){
  var t=document.getElementById('ciTypeFilter').value;
  if(!t){document.getElementById('ciList').innerHTML='<p class="muted">请先选择一个类型</p>';return;}
  fetch('/api/v1/cmdb/ci?type='+encodeURIComponent(t)).then(function(r){return r.json();}).then(function(list){
    if(!list||list.length===0){document.getElementById('ciList').innerHTML='<p class="muted">该类型暂无配置项</p>';return;}
    var html='<table><tr><th>ID</th><th>名称</th><th>状态</th><th>来源</th><th>版本</th></tr>';
    list.forEach(function(c){
      html+='<tr class="ci" onclick="openCI(\''+esc(c.id)+'\')"><td><code>'+esc(c.id)+'</code></td><td>'+esc(c.name)+'</td><td>'+esc(c.status)+'</td><td>'+esc(c.source)+'</td><td>'+esc(c.version)+'</td></tr>';
    });
    html+='</table>';
    document.getElementById('ciList').innerHTML=html;
  }).catch(function(e){console.error(e);});
}
function pollTemplates(){
  var t=document.getElementById('tmplTypeFilter').value;
  if(!t){document.getElementById('tmplList').innerHTML='<p class="muted">请先选择一个类型</p>';return;}
  fetch('/api/v1/cmdb/attr-templates?type='+encodeURIComponent(t)).then(function(r){return r.json();}).then(function(list){
    if(!list||list.length===0){document.getElementById('tmplList').innerHTML='<p class="muted">该类型暂无属性模板</p>';return;}
    var html='<table><tr><th>Key</th><th>标签</th><th>类型</th><th>必填</th></tr>';
    list.forEach(function(x){
      html+='<tr><td><code>'+esc(x.attrKey)+'</code></td><td>'+esc(x.label)+'</td><td>'+esc(x.attrType)+'</td><td>'+(x.required?'是':'否')+'</td></tr>';
    });
    html+='</table>';
    document.getElementById('tmplList').innerHTML=html;
  }).catch(function(e){console.error(e);});
}
function openCI(id){
  fetch('/api/v1/cmdb/ci/'+encodeURIComponent(id)+'/graph').then(function(r){return r.json();}).then(function(g){
    if(g.error){document.getElementById('ciDetail').innerHTML='<p class="msg err">'+esc(g.error)+'</p>';return;}
    var c=g.centerCI||{};
    var h='<h4>'+esc(c.name)+' <small class="muted">('+esc(c.ciType)+' / '+esc(c.id)+')</small></h4>';
    h+='<p>状态: '+esc(c.status)+' ｜ 来源: '+esc(c.source)+' ｜ 版本: '+esc(c.version)+'</p>';
    if(c.attrs&&Object.keys(c.attrs).length){
      h+='<p>属性: '+Object.keys(c.attrs).map(function(k){return '<code>'+esc(k)+'</code>='+esc(c.attrs[k]);}).join('，')+'</p>';
    }
    h+='<h4>关系拓扑（'+(g.relations?g.relations.length:0)+'）</h4>';
    if(!g.relations||g.relations.length===0){h+='<p class="muted">无关系</p>';}
    else {
      g.relations.forEach(function(r){
        h+='<div class="rel"><b>'+esc(r.relationType)+'</b> → '+esc(r.targetName)+' <small class="muted">('+esc(r.targetType)+')</small></div>';
      });
    }
    document.getElementById('ciDetail').innerHTML=h;
  }).catch(function(e){console.error(e);});
}
function cmdbMsg(s,ok){var el=document.getElementById('cmdbMsg');el.className='msg '+(ok?'ok':'err');el.textContent=(ok?'[ok] ':'[err] ')+s;}

document.getElementById('ciForm').addEventListener('submit',function(e){
  e.preventDefault();
  var type=document.getElementById('ciTypeNew').value;
  var attrs={};
  var raw=document.getElementById('ciAttrs').value.trim();
  if(raw){try{attrs=JSON.parse(raw);}catch(err){cmdbMsg('属性 JSON 解析失败: '+err,false);return;}}
  var body={ciType:type,name:document.getElementById('ciName').value,attrs:attrs};
  fetch('/api/v1/cmdb/ci',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)})
    .then(function(r){return r.json().then(function(j){return {s:r.status,j:j};});})
    .then(function(x){cmdbMsg('['+x.s+'] '+JSON.stringify(x.j),x.s<400);pollCIs();})
    .catch(function(err){cmdbMsg('error: '+err,false);});
});

// ---------- 作业编排（M5 DAG 画布） ----------
var SVGNS='http://www.w3.org/2000/svg';
var flow={id:0,name:'',agentID:'',cron:'',dag:[],status:''};
var nodePos={}, selectedNode=null, linking=false, linkSrc=null, nodeStatus={};
var selectedEdge=null;            // {src,dst} 当前选中的依赖边
var view={scale:1,tx:0,ty:0};  // 画布视口变换（缩放/平移）
var history=[], future=[];       // 撤销 / 重做 栈
var panning=null, linkDrag=null;  // 平移 / 拖拽连线 临时态
var GRID=20;                     // 网格吸附粒度
var TYPE_COLOR={'shell':'#6366f1','file':'#0d9488','service':'#d97706'};
var TYPE_SOFT ={'shell':'#eceaff','file':'#d8f3ef','service':'#fef3e2'};
function snap(v){return Math.round(v/GRID)*GRID;}
function snapshot(){history.push(JSON.stringify({dag:flow.dag,pos:nodePos,sel:selectedNode,edge:selectedEdge}));if(history.length>60)history.shift();future=[];}
function undo(){if(!history.length)return;future.push(JSON.stringify({dag:flow.dag,pos:nodePos,sel:selectedNode,edge:selectedEdge}));var s=JSON.parse(history.pop());flow.dag=s.dag;nodePos=s.pos;selectedNode=s.sel;selectedEdge=s.edge;renderFlow();flowMsg('已撤销',true);}
function redo(){if(!future.length)return;history.push(JSON.stringify({dag:flow.dag,pos:nodePos,sel:selectedNode,edge:selectedEdge}));var s=JSON.parse(future.pop());flow.dag=s.dag;nodePos=s.pos;selectedNode=s.sel;selectedEdge=s.edge;renderFlow();flowMsg('已重做',true);}
function flowMsg(s,ok){var el=document.getElementById('wfMsg');if(el){el.className='msg '+(ok?'ok':'err');el.textContent=(ok?'[ok] ':'[err] ')+s;}}
function loadFlows(){
  fetch('/api/v1/agents').then(function(r){return r.json();}).then(function(a){
    var sel=document.getElementById('wfAgent');if(!sel)return;sel.innerHTML='';
    (a||[]).forEach(function(x){var o=document.createElement('option');o.value=x.agentID;o.textContent=x.agentID+' ('+x.hostname+')';sel.appendChild(o);});
  }).catch(function(e){console.error(e);});
  fetch('/api/v1/workflows').then(function(r){return r.json();}).then(function(list){
    var sel=document.getElementById('wfSelect');if(!sel)return;sel.innerHTML='<option value="">（新建空白作业流）</option>';
    (list||[]).forEach(function(w){var o=document.createElement('option');o.value=w.id;o.textContent='#'+w.id+' '+w.name+' ['+w.status+']';sel.appendChild(o);});
  }).catch(function(e){console.error(e);});
}
function openWorkflow(){
  var id=document.getElementById('wfSelect').value;
  if(!id){newWorkflow();return;}
  fetch('/api/v1/workflows/'+encodeURIComponent(id)).then(function(r){return r.json();}).then(function(w){
    if(w.error){flowMsg(w.error,false);return;}
    flow={id:w.id,name:w.name,agentID:w.agentID,cron:w.cron||'',dag:[],status:w.status};
    try{flow.dag=w.dag?JSON.parse(w.dag):[];}catch(e){flow.dag=[];}
    document.getElementById('wfName').value=w.name||'';
    document.getElementById('wfCron').value=w.cron||'';
    var asel=document.getElementById('wfAgent');
    if(w.agentID){for(var i=0;i<asel.options.length;i++){if(asel.options[i].value===w.agentID){asel.selectedIndex=i;break;}}}
    nodePos={};autoLayout();renderFlow();
  }).catch(function(e){console.error(e);});
}
function newWorkflow(){
  flow={id:0,name:'',agentID:document.getElementById('wfAgent').value,cron:'',dag:[],status:'draft'};
  document.getElementById('wfName').value='';
  document.getElementById('wfCron').value='';
  nodePos={};renderFlow();flowMsg('已新建空白作业流，添加步骤后保存',true);
}
function loadDemo(){
  flow={id:0,name:'示例-nginx发布',agentID:document.getElementById('wfAgent').value,cron:'',dag:[
    {id:'n1',name:'拉取镜像',type:'shell',command:'docker pull nginx:latest',path:'',dependsOn:[]},
    {id:'n2',name:'停旧容器',type:'shell',command:'docker stop nginx',path:'',dependsOn:['n1']},
    {id:'n3',name:'起新容器',type:'service',command:'nginx',path:'',dependsOn:['n2']}
  ],status:'draft'};
  document.getElementById('wfName').value=flow.name;
  document.getElementById('wfCron').value='';
  nodePos={};autoLayout();renderFlow();flowMsg('已载入示例作业流（尚未保存，可改动后点「💾 保存」）',true);
}
function addNode(){
  var id='n'+(flow.dag.length+1);
  while(flow.dag.some(function(n){return n.id===id;})){id='n'+Math.floor(Math.random()*1000);}
  snapshot();
  flow.dag.push({id:id,name:'步骤'+id,type:'shell',command:'',path:'',dependsOn:[]});
  nodePos[id]={x:snap(60+Math.random()*40),y:snap(60+flow.dag.length*70)};
  selectNode(id);renderFlow();
}
function deleteNode(id){
  snapshot();
  flow.dag=flow.dag.filter(function(n){return n.id!==id;});
  flow.dag.forEach(function(n){n.dependsOn=(n.dependsOn||[]).filter(function(d){return d!==id;});});
  delete nodePos[id];if(selectedNode===id)selectedNode=null;if(selectedEdge&&selectedEdge.src===id)selectedEdge=null;renderFlow();
}
function selectNode(id){selectedNode=id;renderFlow();}
function toggleLink(){
  linking=!linking;linkSrc=null;
  var b=document.getElementById('linkBtn');if(b)b.textContent=linking?'🔗 连线中…(点两节点)':'🔗 连线';
  var p=document.getElementById('tab-flow');if(p)p.classList.toggle('linkmode',linking);
}
function createsCycle(src,dst){
  var adj={};flow.dag.forEach(function(n){adj[n.id]=n.dependsOn||[];});
  var seen={},stack=[src];
  while(stack.length){var c=stack.pop();if(c===dst)return true;if(seen[c])continue;seen[c]=1;(adj[c]||[]).forEach(function(x){stack.push(x);});}
  return false;
}
function autoLayout(){
  snapshot();
  var indeg={},adj={};
  flow.dag.forEach(function(n){indeg[n.id]=0;adj[n.id]=[];});
  flow.dag.forEach(function(n){(n.dependsOn||[]).forEach(function(d){if(indeg[d]!==undefined){indeg[d]++;adj[n.id].push(d);}});});
  var level={},q=[];
  flow.dag.forEach(function(n){if(indeg[n.id]===0){level[n.id]=0;q.push(n.id);}});
  while(q.length){var cur=q.shift();(adj[cur]||[]).forEach(function(p){if(level[p]===undefined||level[cur]+1>level[p])level[p]=level[cur]+1;indeg[p]--;if(indeg[p]===0)q.push(p);});}
  flow.dag.forEach(function(n){if(level[n.id]===undefined)level[n.id]=0;});
  var per={};
  flow.dag.forEach(function(n){var L=level[n.id];per[L]=per[L]||0;var idx=per[L]++;nodePos[n.id]={x:snap(60+L*230),y:snap(50+idx*100)};});
  renderFlow();
}
function renderFlow(){
  var svg=document.getElementById('canvas');if(!svg)return;
  while(svg.firstChild)svg.removeChild(svg.firstChild);
  var W=170,H=66;
  // defs：网格 + 箭头
  var defs=document.createElementNS(SVGNS,'defs');
  var pat=document.createElementNS(SVGNS,'pattern');
  pat.setAttribute('id','grid');pat.setAttribute('width','26');pat.setAttribute('height','26');pat.setAttribute('patternUnits','userSpaceOnUse');
  var pc=document.createElementNS(SVGNS,'circle');pc.setAttribute('cx','2');pc.setAttribute('cy','2');pc.setAttribute('r','1');pc.setAttribute('fill','#dbe2f1');
  pat.appendChild(pc);defs.appendChild(pat);
  var mk=document.createElementNS(SVGNS,'marker');
  mk.setAttribute('id','arrow');mk.setAttribute('markerWidth','10');mk.setAttribute('markerHeight','10');
  mk.setAttribute('refX','8');mk.setAttribute('refY','3');mk.setAttribute('orient','auto');mk.setAttribute('markerUnits','strokeWidth');
  var mp=document.createElementNS(SVGNS,'path');mp.setAttribute('d','M0,0 L8,3 L0,6 Z');mp.setAttribute('fill','#94a3b8');
  mk.appendChild(mp);defs.appendChild(mk);svg.appendChild(defs);
  // 视口组（承载缩放/平移变换）
  var vp=document.createElementNS(SVGNS,'g');vp.setAttribute('id','viewport');
  vp.setAttribute('transform','translate('+view.tx+','+view.ty+') scale('+view.scale+')');
  svg.appendChild(vp);
  // 背景网格（随视口移动/缩放）
  var bg=document.createElementNS(SVGNS,'rect');bg.setAttribute('x',-3000);bg.setAttribute('y',-3000);bg.setAttribute('width',8000);bg.setAttribute('height',8000);bg.setAttribute('fill','url(#grid)');bg.setAttribute('class','canvasbg');
  vp.appendChild(bg);
  // 边（先画，节点覆盖其上便于拖拽）
  var EG=document.createElementNS(SVGNS,'g');
  flow.dag.forEach(function(n){
    (n.dependsOn||[]).forEach(function(d){
      var s=nodePos[d],t=nodePos[n.id];if(!s||!t)return;
      var x1=s.x+W/2,y1=s.y+H,x2=t.x+W/2,y2=t.y;
      var sel=(selectedEdge&&selectedEdge.src===d&&selectedEdge.dst===n.id);
      var ln=document.createElementNS(SVGNS,'line');
      ln.setAttribute('class','edge'+(sel?' sel':''));
      ln.setAttribute('x1',x1);ln.setAttribute('y1',y1);ln.setAttribute('x2',x2);ln.setAttribute('y2',y2);
      ln.setAttribute('marker-end','url(#arrow)');
      EG.appendChild(ln);
      var hit=document.createElementNS(SVGNS,'line');
      hit.setAttribute('class','edge-hit');hit.setAttribute('x1',x1);hit.setAttribute('y1',y1);hit.setAttribute('x2',x2);hit.setAttribute('y2',y2);
      hit.addEventListener('click',function(ev){ev.stopPropagation();if(linking)return;selectEdge(d,n.id);});
      EG.appendChild(hit);
    });
  });
  vp.appendChild(EG);
  // 拖拽连线预览线
  if(linkDrag&&linkDrag.from){var p0=nodePos[linkDrag.from];if(p0){var pv=document.createElementNS(SVGNS,'line');pv.setAttribute('class','edgeprev');pv.setAttribute('x1',p0.x+W);pv.setAttribute('y1',p0.y+H/2);pv.setAttribute('x2',linkDrag.x);pv.setAttribute('y2',linkDrag.y);vp.appendChild(pv);}}
  // 节点卡片
  var NG=document.createElementNS(SVGNS,'g');
  flow.dag.forEach(function(n){
    var p=nodePos[n.id]||{x:60,y:60};
    var cls='node'+(selectedNode===n.id?' sel':'')+(nodeStatus[n.id]==='running'?' run':'')+(nodeStatus[n.id]==='failed'?' fail':'');
    var g=document.createElementNS(SVGNS,'g');
    g.setAttribute('class',cls);
    g.setAttribute('transform','translate('+p.x+','+p.y+')');
    var rc=document.createElementNS(SVGNS,'rect');
    rc.setAttribute('class','card');rc.setAttribute('width',W);rc.setAttribute('height',H);rc.setAttribute('rx','10');rc.setAttribute('ry','10');
    if(nodeStatus[n.id]){var c={'done':'#059669','running':'#6366f1','pending':'#d97706','blocked':'#64748b','failed':'#e11d48'}[nodeStatus[n.id]];if(c)rc.setAttribute('stroke',c);}
    g.appendChild(rc);
    var bar=document.createElementNS(SVGNS,'rect');bar.setAttribute('width','4');bar.setAttribute('height',H);bar.setAttribute('rx','2');bar.setAttribute('fill',TYPE_COLOR[n.type]||'#6366f1');
    g.appendChild(bar);
    var t1=document.createElementNS(SVGNS,'text');t1.setAttribute('x','14');t1.setAttribute('y','24');t1.setAttribute('class','ntitle');t1.textContent=(n.name||n.id);g.appendChild(t1);
    var t2=document.createElementNS(SVGNS,'text');t2.setAttribute('x','14');t2.setAttribute('y','44');t2.setAttribute('class','ncmd');t2.textContent='▸ '+(n.command||'(无命令)').slice(0,20);g.appendChild(t2);
    var pill=document.createElementNS(SVGNS,'rect');pill.setAttribute('x',(W-48));pill.setAttribute('y','9');pill.setAttribute('width','40');pill.setAttribute('height','16');pill.setAttribute('rx','8');pill.setAttribute('fill',TYPE_SOFT[n.type]||'#eceaff');g.appendChild(pill);
    var pt=document.createElementNS(SVGNS,'text');pt.setAttribute('x',(W-28));pt.setAttribute('y','21');pt.setAttribute('class','ntype');pt.setAttribute('fill',TYPE_COLOR[n.type]||'#6366f1');pt.textContent=n.type;g.appendChild(pt);
    var port=document.createElementNS(SVGNS,'circle');port.setAttribute('class','port');port.setAttribute('cx',W);port.setAttribute('cy',H/2);port.setAttribute('r','6');
    port.addEventListener('mousedown',function(ev){ev.stopPropagation();startLinkDrag(ev,n.id);});
    port.addEventListener('click',function(ev){ev.stopPropagation();});
    g.appendChild(port);
    g.addEventListener('mousedown',function(ev){ev.preventDefault();nodeMouseDown(ev,n.id,p);});
    g.addEventListener('click',function(ev){ev.stopPropagation();if(linking){linkClick(n.id);}else{selectNode(n.id);}});
    NG.appendChild(g);
  });
  vp.appendChild(NG);
  updateZoomLabel();
  renderNodeList();renderNodeEditor();
}
var drag=null;
function flowPoint(ev){
  var vp=document.getElementById('viewport');if(!vp)return{x:0,y:0};
  var ctm=vp.getScreenCTM();if(!ctm)return{x:0,y:0};
  var svg=document.getElementById('canvas');var sp=svg.createSVGPoint();sp.x=ev.clientX;sp.y=ev.clientY;
  var q=sp.matrixTransform(ctm.inverse());
  return {x:q.x,y:q.y};
}
function nodeMouseDown(ev,id,pos){
  var c=flowPoint(ev);
  drag={id:id,ox:c.x-pos.x,oy:c.y-pos.y,moved:false};
  document.addEventListener('mousemove',nodeMouseMove);
  document.addEventListener('mouseup',nodeMouseUp);
}
function nodeMouseMove(ev){
  if(!drag)return;
  var c=flowPoint(ev);
  if(!drag.moved){drag.moved=true;snapshot();}
  nodePos[drag.id]={x:snap(c.x-drag.ox),y:snap(c.y-drag.oy)};
  renderFlow();
}
function nodeMouseUp(){drag=null;document.removeEventListener('mousemove',nodeMouseMove);document.removeEventListener('mouseup',nodeMouseUp);}
function svgMouseDown(ev){
  if(linking)return;
  if(ev.target&&ev.target.closest&&ev.target.closest('.node'))return;
  if(ev.target&&ev.target.classList&&ev.target.classList.contains('edge-hit'))return;
  panning={sx:ev.clientX,sy:ev.clientY,tx:view.tx,ty:view.ty};
  var svg=document.getElementById('canvas');if(svg)svg.classList.add('panning');
  document.addEventListener('mousemove',panMove);
  document.addEventListener('mouseup',panUp);
}
function panMove(ev){if(!panning)return;view.tx=panning.tx+(ev.clientX-panning.sx);view.ty=panning.ty+(ev.clientY-panning.sy);applyView();}
function panUp(){panning=null;var svg=document.getElementById('canvas');if(svg)svg.classList.remove('panning');document.removeEventListener('mousemove',panMove);document.removeEventListener('mouseup',panUp);}
function svgWheel(ev){
  ev.preventDefault();
  var svg=document.getElementById('canvas');var r=svg.getBoundingClientRect();
  var mx=ev.clientX-r.left,my=ev.clientY-r.top;
  var cx=(mx-view.tx)/view.scale,cy=(my-view.ty)/view.scale;
  var f=ev.deltaY<0?1.12:0.89;
  var ns=Math.max(0.3,Math.min(2.5,view.scale*f));
  view.scale=ns;view.tx=mx-cx*ns;view.ty=my-cy*ns;applyView();
}
function applyView(){var vp=document.getElementById('viewport');if(vp)vp.setAttribute('transform','translate('+view.tx+','+view.ty+') scale('+view.scale+')');updateZoomLabel();}
function updateZoomLabel(){var z=document.getElementById('zoomVal');if(z)z.textContent=Math.round(view.scale*100)+'%';}
function zoomBy(f){var svg=document.getElementById('canvas');var r=svg.getBoundingClientRect();var mx=r.width/2,my=r.height/2;var cx=(mx-view.tx)/view.scale,cy=(my-view.ty)/view.scale;var ns=Math.max(0.3,Math.min(2.5,view.scale*f));view.scale=ns;view.tx=mx-cx*ns;view.ty=my-cy*ns;applyView();}
function fitView(){
  var ids=Object.keys(nodePos);if(!ids.length){view={scale:1,tx:0,ty:0};applyView();return;}
  var minx=1e9,miny=1e9,maxx=-1e9,maxy=-1e9;
  ids.forEach(function(id){var p=nodePos[id];if(!p)return;minx=Math.min(minx,p.x);miny=Math.min(miny,p.y);maxx=Math.max(maxx,p.x+170);maxy=Math.max(maxy,p.y+66);});
  var svg=document.getElementById('canvas');var r=svg.getBoundingClientRect();
  var pad=50;var bw=(maxx-minx)+pad*2,bh=(maxy-miny)+pad*2;
  var s=Math.min(r.width/bw,r.height/bh,2);if(s<0.2)s=0.2;
  view.scale=s;view.tx=(r.width-bw*s)/2-(minx-pad)*s;view.ty=(r.height-bh*s)/2-(miny-pad)*s;applyView();
}
function resetView(){view={scale:1,tx:0,ty:0};applyView();}
function addDep(srcId,depId){
  if(srcId===depId){flowMsg('不能依赖自身',false);return false;}
  var src=flow.dag.find(function(n){return n.id===srcId;});
  if(!src)return false;
  if((src.dependsOn||[]).indexOf(depId)>=0){flowMsg('依赖已存在',false);return false;}
  if(createsCycle(srcId,depId)){flowMsg('该依赖会形成环，已忽略',false);return false;}
  src.dependsOn=src.dependsOn||[];src.dependsOn.push(depId);
  flowMsg('已添加依赖: '+srcId+' → '+depId,true);return true;
}
function linkClick(id){
  if(!linkSrc){linkSrc=id;flowMsg('连线源: '+id+'，点击目标步骤',true);return;}
  if(linkSrc===id){flowMsg('不能依赖自身',false);linkSrc=null;return;}
  snapshot();
  addDep(linkSrc,id);
  linkSrc=null;renderFlow();
}
function selectEdge(src,dst){selectedEdge={src:src,dst:dst};renderFlow();flowMsg('已选中依赖 '+src+' → '+dst+'（按 Delete 或 Esc 取消）',true);}
function deleteEdge(src,dst){
  var n=flow.dag.find(function(x){return x.id===dst;});if(!n)return;
  snapshot();
  n.dependsOn=(n.dependsOn||[]).filter(function(d){return d!==src;});
  selectedEdge=null;renderFlow();flowMsg('已删除依赖 '+src+' → '+dst,true);
}
function startLinkDrag(ev,id){
  linking=true;linkSrc=id;
  var c=flowPoint(ev);linkDrag={from:id,x:c.x,y:c.y};
  var b=document.getElementById('linkBtn');if(b)b.textContent='🔗 拖拽连线中…';
  var p=document.getElementById('tab-flow');if(p)p.classList.add('linkmode');
  document.addEventListener('mousemove',linkDragMove);
  document.addEventListener('mouseup',linkDragUp);
}
function linkDragMove(ev){if(!linkDrag)return;var c=flowPoint(ev);linkDrag.x=c.x;linkDrag.y=c.y;renderFlow();}
function linkDragUp(ev){
  document.removeEventListener('mousemove',linkDragMove);
  document.removeEventListener('mouseup',linkDragUp);
  var c=flowPoint(ev);
  var target=null;
  flow.dag.forEach(function(n){var p=nodePos[n.id];if(!p)return;if(c.x>=p.x&&c.x<=p.x+170&&c.y>=p.y&&c.y<=p.y+66)target=n.id;});
  linking=false;linkSrc=null;linkDrag=null;
  var b=document.getElementById('linkBtn');if(b)b.textContent='🔗 连线';
  var p=document.getElementById('tab-flow');if(p)p.classList.remove('linkmode');
  if(target&&target!==id){snapshot();addDep(target,id);}
  renderFlow();
}
function renderNodeList(){
  var el=document.getElementById('nodeList');if(!el)return;
  if(!flow.dag.length){el.innerHTML='<p class="muted">暂无步骤。点「＋ 添加步骤」开始，或点右上「载入示例」。</p>';return;}
  el.innerHTML=flow.dag.map(function(n){
    return '<div class="ci'+(selectedNode===n.id?' sel':'')+'" onclick="selectNode(\''+esc(n.id)+'\')">'+esc(n.name||n.id)+' <small class="muted">'+esc(n.type)+'</small>'+(selectedNode===n.id?' ✦':'')+'</div>';
  }).join('');
}
function renderNodeEditor(){
  var el=document.getElementById('nodeEditor');if(!el)return;
  if(!selectedNode){el.innerHTML='';return;}
  var n=flow.dag.find(function(x){return x.id===selectedNode;});if(!n){el.innerHTML='';return;}
  var others=flow.dag.filter(function(x){return x.id!==n.id;}).map(function(x){
    return '<option value="'+esc(x.id)+'"'+(((n.dependsOn||[]).indexOf(x.id)>=0)?' selected':'')+'>'+esc(x.name||x.id)+'</option>';
  }).join('');
  el.innerHTML='<h4>编辑步骤: '+esc(n.id)+'</h4>'+
    '<label>名称:<input id="nName" value="'+esc(n.name)+'" size="16"></label><br>'+
    '<label>类型:<select id="nType"><option value="shell"'+(n.type==='shell'?' selected':'')+'>shell（执行命令）</option><option value="file"'+(n.type==='file'?' selected':'')+'>file（下发文件）</option><option value="service"'+(n.type==='service'?' selected':'')+'>service（启停服务）</option></select></label><br>'+
    '<label>命令/动作:<input id="nCmd" value="'+esc(n.command)+'" size="28" title="该步骤要执行的内容"></label><br>'+
    '<label>路径(path):<input id="nPath" value="'+esc(n.path)+'" size="18"></label><br>'+
    '<label>依赖(多选):<select id="nDeps" multiple size="3" title="本步骤开始前应完成的其它步骤">'+others+'</select></label><br>'+
    '<div class="btnbar"><button onclick="applyNode()">应用</button> <button onclick="deleteNode(\''+esc(n.id)+'\')">删除步骤</button></div>';
}
function applyNode(){
  var n=flow.dag.find(function(x){return x.id===selectedNode;});if(!n)return;
  snapshot();
  n.name=document.getElementById('nName').value;
  n.type=document.getElementById('nType').value;
  n.command=document.getElementById('nCmd').value;
  n.path=document.getElementById('nPath').value;
  var sel=document.getElementById('nDeps');var deps=[];
  for(var i=0;i<sel.options.length;i++){if(sel.options[i].selected)deps.push(sel.options[i].value);}
  n.dependsOn=deps.filter(function(d){return d!==n.id&&!createsCycle(n.id,d);});
  renderFlow();
}
function saveWorkflow(){
  var name=document.getElementById('wfName').value.trim();
  var agent=document.getElementById('wfAgent').value;
  var cron=document.getElementById('wfCron').value.trim();
  if(!name||!agent){flowMsg('请填写名称和采集端',false);return;}
  var dagStr=JSON.stringify(flow.dag);
  if(flow.id){
    fetch('/api/v1/workflows/'+flow.id,{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify({name:name,dag:dagStr,cron:cron})})
      .then(function(r){return r.json().then(function(j){return{s:r.status,j:j};});})
      .then(function(x){if(x.s>=400){flowMsg('['+x.s+'] '+(x.j.error||''),false);}else{flow.id=x.j.id;flow.status=x.j.status;flow.cron=x.j.cron||'';flowMsg('已保存 #'+x.j.id,true);loadFlows();}});
  }else{
    fetch('/api/v1/workflows',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({name:name,agentID:agent,dag:dagStr,cron:cron})})
      .then(function(r){return r.json().then(function(j){return{s:r.status,j:j};});})
      .then(function(x){if(x.s>=400){flowMsg('['+x.s+'] '+(x.j.error||''),false);}else{flow.id=x.j.id;flow.status=x.j.status;flow.cron=x.j.cron||'';flowMsg('已创建 #'+x.j.id,true);loadFlows();}});
  }
}
function runWorkflow(){
  if(!flow.id){flowMsg('请先保存作业流',false);return;}
  fetch('/api/v1/workflows/'+flow.id+'/run',{method:'POST'})
    .then(function(r){return r.json().then(function(j){return{s:r.status,j:j};});})
    .then(function(x){if(x.s>=400){flowMsg('['+x.s+'] '+(x.j.error||''),false);}else{flowMsg('已触发运行 #'+flow.id,true);pollStatus();}});
}
function alignSel(axis){
  if(!selectedNode){flowMsg('先选中一个步骤作为对齐基准',false);return;}
  var ref=nodePos[selectedNode];if(!ref)return;
  var others=flow.dag.filter(function(n){return n.id!==selectedNode;}).map(function(n){return nodePos[n.id];}).filter(Boolean);
  snapshot();
  if(!others.length){
    if(axis==='left')ref.x=0;else if(axis==='right')ref.x=snap(170-170);else if(axis==='top')ref.y=0;else if(axis==='bottom')ref.y=0;
    renderFlow();flowMsg('已对齐到画布',true);return;
  }
  if(axis==='left'){var x=Math.min.apply(null,others.map(function(p){return p.x;}));ref.x=snap(x);}
  else if(axis==='right'){var x=Math.max.apply(null,others.map(function(p){return p.x+170;}));ref.x=snap(x-170);}
  else if(axis==='top'){var y=Math.min.apply(null,others.map(function(p){return p.y;}));ref.y=snap(y);}
  else if(axis==='bottom'){var y=Math.max.apply(null,others.map(function(p){return p.y+66;}));ref.y=snap(y-66);}
  renderFlow();flowMsg('已'+(axis==='left'?'左':axis==='right'?'右':axis==='top'?'上':'下')+'对齐',true);
}
function paintRunState(){
  var el=document.getElementById('flowLegend');if(!el)return;
  if(!flow.id||!Object.keys(nodeStatus).length){el.className='flowLegend';el.innerHTML='';return;}
  var c={};Object.keys(nodeStatus).forEach(function(k){c[nodeStatus[k]]=(c[nodeStatus[k]]||0)+1;});
  var col={'done':'#059669','running':'#6366f1','pending':'#d97706','blocked':'#64748b','failed':'#e11d48'};
  var label={'done':'成功','running':'运行中','pending':'等待','blocked':'阻塞','failed':'失败'};
  var html='<span class="muted">运行态:</span>';
  Object.keys(c).forEach(function(k){html+='<span class="pill"><span class="dot" style="background:'+(col[k]||'#64748b')+'"></span>'+(label[k]||k)+' '+c[k]+'</span>';});
  html+='<button onclick="switchTab(\'ops\')" title="跳到运维中枢查看任务执行详情">🔍 在运维中枢查看</button>';
  el.className='flowLegend show';el.innerHTML=html;
}
function pollStatus(){
  if(!flow.id)return;
  fetch('/api/v1/workflows/'+flow.id+'/status').then(function(r){return r.json();}).then(function(x){
    if(x.error)return;
    var nt=x.nodeTasks||{};nodeStatus={};
    Object.keys(nt).forEach(function(k){var nid=k.replace(/^wf-\d+-/,'');nodeStatus[nid]=nt[k];});
    renderFlow();paintRunState();
    var cnt={};Object.keys(nodeStatus).forEach(function(k){cnt[nodeStatus[k]]=(cnt[nodeStatus[k]]||0)+1;});
    flowMsg('运行态: '+JSON.stringify(cnt),true);
    setTimeout(function(){var p=document.getElementById('tab-flow');if(p&&p.classList.contains('active'))pollStatus();},3000);
  }).catch(function(e){console.error(e);});
}
function scheduleWorkflowPrompt(){
  if(!flow.id){flowMsg('请先保存作业流',false);return;}
  var cron=document.getElementById('wfCron').value.trim();
  fetch('/api/v1/workflows/'+flow.id+'/schedule',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({cron:cron})})
    .then(function(r){return r.json().then(function(j){return{s:r.status,j:j};});})
    .then(function(x){if(x.s>=400){flowMsg('['+x.s+'] '+(x.j.error||''),false);}else{flow.cron=x.j.cron||'';flow.status=x.j.status;flowMsg('已设置定时: '+(x.j.cron||'(无)'),true);loadFlows();}});
}

// ---------- 部署中心（M3） ----------
function deployMsg(s,ok){var el=document.getElementById('deployMsg');if(el){el.className='msg '+(ok?'ok':'err');el.textContent=(ok?'[ok] ':'[err] ')+s;}}
function loadDeployDemo(){
  document.getElementById('dpName').value='deploy-nginx';
  document.getElementById('dpType').value='script';
  document.getElementById('dpRepo').value='https://git.example.com/ops/nginx-deploy.git';
  document.getElementById('dpContent').value='';
  document.getElementById('dpPath').value='';
  document.getElementById('dpTargets').value='dev-10.0.0.1, dev-10.0.0.2';
  deployMsg('已载入示例，可改后点「登记部署」',true);
}
function dpStatusPill(st){
  var cls={'created':'info','running':'warn','success':'ok','failed':'fail','rolledback':'warn'}[st]||'info';
  return '<span class="pill '+cls+'">'+esc(st)+'</span>';
}
function pollDeploys(){
  var st=document.getElementById('dpStatusFilter').value;
  fetch('/api/v1/deploys'+(st?'?status='+encodeURIComponent(st):''))
    .then(function(r){return r.json();})    .then(function(list){
      var fl=applyFocus(list||[],'deploy');
      var note=focusDevice?'<p class="hint">🔗 已按设备 <code>'+esc(focusDevice.id)+'</code> 过滤（'+fl.length+' 条）</p>':'';
      if(!fl||fl.length===0){document.getElementById('deployList').innerHTML=note+'<p class="muted">暂无部署任务。在左侧登记一个吧。</p>';return;}
      var html=note+'<table><tr><th>ID</th><th>名称</th><th>类型</th><th>目标设备</th><th>状态</th><th>操作</th></tr>';
      fl.forEach(function(d){
        var targets=(d.target_ids||'').replace(/,/g,', ');
        html+='<tr><td><code>'+esc(d.id)+'</code></td><td>'+esc(d.name)+'</td><td>'+esc(d.type)+'</td>'+
          '<td><code>'+esc(targets)+'</code></td><td>'+dpStatusPill(d.status)+'</td>'+
          '<td><button onclick="execDeploy('+d.id+')">▶ 执行</button> <button onclick="rollbackDeploy('+d.id+')">↩ 回滚</button> <button onclick="openDeploy('+d.id+')">详情</button></td></tr>';
      });
      html+='</table>';
      document.getElementById('deployList').innerHTML=html;
    }).catch(function(e){console.error(e);});
}
function execDeploy(id){
  fetch('/api/v1/deploys/'+id+'/execute',{method:'POST'})
    .then(function(r){return r.json().then(function(j){return{s:r.status,j:j};});})
    .then(function(x){deployMsg('['+x.s+'] '+(x.j.error||'已触发执行 #'+id),x.s<400);pollDeploys();})
    .catch(function(e){deployMsg('error: '+e,false);});
}
function rollbackDeploy(id){
  fetch('/api/v1/deploys/'+id+'/rollback',{method:'POST'})
    .then(function(r){return r.json().then(function(j){return{s:r.status,j:j};});})
    .then(function(x){deployMsg('['+x.s+'] '+(x.j.error||'已回滚 #'+id),x.s<400);pollDeploys();})
    .catch(function(e){deployMsg('error: '+e,false);});
}
function openDeploy(id){
  fetch('/api/v1/deploys/'+id).then(function(r){return r.json();}).then(function(d){
    var h='<h3>部署 #'+esc(d.id)+' · '+esc(d.name)+'</h3>';
    h+='<p>类型: '+esc(d.type)+' ｜ 状态: '+dpStatusPill(d.status)+'</p>';
    h+='<p>目标设备: <code>'+esc((d.target_ids||'').replace(/,/g,', '))+'</code></p>';
    if(d.repo_url)h+='<p>仓库: <code>'+esc(d.repo_url)+'</code></p>';
    if(d.path)h+='<p>路径: <code>'+esc(d.path)+'</code></p>';
    if(d.content)h+='<p>内容: <code>'+esc(d.content)+'</code></p>';
    h+='<p class="muted">创建人: '+esc(d.created_by)+' ｜ 创建: '+esc(d.created_at)+' ｜ 更新: '+esc(d.updated_at)+'</p>';
    if(d.task_ids)h+='<p>派发任务: <code>'+esc((d.task_ids||'').replace(/,/g,', '))+'</code></p>';
    document.getElementById('drawerBody').innerHTML=h;
    document.getElementById('drawer').classList.add('open');
  }).catch(function(e){console.error(e);});
}

// ---------- 日志检索（M6） ----------
var logOffset=0;
function logMsg(s,ok){var el=document.getElementById('logMsg');if(el){el.className='msg '+(ok?'ok':'err');el.textContent=(ok?'[ok] ':'[err] ')+s;}}
function logLevelPill(lv){
  var cls={'error':'fail','warn':'warn','info':'info'}[lv]||'info';
  return '<span class="pill '+cls+'">'+esc(lv)+'</span>';
}
function buildLogQuery(offset){
  var p=[];
  var d=document.getElementById('logDevice').value.trim();
  var a=document.getElementById('logAgent').value.trim();
  var lv=document.getElementById('logLevel').value;
  var src=document.getElementById('logSource').value;
  var kw=document.getElementById('logKeyword').value.trim();
  var f=document.getElementById('logFrom').value.trim();
  var t=document.getElementById('logTo').value.trim();
  var lim=document.getElementById('logLimit').value.trim()||'200';
  if(d)p.push('deviceID='+encodeURIComponent(d));
  if(a)p.push('agentID='+encodeURIComponent(a));
  if(lv)p.push('level='+encodeURIComponent(lv));
  if(src)p.push('source='+encodeURIComponent(src));
  if(kw)p.push('keyword='+encodeURIComponent(kw));
  if(f)p.push('from='+encodeURIComponent(f));
  if(t)p.push('to='+encodeURIComponent(t));
  p.push('limit='+encodeURIComponent(lim));
  p.push('offset='+offset);
  return p.join('&');
}
function searchLogs(offset){
  logOffset=(offset||0);
  document.getElementById('logLimitInfo').textContent=document.getElementById('logLimit').value||'200';
  fetch('/api/v1/logs?'+buildLogQuery(logOffset))
    .then(function(r){return r.json();}).then(function(list){
      if(!list||list.length===0){document.getElementById('logList').innerHTML='<p class="muted">没有匹配的日志。</p>';updateLogPage(0);return;}
      var html='<table><tr><th>时间</th><th>级别</th><th>来源</th><th>设备</th><th>Agent</th><th>消息</th></tr>';
      list.forEach(function(e){
        var ts=(e.timestamp||'').toString().replace('T',' ').replace('Z','');
        html+='<tr><td><small class="muted">'+esc(ts)+'</small></td><td>'+logLevelPill(e.level)+'</td><td>'+esc(e.source||'')+'</td><td><code>'+(e.deviceID||'')+'</code></td><td><code>'+(e.agentID||'')+'</code></td><td style="white-space:pre-wrap">'+esc(e.message||'')+'</td></tr>';
      });
      html+='</table>';
      document.getElementById('logList').innerHTML=html;
      updateLogPage(list.length);
    }).catch(function(err){logMsg('error: '+err,false);});
}
function updateLogPage(n){
  var lim=parseInt(document.getElementById('logLimit').value||'200',10);
  var cur=Math.floor(logOffset/lim)+1;
  document.getElementById('logPageInfo').textContent='第 '+cur+' 页（本页 '+n+' 条）';
}
function logPrev(){ if(logOffset>0){searchLogs(Math.max(0,logOffset-parseInt(document.getElementById('logLimit').value||'200',10)));} }
function logNext(){ searchLogs(logOffset+parseInt(document.getElementById('logLimit').value||'200',10)); }
function resetLogFilters(){
  ['logDevice','logAgent','logKeyword','logFrom','logTo'].forEach(function(id){document.getElementById(id).value='';});
  document.getElementById('logLevel').value='';document.getElementById('logSource').value='';
  document.getElementById('logList').innerHTML='<p class="muted">已清空，填写条件后点「查询」。</p>';
}

// ---------- 监控告警（M7，独立 Tab） ----------
function pollAlertsFull(){
  fetch('/api/v1/alerts').then(function(r){return r.json();}).then(function(list){
    var fl=applyFocus(list||[],'alert');
    var crit=0,warn=0;
    fl.forEach(function(a){if(a.severity==='critical')crit++;else warn++;});
    var sc=document.getElementById('statCritical');if(sc)sc.textContent=crit;
    var sw=document.getElementById('statWarning');if(sw)sw.textContent=warn;
    var stEl=document.getElementById('statTotalAlerts');if(stEl)stEl.textContent=fl.length;
    var note=focusDevice?'<p class="hint">🔗 已按设备 <code>'+esc(focusDevice.id)+'</code> 过滤（'+fl.length+' 条）</p>':'';
    if(fl.length===0){document.getElementById('alertsFull').innerHTML=note+'<p class="muted">暂无告警，一切正常 ✅</p>';return;}
    var html=note;
    fl.forEach(function(a){
      var cls=a.severity==='critical'?'alert':'alert warn';
      var ast=a.status||'firing';
      var badge = ast==='acknowledged' ? '<span class="badge ok">已确认</span>'
                 : ast==='silenced' ? '<span class="badge info">已静默</span>'
                 : '<span class="badge fail">待处理</span>';
      var actions='';
      if(ast==='firing'){
        actions='<div class="alert-actions">'
          +'<button class="btn xs" onclick="ackAlert(\''+esc(a.alertID)+'\')">✓ 确认</button>'
          +'<button class="btn xs outline" onclick="silenceAlert(\''+esc(a.alertID)+'\')">🔕 静默</button>'
          +'</div>';
      } else {
        var meta=esc(a.acknowledgedBy||'');
        if(ast==='silenced'&&a.silencedUntil){ meta+=' · 至 '+esc(a.silencedUntil); }
        actions='<div class="alert-actions"><span class="muted" style="font-size:12px">处理人：'+(meta||'—')+'</span></div>';
      }
      html+='<div class="'+cls+'">'
        +'<div class="alert-head"><b>['+esc(a.severity)+']</b> '+badge+'</div>'
        +'设备 '+esc(a.deviceID)+' ｜ Agent '+esc(a.agentID)
        +(a.comment?'<br><small class="muted">备注：'+esc(a.comment)+'</small>':'')
        +'<br>'+esc(a.message)
        +'<br><small class="muted">'+esc(a.createdAt)+'</small>'
        +actions
        +'<button class="jbtn" style="margin-top:6px" onclick="setFocus(\''+esc(a.deviceID)+'\',\'\',\'\',\'\');switchTab(\'alerts\')">🔗 上下文串联</button>'
        +'</div>';
    });
    document.getElementById('alertsFull').innerHTML=html;
  }).catch(function(e){console.error(e);});
}

// 确认告警（M7）：POST /api/v1/alerts/{id}/ack
function ackAlert(id){
  fetch('/api/v1/alerts/'+encodeURIComponent(id)+'/ack',{method:'POST'}).then(function(r){
    return r.json().then(function(j){return {s:r.status,j:j};});
  }).then(function(x){
    if(x.s<400){ pollAlertsFull(); pollAlerts(); }
    else { alert('确认失败：'+(x.j.error||x.s)); }
  }).catch(function(err){ alert('确认失败：'+err); });
}

// 静默告警（M7）：POST /api/v1/alerts/{id}/silence（默认 24h）
function silenceAlert(id){
  var dur=prompt('静默时长（分钟，留空=24 小时）：','1440');
  if(dur===null) return;
  var minutes=parseInt(dur,10); if(isNaN(minutes)||minutes<=0) minutes=1440;
  var comment=prompt('处理备注（可选）：','')||'';
  fetch('/api/v1/alerts/'+encodeURIComponent(id)+'/silence',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({durationMinutes:minutes,comment:comment})})
    .then(function(r){return r.json().then(function(j){return {s:r.status,j:j};});})
    .then(function(x){
      if(x.s<400){ pollAlertsFull(); pollAlerts(); }
      else { alert('静默失败：'+(x.j.error||x.s)); }
    }).catch(function(err){ alert('静默失败：'+err); });
}

// ---------- 跨模块联动（F1） ----------
var focusDevice=null;
function setFocus(id,ip,agentID,segment){
  focusDevice={id:id,ip:ip||'',agentID:agentID||'',segment:segment||''};
  var b=document.getElementById('ctxbar'); if(b)b.classList.add('show');
  var d=document.getElementById('ctxDev'); if(d)d.textContent=id+(ip?(' ('+ip+')'):'');
}
function clearFocus(){ focusDevice=null; var b=document.getElementById('ctxbar'); if(b)b.classList.remove('show'); pollTasks(); pollAlertsFull(); pollDeploys(); }
function applyFocus(list,kind){
  if(!focusDevice||!list) return list;
  return list.filter(function(x){
    if(kind==='task') return x.agentID===focusDevice.agentID;
    if(kind==='alert') return (x.deviceID===focusDevice.id)||(x.agentID===focusDevice.agentID);
    if(kind==='deploy'){ var ts=((x.target_ids||'')+'').split(/[,\s]+/).map(function(s){return s.trim();}).filter(Boolean); return ts.indexOf(focusDevice.id)>=0; }
    return true;
  });
}
function jumpFocus(tab){
  if(!focusDevice) return;
  if(tab==='logs'){ switchTab('logs'); var di=document.getElementById('logDevice'); if(di)di.value=focusDevice.id; searchLogs(0); return; }
  if(tab==='cmdb'){ focusCI(); return; }
  switchTab(tab);
}
function focusCI(){
  switchTab('cmdb');
  if(!focusDevice) return;
  fetch('/api/v1/cmdb/types').then(function(r){return r.json();}).then(function(ts){
    Promise.all((ts||[]).map(function(t){
      return fetch('/api/v1/cmdb/ci?type='+encodeURIComponent(t.type)).then(function(r){return r.json();}).then(function(arr){return (arr||[]).filter(function(c){return c.deviceID===focusDevice.id;});});
    })).then(function(groups){
      var all=[]; groups.forEach(function(g){all=all.concat(g);});
      var el=document.getElementById('ciList');
      if(!all.length){ el.innerHTML='<p class="muted">配置库中无关联该设备的配置项。</p>'; return; }
      var html='<p class="hint">🔗 已按设备 <code>'+esc(focusDevice.id)+'</code> 过滤（'+all.length+' 条）</p>';
      html+='<table><tr><th>ID</th><th>名称</th><th>类型</th><th>状态</th></tr>';
      all.forEach(function(c){ html+='<tr class="ci" onclick="openCI(\''+esc(c.id)+'\')"><td><code>'+esc(c.id)+'</code></td><td>'+esc(c.name)+'</td><td>'+esc(c.ciType)+'</td><td>'+esc(c.status)+'</td></tr>'; });
      html+='</table>';
      el.innerHTML=html;
    }).catch(function(e){console.error(e);});
  }).catch(function(e){console.error(e);});
}

// ---------- 今日成功率趋势线（F2） ----------
function paintTrend(){
  var el=document.getElementById('ovTrend'); if(!el) return;
  var tasks=lastTasks||[];
  var now=new Date(); var buckets={};
  for(var h=0;h<24;h++) buckets[h]={done:0,fail:0};
  var has=false;
  tasks.forEach(function(t){
    if(t.status!=='done'&&t.status!=='failed') return;
    var d=new Date(t.createdAt); if(isNaN(d.getTime())) return;
    if(d.getFullYear()!==now.getFullYear()||d.getMonth()!==now.getMonth()||d.getDate()!==now.getDate()) return;
    var h=d.getHours(); buckets[h][t.status==='done'?'done':'fail']++; has=true;
  });
  if(!has){ el.innerHTML='<p class="muted">今日暂无终态任务。下发的任务完成后会在此显示成功率趋势。</p>'; return; }
  var W=720,H=180,padL=34,padR=14,padT=14,padB=24;
  var plotW=W-padL-padR, plotH=H-padT-padB;
  var hrs=[]; for(var h=0;h<24;h++){ if(buckets[h].done+buckets[h].fail>0) hrs.push(h); }
  var n=hrs.length;
  var x=function(i){ return padL + (n===1?plotW/2:(plotW*i/(n-1))); };
  var maxCnt=1; hrs.forEach(function(h){ var c=buckets[h].done+buckets[h].fail; if(c>maxCnt)maxCnt=c; });
  var yRate=function(rate){ return padT + plotH*(1-rate/100); };
  var bars='';
  hrs.forEach(function(h,i){ var c=buckets[h].done+buckets[h].fail; var bx=x(i)-9, bh=plotH*(c/maxCnt); bars+='<rect x="'+bx+'" y="'+(padT+plotH-bh)+'" width="18" height="'+bh+'" rx="3" fill="rgba(13,148,136,.18)"></rect>'; });
  var area='', line='', dots='', labels='';
  hrs.forEach(function(h,i){
    var c=buckets[h].done+buckets[h].fail; var rate=c?(buckets[h].done/c*100):0;
    var px=x(i), py=yRate(rate);
    if(i===0) area+='M'+px+' '+(padT+plotH);
    area+=' L'+px+' '+py;
    line+=(i===0?'M':'L')+px+' '+py+' ';
    dots+='<circle class="trend-pt" cx="'+px+'" cy="'+py+'" r="4" title="'+h+'时：成功 '+buckets[h].done+' / 失败 '+buckets[h].fail+'（成功率 '+rate.toFixed(0)+'%）"></circle>';
    labels+='<text x="'+px+'" y="'+(H-7)+'" text-anchor="middle" font-size="10" fill="var(--text-3)">'+h+'h</text>';
  });
  area+=' L'+x(n-1)+' '+(padT+plotH)+' Z';
  el.innerHTML='<svg viewBox="0 0 '+W+' '+H+'" width="100%" style="display:block">'
    + '<line x1="'+padL+'" y1="'+(padT+plotH)+'" x2="'+(W-padR)+'" y2="'+(padT+plotH)+'" stroke="var(--border-2)" stroke-width="1"/>'
    + bars + '<path class="trend-area" d="'+area+'"/>' + '<path class="trend-line" d="'+line+'"/>' + dots + labels
    + '</svg>';
}

// ---------- 网段拓扑（F2） ----------
var SEG_META=[
  {cidr:'10.20.0.0/24',name:'mgmt-net（管理网）',color:'var(--indigo)'},
  {cidr:'10.21.0.0/24',name:'data-net（数据网）',color:'var(--teal)'},
  {cidr:'10.22.0.0/24',name:'soc-net（安全网）',color:'var(--amber)'},
  {cidr:'10.30.0.0/16',name:'seg-net（业务网）',color:'var(--rose)'}
];
function ipToInt(ip){ var p=(ip||'').split('.'); if(p.length!==4) return -1; return ((+p[0])<<24)+((+p[1])<<16)+((+p[2])<<8)+(+p[3]); }
function cidrMatch(ip,cidr){ var m=cidr.split('/'); if(m.length!==2) return false; var base=ipToInt(m[0]),addr=ipToInt(ip),pre=+m[1]; if(base<0||addr<0) return false; var mask=pre===0?0:(0xFFFFFFFF<<(32-pre)); return (addr&mask)===(base&mask); }
function paintTopo(){
  var el=document.getElementById('ovTopo'); if(!el) return;
  var devs=lastDevices||{}; var counts={};
  SEG_META.forEach(function(m){ counts[m.cidr]=0; });
  Object.keys(devs).forEach(function(seg){ (devs[seg]||[]).forEach(function(d){ SEG_META.forEach(function(m){ if(cidrMatch(d.ip,m.cidr)) counts[m.cidr]++; }); }); });
  var segs=SEG_META.map(function(m){ return {name:m.name,color:m.color,count:counts[m.cidr]||0}; });
  if(segs.length===0){ el.innerHTML='<p class="muted">暂无网段设备。</p>'; return; }
  var W=720,H=Math.max(170,30+segs.length*46),padL=20,padT=20;
  var cpX=30,cpY=H/2,cpW=150,cpH=58;
  var svg='<svg viewBox="0 0 '+W+' '+H+'" width="100%" style="display:block">';
  svg+='<rect x="'+cpX+'" y="'+(cpY-cpH/2)+'" width="'+cpW+'" height="'+cpH+'" rx="12" fill="var(--surface-2)" stroke="var(--accent)" stroke-width="2"/>';
  svg+='<text x="'+(cpX+cpW/2)+'" y="'+(cpY-4)+'" text-anchor="middle" font-size="13" font-weight="600" fill="var(--text)">控制面</text>';
  svg+='<text x="'+(cpX+cpW/2)+'" y="'+(cpY+14)+'" text-anchor="middle" font-size="11" fill="var(--text-3)">OpsMesh</text>';
  var nx=cpX+cpW+120, nw=W-nx-20, nh=40;
  segs.forEach(function(s,i){
    var ny=padT+i*46;
    svg+='<line class="topo-edge" x1="'+(cpX+cpW)+'" y1="'+cpY+'" x2="'+nx+'" y2="'+(ny+nh/2)+'"/>';
    svg+='<rect x="'+nx+'" y="'+ny+'" width="'+nw+'" height="'+nh+'" rx="10" fill="var(--surface-2)" stroke="'+s.color+'" stroke-width="2"/>';
    svg+='<circle cx="'+(nx+20)+'" cy="'+(ny+nh/2)+'" r="7" fill="'+s.color+'"/>';
    svg+='<text class="topo-label" x="'+(nx+38)+'" y="'+(ny+nh/2-3)+'">'+esc(s.name)+'</text>';
    svg+='<text class="topo-count" x="'+(nx+38)+'" y="'+(ny+nh/2+15)+'" fill="'+s.color+'">'+s.count+' 台设备</text>';
  });
  svg+='</svg>';
  el.innerHTML=svg;
}

// ---------- 动态身份注入（F3） ----------
function fetchMe(){
  fetch('/api/v1/me').then(function(r){return r.json().then(function(j){return {s:r.status,j:j};});}).then(function(x){
    if(x.s!==200) return;
    var t=x.j.tenantID||'default'; var u=x.j.userID||'local';
    var role=(x.j.roles&&x.j.roles.length)?x.j.roles.join('/'):'admin';
    var te=document.getElementById('idTenant'); if(te)te.textContent=t;
    var re=document.getElementById('idRole'); if(re)re.textContent=role;
    var chip=document.getElementById('identityChip'); if(chip)chip.title='身份由前置网关注入（X-Tenant / X-User / X-User-Roles）；当前：租户 '+t+' · 用户 '+u;
  }).catch(function(e){console.error('me',e);});
}

// ---------- 启动 ----------
window.onload=function(){
  loadAgents();pollDevices();pollTasks();pollAlerts();paintOverview();fetchMe();
  setInterval(pollDevices,5000);setInterval(pollTasks,5000);setInterval(pollAlerts,10000);
  document.getElementById('statusFilter').addEventListener('change',pollTasks);
  document.getElementById('taskForm').addEventListener('submit',function(e){
    e.preventDefault();
    var body={
      agentID:document.getElementById('agentID').value,
      type:document.getElementById('type').value,
      command:document.getElementById('command').value,
      path:document.getElementById('path').value,
      content:document.getElementById('content').value
    };
    fetch('/api/v1/tasks',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)})
      .then(function(r){return r.json().then(function(j){return {s:r.status,j:j};});})
      .then(function(x){var el=document.getElementById('taskResult');el.className='msg '+(x.s<400?'ok':'err');el.textContent='['+x.s+'] '+JSON.stringify(x.j);pollTasks();pollDevices();})
      .catch(function(err){var el=document.getElementById('taskResult');el.className='msg err';el.textContent='error: '+err;});
  });
  var df=document.getElementById('deployForm');
  if(df)df.addEventListener('submit',function(e){
    e.preventDefault();
    var body={
      name:document.getElementById('dpName').value.trim(),
      type:document.getElementById('dpType').value,
      repo_url:document.getElementById('dpRepo').value.trim(),
      content:document.getElementById('dpContent').value,
      path:document.getElementById('dpPath').value.trim(),
      target_ids:document.getElementById('dpTargets').value.trim()
    };
    if(!body.name||!body.target_ids){deployMsg('请填写名称和至少一个目标设备',false);return;}
    fetch('/api/v1/deploys',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)})
      .then(function(r){return r.json().then(function(j){return{s:r.status,j:j};});})
      .then(function(x){deployMsg('['+x.s+'] '+(x.j.error||'已登记 #'+(x.j&&x.j.id)),x.s<400);if(x.s<400)pollDeploys();})
      .catch(function(err){deployMsg('error: '+err,false);});
  });
  setInterval(function(){var p=document.getElementById('tab-alerts');if(p&&p.classList.contains('active'))pollAlertsFull();},10000);
  setInterval(function(){var p=document.getElementById('tab-deploy');if(p&&p.classList.contains('active'))pollDeploys();},8000);
  // 画布：平移 / 滚轮缩放 / 快捷键（仅在作业编排 tab 激活时生效）
  var cv=document.getElementById('canvas');
  if(cv){cv.addEventListener('mousedown',svgMouseDown);cv.addEventListener('wheel',svgWheel,{passive:false});}
  document.addEventListener('keydown',flowKey);
};
function flowKey(e){
  var pf=document.getElementById('tab-flow');if(!pf||!pf.classList.contains('active'))return;
  var t=(document.activeElement&&document.activeElement.tagName)||'';
  if(t==='INPUT'||t==='SELECT'||t==='TEXTAREA')return;
  if(e.key==='Delete'||e.key==='Backspace'){
    if(selectedEdge){deleteEdge(selectedEdge.src,selectedEdge.dst);}
    else if(selectedNode){deleteNode(selectedNode);}
    e.preventDefault();
  }else if(e.key==='Escape'){
    if(linking)toggleLink();selectedEdge=null;selectedNode=null;renderFlow();
  }else if((e.ctrlKey||e.metaKey)&&(e.key==='z'||e.key==='Z')){
    if(e.shiftKey)redo();else undo();e.preventDefault();
  }else if((e.ctrlKey||e.metaKey)&&(e.key==='y'||e.key==='Y')){
    redo();e.preventDefault();
  }else if((e.ctrlKey||e.metaKey)&&(e.key==='s'||e.key==='S')){
    saveWorkflow();e.preventDefault();
  }
}
