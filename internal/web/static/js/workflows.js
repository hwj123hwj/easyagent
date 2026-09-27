// 工作流（流水线）页：极简表单（默认）/ YAML 高级模式、模板、运行列表、详情、审批
//
// 表单的心智模型："把几句话按顺序交给 AI，后面的可以看到前面的结果"。
// id、变量、模板语法全部由系统生成，不出现在界面上。

import { api } from './api.js';

const ACTIVE_STATUSES = new Set(['running', 'waiting_approval']);

const EXAMPLE_YAML = `name: demo-pipeline
vars:
  topic: Go context
steps:
  - id: outline
    prompt: "为「{{vars.topic}}」写一份 5 点研究大纲"
  - id: expand
    foreach: ["要点1", "要点2", "要点3"]
    concurrency: 2
    retries: 1
    prompt: "展开要点「{{item}}」，给出 3 条细节"
    depends_on: [outline]
  - id: merge
    prompt: "把以下内容整理成短文：\\n{{steps.expand.output}}"
    depends_on: [expand]
  - id: publish
    prompt: "最终确认并输出全文：\\n{{steps.merge.output}}"
    confirm: true
    depends_on: [expand, merge]
`;

// 内置模板：一键载入，改几个字就能跑
const TEMPLATES = [
  {
    label: '研究报告',
    form: {
      name: '研究报告',
      autoLink: true,
      steps: [
        { prompt: '围绕「在此输入研究主题」列出研究大纲', foreach: '', confirm: false },
        { prompt: '深入研究该维度：要点、证据与结论', foreach: '技术路线, 成本, 风险', confirm: false },
        { prompt: '把以上研究内容整理成一份结构化报告', foreach: '', confirm: false },
        { prompt: '通读全文，输出最终定稿', foreach: '', confirm: true },
      ],
    },
  },
  {
    label: '批量翻译',
    form: {
      name: '批量翻译',
      autoLink: true,
      steps: [
        { prompt: '把内容翻译成英文，只输出译文', foreach: '第一段原文, 第二段原文, 第三段原文', confirm: false },
        { prompt: '按顺序合并以上译文，不做额外说明', foreach: '', confirm: false },
      ],
    },
  },
  {
    label: '内容创作',
    form: {
      name: '内容创作',
      autoLink: true,
      steps: [
        { prompt: '为主题「在此输入主题」写一篇短文初稿', foreach: '', confirm: false },
        { prompt: '审阅上面的文章，直接给出修改后的全文', foreach: '', confirm: false },
        { prompt: '定稿发布', foreach: '', confirm: true },
      ],
    },
  },
];

// ─── 表单 → YAML：接线全自动，用户只提供每句话 ─────────────────────────────

function yamlScalar(s) {
  s = String(s ?? '');
  if (s === '') return '""';
  if (/^[A-Za-z0-9_.\- ]+$/.test(s) && s.trim() === s) return s;
  return JSON.stringify(s);
}

function formToYAML(f) {
  const lines = [`name: ${yamlScalar(f.name || '流水线')}`, 'steps:'];
  f.steps.forEach((s, i) => {
    const id = 'step' + (i + 1);
    let prompt = s.prompt.trim();
    const fe = (s.foreach || '').trim();
    if (fe) {
      prompt += '\n\n（当前处理项：{{item}}）';
    }
    if (f.autoLink && i > 0) {
      prompt += `\n\n--- 上一步的输出 ---\n{{steps.step${i}.output}}`;
    }
    lines.push(`  - id: ${id}`);
    lines.push(`    prompt: ${JSON.stringify(prompt)}`);
    if (fe) {
      if (!fe.includes('{{') && !fe.includes('\n') && fe.includes(',')) {
        const items = fe.split(',').map(x => x.trim()).filter(Boolean);
        lines.push(`    foreach: [${items.map(yamlScalar).join(', ')}]`);
      } else {
        lines.push(`    foreach: ${yamlScalar(fe)}`);
      }
    }
    if (s.confirm) lines.push('    confirm: true');
  });
  return lines.join('\n') + '\n';
}

// ─── DOM 助手 ────────────────────────────────────────────────────────────────

function el(tag, className, text) {
  const e = document.createElement(tag);
  if (className) e.className = className;
  if (text !== undefined) e.textContent = text;
  return e;
}

// ─── 页面 ────────────────────────────────────────────────────────────────────

export class WorkflowsPage {
  constructor(state) {
    this.state = state;
    this.mode = 'form';
    this.runs = [];
    this.currentRunID = null;
    this.pollTimer = null;
    this.listTimer = null;
    this.form = TEMPLATES[0] ? this.fromTemplate(TEMPLATES[0]) : this.blankForm();

    this.$yaml = document.getElementById('wf-yaml');
    this.$preview = document.getElementById('wf-yaml-preview');
    this.$form = document.getElementById('wf-form');
    this.$submit = document.getElementById('wf-submit-btn');
    this.$submitMsg = document.getElementById('wf-submit-msg');
    this.$runList = document.getElementById('wf-run-list');
    this.$detail = document.getElementById('wf-detail');
    this.$events = document.getElementById('wf-events');
    this.$steps = document.getElementById('wf-steps');
    this.$outputs = document.getElementById('wf-outputs');

    document.getElementById('wf-mode-form').onclick = () => this.setMode('form');
    document.getElementById('wf-mode-yaml').onclick = () => this.setMode('yaml');
    document.getElementById('wf-preview-btn').onclick = () => this.togglePreview();

    const chips = document.getElementById('wf-template-chips');
    for (const t of TEMPLATES) {
      const chip = el('button', 'chip', t.label);
      chip.onclick = () => { this.form = this.fromTemplate(t); this.renderForm(); this.hidePreview(); };
      chips.appendChild(chip);
    }

    this.$submit.onclick = () => this.submit();
    document.getElementById('wf-refresh-btn').onclick = () => this.loadRuns();
    document.getElementById('wf-close-btn').onclick = () => this.closeDetail();
    document.getElementById('wf-approve-btn').onclick = () => this.gate('/approve');
    document.getElementById('wf-reject-btn').onclick = () => this.gate('/reject');
    document.getElementById('wf-cancel-btn').onclick = () => this.cancelRun();

    this.renderForm();
  }

  fromTemplate(t) {
    return { name: t.form.name, autoLink: t.form.autoLink, steps: JSON.parse(JSON.stringify(t.form.steps)) };
  }

  blankForm() {
    return {
      name: '我的流水线',
      autoLink: true,
      steps: [{ prompt: '', foreach: '', confirm: false }, { prompt: '', foreach: '', confirm: false }],
    };
  }

  setMode(mode) {
    this.mode = mode;
    document.getElementById('wf-mode-form').classList.toggle('active', mode === 'form');
    document.getElementById('wf-mode-yaml').classList.toggle('active', mode === 'yaml');
    this.$form.hidden = mode !== 'form';
    this.$yaml.hidden = mode !== 'yaml';
    if (mode === 'yaml' && !this.$yaml.value.trim()) {
      this.$yaml.value = formToYAML(this.form);
    }
    this.hidePreview();
  }

  togglePreview() {
    if (!this.$preview.hidden) { this.hidePreview(); return; }
    this.$preview.textContent = this.currentYAML();
    this.$preview.hidden = false;
    document.getElementById('wf-preview-btn').textContent = '收起';
  }

  hidePreview() {
    this.$preview.hidden = true;
    document.getElementById('wf-preview-btn').textContent = '预览 YAML';
  }

  currentYAML() {
    return this.mode === 'form' ? formToYAML(this.form) : this.$yaml.value;
  }

  // ── 表单渲染 ──────────────────────────────────────────────────────────

  renderForm() {
    this.$form.innerHTML = '';
    const f = this.form;

    // 顶部：名称 + 自动衔接开关
    const top = el('div', 'wf-top');
    const nameWrap = el('label', 'wf-top-name');
    const nameInput = el('input', 'wf-input');
    nameInput.placeholder = '流水线名称';
    nameInput.value = f.name;
    nameInput.oninput = () => { f.name = nameInput.value; this.hidePreview(); };
    nameWrap.appendChild(nameInput);
    top.appendChild(nameWrap);

    const linkWrap = el('label', 'wf-autolink');
    const link = el('input');
    link.type = 'checkbox';
    link.checked = f.autoLink;
    link.onchange = () => { f.autoLink = link.checked; this.hidePreview(); };
    linkWrap.append(link, el('span', null, '自动衔接上一步结果'));
    linkWrap.title = '开启后，每一步自动参考上一步的输出，不需要写任何语法';
    top.appendChild(linkWrap);
    this.$form.appendChild(top);

    // 步骤流水线
    f.steps.forEach((s, i) => {
      this.$form.appendChild(this.stepCard(s, i));
    });

    const addStep = el('button', 'wf-add-step', '＋ 添加一步');
    addStep.onclick = () => { f.steps.push({ prompt: '', foreach: '', confirm: false }); this.renderForm(); };
    this.$form.appendChild(addStep);
  }

  stepCard(s, i) {
    const f = this.form;
    const card = el('div', 'wf-step-card');

    const head = el('div', 'wf-step-head');
    head.append(el('span', 'wf-step-num', String(i + 1)));
    head.append(el('span', 'wf-step-title', i === 0 ? '开始' : '然后'));
    const btns = el('div', 'wf-step-btns');
    const moveUp = el('button', 'wf-icon-btn', '↑');
    moveUp.title = '上移';
    moveUp.disabled = i === 0;
    moveUp.onclick = () => { [f.steps[i - 1], f.steps[i]] = [f.steps[i], f.steps[i - 1]]; this.renderForm(); };
    const moveDown = el('button', 'wf-icon-btn', '↓');
    moveDown.title = '下移';
    moveDown.disabled = i === f.steps.length - 1;
    moveDown.onclick = () => { [f.steps[i + 1], f.steps[i]] = [f.steps[i], f.steps[i + 1]]; this.renderForm(); };
    const del = el('button', 'wf-icon-btn', '×');
    del.title = '删除';
    del.disabled = f.steps.length <= 1;
    del.onclick = () => { f.steps.splice(i, 1); this.renderForm(); };
    btns.append(moveUp, moveDown, del);
    head.appendChild(btns);
    card.appendChild(head);

    const prompt = el('textarea', 'wf-input wf-prompt');
    prompt.placeholder = i === 0
      ? '让 AI 做什么？例如：围绕「远程办公」写一份提纲'
      : '然后让 AI 做什么？会自动参考上一步的结果';
    prompt.value = s.prompt;
    prompt.rows = 2;
    prompt.oninput = () => { s.prompt = prompt.value; this.hidePreview(); };
    card.appendChild(prompt);

    const opts = el('div', 'wf-step-opts');
    const feWrap = el('label', 'wf-opt');
    feWrap.append(el('span', 'wf-opt-label', '批量'), el('span', 'wf-opt-hint', '逗号分隔，每项跑一次'));
    const fe = el('input', 'wf-input wf-opt-fe');
    fe.placeholder = '不批量';
    fe.value = s.foreach;
    fe.oninput = () => { s.foreach = fe.value; this.hidePreview(); };
    feWrap.appendChild(fe);
    opts.appendChild(feWrap);

    const confirmWrap = el('label', 'wf-opt wf-opt-check');
    const confirm = el('input');
    confirm.type = 'checkbox';
    confirm.checked = s.confirm;
    confirm.onchange = () => { s.confirm = confirm.checked; this.hidePreview(); };
    confirmWrap.append(confirm, el('span', 'wf-opt-label', '这步完成后停下来等我确认'));
    opts.appendChild(confirmWrap);

    card.appendChild(opts);
    return card;
  }

  validateForm() {
    if (!this.form.steps.length) return '至少需要一步';
    for (let i = 0; i < this.form.steps.length; i++) {
      if (!this.form.steps[i].prompt.trim()) return `第 ${i + 1} 步还没写内容`;
    }
    return null;
  }

  // ── 提交 ─────────────────────────────────────────────────────────────

  async submit() {
    const yaml = this.currentYAML();
    if (!yaml.trim()) {
      this.$submitMsg.textContent = '请先填写流水线';
      return;
    }
    if (this.mode === 'form') {
      const err = this.validateForm();
      if (err) {
        this.$submitMsg.textContent = err;
        return;
      }
    }
    this.$submit.disabled = true;
    this.$submitMsg.textContent = '';
    try {
      const res = await api.postRaw('/workflows', yaml, 'text/yaml');
      this.$submitMsg.textContent = '已启动 ✓';
      await this.loadRuns();
      this.showRun(res.run_id);
    } catch (e) {
      this.$submitMsg.textContent = '失败：' + e.message;
    } finally {
      this.$submit.disabled = false;
    }
  }

  // ── 运行列表与详情 ────────────────────────────────────────────────────

  activate() {
    this.loadRuns();
    clearInterval(this.listTimer);
    this.listTimer = setInterval(() => this.loadRuns(), 5000);
  }

  deactivate() {
    clearInterval(this.listTimer);
  }

  async loadRuns() {
    let runs;
    try {
      const res = await api.get('/workflows');
      runs = res.runs || [];
    } catch (e) {
      return; // 401 已由 api 层处理
    }
    this.runs = runs;
    this.$runList.innerHTML = '';
    if (!runs.length) {
      this.$runList.innerHTML = '<div class="wf-empty">还没有运行记录<br><span>从左边选个模板，改几个字就能跑</span></div>';
      return;
    }
    for (const run of runs) {
      const row = el('div', 'wf-run-row' + (run.id === this.currentRunID ? ' selected' : ''));
      row.innerHTML = `
        <span class="badge badge-${run.status}">${run.status}</span>
        <span class="wf-run-name">${escapeHTML(run.name)}</span>
        <span class="wf-run-time">${formatTime(run.started_at)}</span>`;
      row.onclick = () => this.showRun(run.id);
      this.$runList.appendChild(row);
    }
  }

  async showRun(runID) {
    this.currentRunID = runID;
    this.$detail.hidden = false;
    await this.refreshRun();
    clearInterval(this.pollTimer);
    this.pollTimer = setInterval(() => this.refreshRun(), 1500);
  }

  closeDetail() {
    this.currentRunID = null;
    this.$detail.hidden = true;
    clearInterval(this.pollTimer);
    this.loadRuns();
  }

  async refreshRun() {
    if (!this.currentRunID) return;
    let meta, events;
    try {
      const res = await api.get('/workflows/' + this.currentRunID);
      meta = res.meta;
      events = res.events || [];
    } catch (e) {
      if (e.status === 404) this.closeDetail();
      return;
    }

    document.getElementById('wf-detail-title').textContent = `${meta.name}`;
    this.renderActions(meta.status);
    this.renderSteps(meta);
    this.renderOutputs(meta.outputs || {});
    this.renderEvents(events);

    if (!ACTIVE_STATUSES.has(meta.status)) {
      clearInterval(this.pollTimer);
    }
  }

  renderActions(status) {
    const waiting = status === 'waiting_approval';
    const active = ACTIVE_STATUSES.has(status);
    document.getElementById('wf-approve-btn').hidden = !waiting;
    document.getElementById('wf-reject-btn').hidden = !waiting;
    document.getElementById('wf-cancel-btn').hidden = !active;
  }

  renderSteps(meta) {
    const order = Object.keys(meta.steps || {});
    this.$steps.innerHTML = '';
    for (const id of order) {
      const s = meta.steps[id];
      const items = s.items_total ? ` (${s.items_done}/${s.items_total})` : '';
      const row = el('div', 'wf-step-row');
      row.innerHTML = `
        <span class="badge badge-${s.status}">${s.status}${items}</span>
        <span class="wf-step-id">${stepLabel(id)}</span>
        ${s.cached ? '<span class="wf-tag">cached</span>' : ''}
        ${s.error ? `<span class="wf-step-error">${escapeHTML(s.error)}</span>` : ''}`;
      this.$steps.appendChild(row);
    }
  }

  renderOutputs(outputs) {
    this.$outputs.innerHTML = '';
    const ids = Object.keys(outputs);
    if (!ids.length) {
      this.$outputs.innerHTML = '<div class="wf-empty">暂无产出</div>';
      return;
    }
    for (const id of ids) {
      const block = document.createElement('details');
      block.className = 'wf-output-block';
      const summary = document.createElement('summary');
      summary.textContent = '第 ' + stepLabel(id) + ' 步' + (outputs[id].cached ? '（缓存）' : '');
      const pre = document.createElement('pre');
      pre.textContent = outputs[id].output || '';
      block.append(summary, pre);
      this.$outputs.appendChild(block);
    }
  }

  renderEvents(events) {
    const important = events.filter(e =>
      ['run_start', 'step_end', 'item_end', 'step_error', 'gate_wait', 'gate_result', 'run_end'].includes(e.event));
    this.$events.innerHTML = '';
    for (const e of important.slice(-200)) {
      const line = el('div', 'wf-event-line wf-event-' + e.event);
      const parts = [formatTime(e.ts), e.event, e.step ? stepLabel(e.step) : ''];
      if (e.item !== undefined && e.item !== null) parts.push('#' + (e.item + 1));
      if (e.error) parts.push('⚠ ' + e.error);
      line.textContent = parts.filter(Boolean).join('  ');
      this.$events.appendChild(line);
    }
    this.$events.scrollTop = this.$events.scrollHeight;
  }

  async gate(action) {
    if (!this.currentRunID) return;
    try {
      await api.post('/workflows/' + this.currentRunID + action, {});
      await this.refreshRun();
    } catch (e) {
      alert('操作失败：' + e.message);
    }
  }

  async cancelRun() {
    if (!this.currentRunID) return;
    try {
      await api.post('/workflows/' + this.currentRunID + '/cancel', {});
      await this.refreshRun();
    } catch (e) {
      alert('取消失败：' + e.message);
    }
  }
}

// stepLabel: 引擎 id（step1…）→ 用户视角"第 N 步"
function stepLabel(id) {
  const m = /^step(\d+)$/.exec(id);
  return m ? m[1] : id;
}

function escapeHTML(s) {
  return String(s ?? '').replace(/[&<>"']/g, c => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
  }[c]));
}

function formatTime(iso) {
  if (!iso) return '';
  const d = new Date(iso);
  return isNaN(d) ? '' : d.toLocaleTimeString();
}
