package main

const indexHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>校园 VPN 分流助手</title>
  <style>
    :root { color-scheme: light; --ink:#17231d; --muted:#65716b; --paper:#f4f7f2; --card:#fff; --green:#176b45; --green2:#289667; --line:#dce5de; --warn:#a45b13; --bad:#a53b3b; }
    * { box-sizing:border-box }
    body { margin:0; min-height:100vh; font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","Microsoft YaHei",sans-serif; color:var(--ink); background:radial-gradient(circle at 8% 0,#ddefdf 0,transparent 36%),radial-gradient(circle at 100% 90%,#e8e1ca 0,transparent 35%),var(--paper); }
    main { width:min(760px,calc(100% - 32px)); margin:0 auto; padding:52px 0 36px; }
    .brand { display:flex; align-items:center; gap:14px; margin-bottom:26px; }
    .logo { width:48px; height:48px; display:grid; place-items:center; border-radius:15px; color:#fff; background:linear-gradient(145deg,var(--green2),var(--green)); box-shadow:0 10px 28px #176b4538; font-size:25px; }
    h1 { font-size:25px; margin:0 0 3px; letter-spacing:-.02em; }
    .subtitle { color:var(--muted); font-size:14px; }
    .card { background:#ffffffdc; border:1px solid #fff; border-radius:22px; padding:26px; box-shadow:0 18px 55px #26462d14; backdrop-filter:blur(14px); }
    label { display:block; font-size:14px; font-weight:650; margin-bottom:9px; }
    input { width:100%; padding:13px 14px; border:1px solid var(--line); border-radius:12px; background:#fbfcfb; font:15px ui-monospace,SFMono-Regular,Consolas,monospace; color:var(--ink); outline:none; }
    input:focus { border-color:var(--green2); box-shadow:0 0 0 3px #2896671b; }
    .hint { margin:8px 0 20px; color:var(--muted); font-size:12px; line-height:1.55; }
    .actions { display:flex; flex-wrap:wrap; gap:10px; }
    button { border:0; border-radius:12px; padding:12px 18px; cursor:pointer; font-size:14px; font-weight:650; transition:.16s ease; }
    button:hover { transform:translateY(-1px); }
    button:disabled { opacity:.55; cursor:wait; transform:none; }
    .primary { background:var(--green); color:#fff; box-shadow:0 7px 18px #176b4530; }
    .secondary { background:#eaf1eb; color:#29503b; }
    .ghost { background:transparent; color:var(--muted); border:1px solid var(--line); }
    #result { display:none; margin-top:20px; border-radius:14px; padding:15px 16px; font-size:14px; line-height:1.55; background:#eef5ef; border:1px solid #d4e5d8; }
    #result.bad { background:#fff0ee; border-color:#f1d2cf; color:var(--bad); }
    #result.warn { background:#fff7e8; border-color:#efdfc2; color:var(--warn); }
    .routes { margin-top:12px; padding-top:10px; border-top:1px solid currentColor; border-color:#00000012; display:grid; grid-template-columns:max-content 1fr; gap:6px 14px; font-size:12px; }
    .routes span:nth-child(odd) { color:var(--muted); }
    .steps { margin:18px 2px 0; padding:0; list-style:none; display:grid; gap:10px; color:var(--muted); font-size:13px; }
    .steps li { display:flex; gap:10px; align-items:flex-start; }
    .num { flex:0 0 23px; width:23px; height:23px; border-radius:50%; display:grid; place-items:center; background:#e6eee7; color:var(--green); font-size:12px; font-weight:700; }
    footer { margin-top:16px; color:#829088; font-size:11px; text-align:center; }
    @media(max-width:540px){ main{padding-top:28px}.card{padding:20px}.actions button{flex:1 1 42%} }
  </style>
</head>
<body>
<main>
  <div class="brand"><div class="logo">⇄</div><div><h1>校园 VPN 分流助手</h1><div class="subtitle">Campus Split VPN · 临时路由工具</div></div></div>
  <section class="card">
    <label for="targets">校园服务器地址或网段</label>
    <input id="targets" value="172.25.24.135" autocomplete="off" spellcheck="false">
    <div class="hint">多个地址用逗号分隔；也支持网段，例如 172.25.0.0/16。默认值来自当前项目说明。</div>
    <div class="actions">
      <button class="primary" onclick="applySplit()">一键分流</button>
      <button class="secondary" onclick="checkStatus()">重新检查</button>
      <button class="ghost" onclick="restoreRoutes()">恢复路由</button>
      <button class="ghost" onclick="quitApp()">关闭程序</button>
    </div>
    <div id="result"></div>
  </section>
  <ol class="steps">
    <li><span class="num">1</span><span>先关闭其他 VPN/代理，只连接校园 VPN（MotionPro）。</span></li>
    <li><span class="num">2</span><span>点击“一键分流”，同意管理员权限请求。</span></li>
    <li><span class="num">3</span><span>看到“分流完成”后，再开启你平时使用的其他 VPN/代理。</span></li>
  </ol>
  <footer>所有改动只作用于本机路由；断开 VPN 或重启通常也会恢复网络。</footer>
</main>
<script>
const token='__TOKEN__';
const result=document.getElementById('result');
const buttons=[...document.querySelectorAll('button')];
function busy(on){buttons.forEach(b=>b.disabled=on)}
function endpoint(name){return '/api/'+token+'/'+name}
function show(data){
  result.style.display='block'; result.className=data.ok?'':'bad';
  let html='<strong>'+escapeHtml(data.message||'操作完成')+'</strong>';
  if(data.details?.length) html+='<div>'+data.details.map(escapeHtml).join('<br>')+'</div>';
  const s=data.snapshot;
  if(s){
    html+='<div class="routes"><span>状态</span><b>'+escapeHtml(s.summary||'—')+'</b>';
    if(s.vpnInterface) html+='<span>校园 VPN</span><b>'+escapeHtml(s.vpnInterface)+' · '+escapeHtml(s.vpnGateway||'')+'</b>';
    if(s.localInterface) html+='<span>本地网络</span><b>'+escapeHtml(s.localInterface)+' · '+escapeHtml(s.localGateway||'')+'</b>';
    if(s.internetRoute) html+='<span>普通流量</span><b>'+escapeHtml(s.internetRoute)+'</b>';
    Object.entries(s.targetRoutes||{}).forEach(([k,v])=>html+='<span>'+escapeHtml(k)+'</span><b>'+escapeHtml(v)+'</b>');
    html+='</div>';
  }
  result.innerHTML=html;
}
function escapeHtml(v){return String(v).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]))}
async function call(name,method='GET'){
  busy(true); result.style.display='block'; result.className='warn'; result.textContent=name==='apply'?'正在请求管理员权限并调整路由…':'正在检查…';
  try{
    const targets=document.getElementById('targets').value;
    const options={method,headers:{'Content-Type':'application/json'}};
    let url=endpoint(name);
    if(method==='POST') options.body=JSON.stringify({targets}); else url+='?targets='+encodeURIComponent(targets);
    const response=await fetch(url,options); show(await response.json());
  }catch(e){show({ok:false,message:'请求失败：'+e.message})}finally{busy(false)}
}
function applySplit(){call('apply','POST')}
function checkStatus(){call('status')}
function restoreRoutes(){call('restore','POST')}
async function quitApp(){await call('quit','POST');buttons.forEach(b=>b.disabled=true)}
checkStatus();
</script>
</body>
</html>`
