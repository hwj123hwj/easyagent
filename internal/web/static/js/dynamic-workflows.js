import { api } from './api.js';
import { renderMarkdown } from './markdown.js';
const names={running:'执行中',completed:'已完成',failed:'失败',errored:'失败',cancelled:'已取消',stopped:'已停止',interrupted:'等待恢复'};
const label=status=>names[status] || status;
function el(tag,css,text){const node=document.createElement(tag);node.className=css;if(text!==undefined)node.textContent=text;return node;}
export class DynamicWorkflowsPage {
  constructor(state){
    this.state=state;this.list=document.getElementById('dw-list');this.detail=document.getElementById('dw-detail');this.notice=document.getElementById('dw-notice');
    document.getElementById('dw-create').onclick=()=>state.composeWorkflow();
    document.getElementById('dw-refresh').onclick=()=>this.refresh();
  }
  activate(){this.active=true;this.refresh();clearInterval(this.timer);this.timer=setInterval(()=>this.refresh(),2000);}
  deactivate(){this.active=false;this.revision=(this.revision||0)+1;clearInterval(this.timer);}
  async refresh(){
    if(this.loading)return;
    this.loading=true;const revision=this.revision;
    try{
      const data=await api.get('/dynamic-workflows');if(!this.active||revision!==this.revision)return;
      this.notice.textContent=data.error||'';
      const signature=JSON.stringify(data.runs.map(r=>[r.id,r.status,r.name]));
      if(signature!==this.signature){
        const focusedRun=this.list.contains(document.activeElement)?document.activeElement.dataset.id:null;
        this.signature=signature;this.list.replaceChildren();
        for(const run of data.runs){const button=el('button','dw-run');button.dataset.id=run.id;button.append(el('span','dw-run-name',run.name),el('span','dw-run-status',label(run.status)));button.onclick=()=>this.select(run.id);this.list.append(button);}
        if(focusedRun)[...this.list.children].find(n=>n.dataset.id===focusedRun)?.focus({preventScroll:true});
        if(!data.runs.length)this.list.append(el('p','dw-empty','还没有动态工作流。可以先试试：分别研究技术、成本与风险，再汇总成报告。'));
      }
      if(this.selected){const id=this.selected,run=await api.get('/dynamic-workflows/'+encodeURIComponent(id));if(this.active&&revision===this.revision&&this.selected===id)this.render(run);}
      this.markSelected();
    }catch(error){if(this.active)this.notice.textContent='读取失败：'+error.message;}finally{this.loading=false;}
  }
  async select(id){
    this.selected=id;this.detailSignature='';this.openNodes=[];this.markSelected();
    this.detail.replaceChildren(el('p','dw-empty','正在读取工作流…'));
    try{const run=await api.get('/dynamic-workflows/'+encodeURIComponent(id));if(this.active&&this.selected===id)this.render(run);}catch(error){if(this.selected===id)this.detail.replaceChildren(el('p','dw-empty','读取失败：'+error.message));}
  }
  markSelected(){for(const row of this.list.children)if(row.dataset.id){row.classList.toggle('selected',row.dataset.id===this.selected);row.setAttribute('aria-pressed',String(row.dataset.id===this.selected));}}
  render(run){
    const signature=JSON.stringify(run);if(signature===this.detailSignature)return;this.detailSignature=signature;
    const scroll=this.detail.scrollTop,scriptOpen=this.detail.querySelector('.dw-script')?.open;
    const focusAction=this.detail.contains(document.activeElement)?document.activeElement.dataset.action:null;
    const header=el('div','panel-header');header.append(el('h3','',run.name));
    const action=el('button','btn btn-ghost',run.status==='running'?'取消运行':'恢复运行');action.dataset.action='run';
    action.disabled=this.actionBusy;action.hidden=!['running','cancelled','interrupted','stopped'].includes(run.status);action.onclick=()=>this.control(run);
    header.append(action);this.detail.replaceChildren(header,el('p','dw-meta',`${label(run.status)} · ${run.id}`));
    if(run.error)this.detail.append(el('p','dw-error',run.error));
    if(['stopped','cancelled','interrupted'].includes(run.status))this.detail.append(el('p','dw-intro','恢复会复用已完成的结果；未完成的任务可能再次执行，请先查看 Actor 会话中的操作记录。'));
    const actors=run.state?.actors||[],nodes=run.state?.nodes||[];
    const actorNames=new Map(actors.map(a=>[`${a.siteId}@${a.ordinal}`,a.name||a.siteId]));
    this.detail.append(el('h4','','执行节点'));
    const nodeList=el('div','dw-nodes');
    if(!nodes.length)nodeList.append(el('p','dw-empty',run.status==='running'?'正在编译脚本、准备 Actor…':'暂无执行节点'));
    for(const node of nodes){const row=el('details','dw-node'),summary=el('summary','');summary.textContent=`${actorNames.get(`${node.actorSiteId}@${node.actorOrdinal}`)||node.kind} · ${node.siteId} / ${node.ordinal} · ${label(node.status)}`;summary.dataset.action='node-'+node.siteId+'-'+node.ordinal;row.append(summary);if(node.result!==undefined)row.append(el('pre','',typeof node.result==='string'?node.result:JSON.stringify(node.result,null,2)));if(node.error)row.append(el('pre','dw-error',node.error.message||JSON.stringify(node.error)));nodeList.append(row);}
    // Preserve user-expanded node details while status snapshots change.
    const openNodes=new Set(this.openNodes||[]);nodeList.addEventListener('toggle',()=>{this.openNodes=[...nodeList.children].flatMap((n,i)=>n.open?[i]:[]);},true);
    [...nodeList.children].forEach((n,i)=>{if(openNodes.has(i))n.open=true;});this.detail.append(nodeList);
    const sessions=el('div','dw-sessions');
    for(const [key,id] of Object.entries(run.sessions||{})){const button=el('button','btn btn-ghost',`查看 ${actorNames.get(key)||key} 会话`);button.dataset.action='session-'+id;button.onclick=async()=>{this.state.navigate('page-chat');await this.state.selectSession(id);};sessions.append(button);}
    this.detail.append(sessions);
    if(run.result!==undefined&&run.result!==null){this.detail.append(el('h4','','最终产出'));const result=el('div','markdown-body');result.innerHTML=renderMarkdown(typeof run.result==='string'?run.result:'```json\n'+JSON.stringify(run.result,null,2)+'\n```');this.detail.append(result);}
    const script=el('details','dw-script');script.open=!!scriptOpen;const scriptSummary=el('summary','','工作流脚本');scriptSummary.dataset.action='script';script.append(scriptSummary,el('pre','',run.script));this.detail.append(script);
    this.detail.scrollTop=scroll;
    if(focusAction)[...this.detail.querySelectorAll('[data-action]')].find(n=>n.dataset.action===focusAction)?.focus({preventScroll:true});
  }
  async control(run){
    if(this.actionBusy)return;
    const resume=run.status!=='running';
    if(resume&&!window.confirm('恢复会复用已完成结果，但未完成任务中的外部操作可能再次执行。已检查 Actor 会话，继续恢复？'))return;
    this.actionBusy=true;const button=this.detail.querySelector('[data-action="run"]');if(button)button.disabled=true;
    try{await api.post(`/dynamic-workflows/${encodeURIComponent(run.id)}/${resume?'resume':'cancel'}`,resume?{acknowledge_incomplete_actions:true}:{});this.notice.textContent=resume?'已请求恢复':'已请求取消';this.detailSignature='';await this.refresh();}catch(error){this.notice.textContent='操作失败：'+error.message;}finally{this.actionBusy=false;if(button?.isConnected)button.disabled=false;this.detailSignature='';}
  }
}
