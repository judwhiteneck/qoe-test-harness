package dashboard

// indexHTML is the engineer view: a working-latency CDF overlay (LL vs classic)
// with cohort filters and a runs table. Vanilla JS, no build step, no CDN — it
// fetches /api/cdf and /api/runs with the current filters. Kept deliberately
// plain; visual polish is a /design-review pass once the data shape is proven.
const indexHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>LLD/L4S Engineer View</title>
<style>
  :root { --ll:#1f77b4; --classic:#d62728; --ink:#1a1a1a; --muted:#666; --line:#ddd; }
  body { font:14px/1.5 system-ui,sans-serif; color:var(--ink); margin:0; padding:24px; }
  h1 { font-size:18px; margin:0 0 4px; }
  .sub { color:var(--muted); margin:0 0 20px; }
  .filters { display:flex; gap:8px; flex-wrap:wrap; align-items:end; margin-bottom:16px; }
  .filters label { display:flex; flex-direction:column; font-size:12px; color:var(--muted); }
  .filters input, .filters select { font:14px system-ui; padding:5px 7px; border:1px solid var(--line); border-radius:6px; }
  button { font:14px system-ui; padding:6px 14px; border:1px solid var(--ink); background:var(--ink); color:#fff; border-radius:6px; cursor:pointer; }
  .legend { display:flex; gap:16px; margin:8px 0; font-size:13px; }
  .legend span { display:inline-flex; align-items:center; gap:6px; }
  .swatch { width:12px; height:12px; border-radius:2px; display:inline-block; }
  .summary { color:var(--muted); margin:6px 0 18px; }
  table { border-collapse:collapse; width:100%; font-size:13px; }
  th, td { text-align:left; padding:6px 10px; border-bottom:1px solid var(--line); }
  th { color:var(--muted); font-weight:600; }
  .v-pass { color:#1a7f37; font-weight:600; }
  .v-fail { color:#cf222e; font-weight:600; }
  .v-inconclusive { color:#9a6700; font-weight:600; }
  svg { background:#fff; border:1px solid var(--line); border-radius:8px; }
</style>
</head>
<body>
<h1>LLD/L4S Engineer View</h1>
<p class="sub">Working-latency CDF (deviation from idle), merged across the selected cohort. Low-latency traffic should hug the left; classic spreads right under load.</p>

<div class="filters">
  <label>ISP <input id="f-isp" placeholder="any"></label>
  <label>Region <input id="f-region" placeholder="any"></label>
  <label>Device
    <select id="f-device"><option value="">any</option><option>cli</option><option>android</option><option>ios</option></select>
  </label>
  <label>Verdict
    <select id="f-verdict"><option value="">any</option><option>pass</option><option>fail</option><option>inconclusive</option></select>
  </label>
  <button onclick="refresh()">Refresh</button>
</div>

<div class="legend">
  <span><i class="swatch" style="background:var(--ll)"></i> low-latency</span>
  <span><i class="swatch" style="background:var(--classic)"></i> classic</span>
  <span style="color:var(--muted)">dashed = 10 ms pass line</span>
</div>

<svg id="chart" width="760" height="380" viewBox="0 0 760 380" role="img" aria-label="CDF overlay"></svg>
<p class="summary" id="summary">Loading…</p>

<table>
  <thead><tr><th>Run</th><th>Started</th><th>ISP</th><th>Region</th><th>Device</th><th>Verdict</th><th>Base RTT</th><th>Marking</th></tr></thead>
  <tbody id="rows"></tbody>
</table>

<script>
const W=760,H=380,P={l:48,r:16,t:16,b:36};
const MS_MIN=0.5, MS_MAX=1000;
function xlog(ms){ ms=Math.max(MS_MIN,Math.min(MS_MAX,ms));
  const f=(Math.log10(ms)-Math.log10(MS_MIN))/(Math.log10(MS_MAX)-Math.log10(MS_MIN));
  return P.l + f*(W-P.l-P.r); }
function y(p){ return P.t + (1-p)*(H-P.t-P.b); }
function q(){ const g=id=>document.getElementById(id).value.trim();
  const p=new URLSearchParams();
  for(const [k,id] of [["isp","f-isp"],["region","f-region"],["device","f-device"],["verdict","f-verdict"]]){
    const v=g(id); if(v) p.set(k,v); }
  return p.toString(); }
function path(pts){ if(!pts||!pts.length) return "";
  // step CDF: rise vertically at each bin's upper edge
  let d="M "+xlog(MS_MIN)+" "+y(0); let prevY=y(0);
  for(const pt of pts){ const X=xlog(pt.ms); d+=" L "+X+" "+prevY+" L "+X+" "+y(pt.p); prevY=y(pt.p); }
  d+=" L "+xlog(MS_MAX)+" "+prevY; return d; }
function axes(svg){ for(const ms of [1,10,100,1000]){ const X=xlog(ms);
    svg.innerHTML+='<line x1="'+X+'" y1="'+P.t+'" x2="'+X+'" y2="'+(H-P.b)+'" stroke="#eee"/>';
    svg.innerHTML+='<text x="'+X+'" y="'+(H-P.b+16)+'" text-anchor="middle" font-size="11" fill="#666">'+ms+'ms</text>'; }
  for(const p of [0,0.25,0.5,0.75,1]){ const Y=y(p);
    svg.innerHTML+='<line x1="'+P.l+'" y1="'+Y+'" x2="'+(W-P.r)+'" y2="'+Y+'" stroke="#f3f3f3"/>';
    svg.innerHTML+='<text x="'+(P.l-6)+'" y="'+(Y+3)+'" text-anchor="end" font-size="11" fill="#666">'+p.toFixed(2)+'</text>'; }
  const X10=xlog(10);
  svg.innerHTML+='<line x1="'+X10+'" y1="'+P.t+'" x2="'+X10+'" y2="'+(H-P.b)+'" stroke="#999" stroke-dasharray="4 3"/>'; }
async function refresh(){
  const qs=q();
  const cdf=await (await fetch("/api/cdf"+(qs?"?"+qs:""))).json();
  const svg=document.getElementById("chart"); svg.innerHTML=""; axes(svg);
  svg.innerHTML+='<path d="'+path(cdf.classic)+'" fill="none" stroke="var(--classic)" stroke-width="2"/>';
  svg.innerHTML+='<path d="'+path(cdf.ll)+'" fill="none" stroke="var(--ll)" stroke-width="2"/>';
  document.getElementById("summary").textContent =
    cdf.runs+" run(s) merged · "+cdf.samples_ll+" LL samples · "+cdf.samples_classic+" classic samples";
  const list=await (await fetch("/api/runs"+(qs?"?"+qs:""))).json();
  const tb=document.getElementById("rows"); tb.innerHTML="";
  for(const r of (list.runs||[])){ const tr=document.createElement("tr");
    const dt=new Date(r.started_at_unix_ms).toISOString().replace("T"," ").slice(0,19);
    tr.innerHTML='<td>'+r.run_id+'</td><td>'+dt+'</td><td>'+(r.isp||"")+'</td><td>'+(r.region||"")+
      '</td><td>'+(r.device_class||"")+'</td><td class="v-'+r.verdict+'">'+r.verdict+
      '</td><td>'+r.base_rtt_ms.toFixed(2)+' ms</td><td>'+(r.marking_survival*100).toFixed(1)+'%</td>';
    tb.appendChild(tr); }
}
refresh();
</script>
</body>
</html>`
