// Web commands use the same authenticated APIs as their visible controls.
export const commands = [
  {name:'help', label:'使用帮助', description:'查看命令和输入快捷键'},
  {name:'new', label:'新对话', description:'开始新对话，保留当前历史'},
  {name:'model', label:'切换模型', description:'打开可搜索的模型列表'},
  {name:'workflow', label:'动态工作流', description:'追加任务，由 Agent 设计并执行；留空查看运行记录', args:true},
  {name:'sessions', label:'历史会话', description:'浏览和管理全部会话'},
  {name:'context', label:'会话信息', description:'查看当前模型和工作区', session:true},
  {name:'tools', label:'可用工具', description:'查看服务提供的工具'},
  {name:'compact', label:'压缩上下文', description:'总结较早消息，可追加保留要求', session:true, args:true},
  {name:'stop', label:'停止生成', description:'停止当前会话的任务'},
  {name:'settings', label:'设置', description:'飞书连接、配对与服务状态'},
];
export function parseCommand(text) {
  const match = text.trim().match(/^\/([a-z]+)(?:\s+([\s\S]*))?$/i);
  return match ? {name:match[1].toLowerCase(), args:(match[2] || '').trim()} : null;
}
export function filterCommands(text) {
  if (!/^\/[^\s/]*$/.test(text)) return null;
  const query = text.slice(1).toLocaleLowerCase();
  return commands.filter(c => `${c.name} ${c.label} ${c.description}`.toLocaleLowerCase().includes(query));
}
export class CommandMenu {
  constructor(input, onChange) {
    this.input = input; this.onChange = onChange;
    this.menu = document.getElementById('command-menu');
    this.list = document.getElementById('command-options');
    this.menu.addEventListener('pointerdown', e => e.preventDefault());
    this.list.addEventListener('click', e => { const row = e.target.closest('[data-command]'); if (row) this.choose(Number(row.dataset.command)); });
    input.addEventListener('blur', () => this.close());
    input.addEventListener('focus', () => this.update());
    const position = () => { if (!this.menu.hidden) this.position(); };
    window.addEventListener('resize', position);
    window.visualViewport?.addEventListener('resize', position);
    window.visualViewport?.addEventListener('scroll', position);
    new ResizeObserver(position).observe(input.closest('.input-container'));
  }
  update() {
    this.filtered = filterCommands(this.input.value);
    if (!this.filtered || document.activeElement !== this.input) { this.close(); return; }
    this.list.replaceChildren(); this.active = 0;
    for (const [index,c] of this.filtered.entries()) {
      const row = document.createElement('div'); row.className = 'command-option'; row.id = `command-${c.name}`; row.dataset.command = index;
      row.setAttribute('role','option');
      const name = document.createElement('span'); name.className = 'command-name'; name.textContent = '/' + c.name;
      const content = document.createElement('span'); content.className = 'command-description';
      const title = document.createElement('strong'); title.textContent = c.label;
      const detail = document.createElement('span'); detail.textContent = c.description;
      content.append(title,detail); row.append(name,content); this.list.append(row);
    }
    document.getElementById('command-empty').hidden = !!this.filtered.length;
    this.menu.hidden = false; this.input.setAttribute('aria-expanded','true'); this.position(); this.activate(0);
  }
  position() {
    const rect = this.input.closest('.input-container').getBoundingClientRect();
    const v = window.visualViewport;
    const left = v?.offsetLeft || 0, top = v?.offsetTop || 0, width = v?.width || innerWidth;
    this.menu.style.left = Math.max(left + 12, rect.left) + 'px';
    this.menu.style.width = Math.min(rect.width, width - 24) + 'px';
    this.menu.style.maxHeight = Math.max(0, rect.top - top - 20) + 'px';
    this.menu.style.top = rect.top - 8 + 'px';
  }
  activate(index) {
    this.active = index;
    [...this.list.children].forEach((row,i) => row.setAttribute('aria-selected',String(index === i)));
    const row = this.list.children[index];
    if (row) { this.input.setAttribute('aria-activedescendant',row.id); row.scrollIntoView({block:'nearest'}); }
    else this.input.removeAttribute('aria-activedescendant');
  }
  choose(index = this.active) {
    const command = this.filtered?.[index]; if (!command) return;
    this.input.value = '/' + command.name + ' '; this.input.focus(); this.close(); this.onChange();
  }
  keydown(e) {
    if (e.isComposing || e.keyCode === 229 || this.menu.hidden) return false;
    if (e.key === 'Escape') { e.preventDefault(); this.close(); return true; }
    if (['ArrowUp','ArrowDown'].includes(e.key)) {
      e.preventDefault(); if (this.filtered.length) this.activate((this.active + (e.key === 'ArrowDown' ? 1 : -1) + this.filtered.length) % this.filtered.length); return true;
    }
    if ((e.key === 'Tab' && !e.shiftKey || e.key === 'Enter' && !e.shiftKey) && this.filtered.length) {
      e.preventDefault(); this.choose(); return true;
    }
    if (e.key === 'Tab') this.close();
    return false;
  }
  close() { this.menu.hidden = true; this.input.setAttribute('aria-expanded','false'); this.input.removeAttribute('aria-activedescendant'); }
}
