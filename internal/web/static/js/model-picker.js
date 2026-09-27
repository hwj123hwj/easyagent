export const modelKey = model => `${model.provider}/${model.id}`;
export function filterModels(models, query) {
  const terms = query.toLocaleLowerCase().trim().split(/\s+/).filter(Boolean);
  return models.filter(model => terms.every(term => `${model.name || ''} ${model.id} ${model.provider}`.toLocaleLowerCase().includes(term)));
}

// A bounded, themed picker: the OS menu cannot inherit the app's surfaces.
export class ModelPicker {
  constructor(button, onSelect) {
    this.button = button;
    this.onSelect = onSelect;
    this.models = [];
    this.menu = document.createElement('section');
    this.menu.id = 'model-menu';
    this.menu.className = 'model-menu';
    this.menu.hidden = true;
    this.menu.setAttribute('role', 'dialog');
    this.menu.setAttribute('aria-label', '选择模型');
    this.menu.innerHTML = '<div class="model-menu-heading"><span>选择模型</span><span class="model-count" role="status"></span></div><input class="model-search" type="search" role="combobox" aria-label="搜索模型" aria-autocomplete="list" aria-expanded="true" aria-controls="model-options" placeholder="搜索名称或服务商…" autocomplete="off"><div id="model-options" class="model-options" role="listbox" aria-label="可用模型"></div><p class="model-empty" hidden>没有匹配的模型，试试其他关键词</p><div class="model-menu-hint">↑ ↓ 选择 · Enter 确认 · Esc 关闭</div>';
    document.body.append(this.menu);
    this.search = this.menu.querySelector('input');
    this.list = this.menu.querySelector('.model-options');
    button.onclick = () => this.menu.hidden ? this.open() : this.close();
    button.onkeydown = event => {
      if (event.key === 'ArrowDown' || event.key === 'ArrowUp') { event.preventDefault(); this.open(); }
    };
    this.search.oninput = () => this.render();
    this.menu.addEventListener('keydown', event => {
      if (event.isComposing) return;
      if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); this.close(); }
      else if (event.key === 'Tab') this.close();
      else if (['ArrowDown', 'ArrowUp', 'Enter'].includes(event.key)) {
        event.preventDefault();
        if (!this.filtered.length) return;
        if (event.key === 'Enter') this.choose(this.filtered[this.active]);
        else this.activate((this.active + (event.key === 'ArrowDown' ? 1 : -1) + this.filtered.length) % this.filtered.length);
      }
    });
    document.addEventListener('pointerdown', event => {
      if (!this.menu.contains(event.target) && !button.contains(event.target)) this.close(false);
    });
    const reposition = () => { if (!this.menu.hidden) this.position(); };
    window.addEventListener('resize', reposition);
    window.visualViewport?.addEventListener('resize', reposition);
    window.visualViewport?.addEventListener('scroll', reposition);
    this.observer = new MutationObserver(() => { if (button.disabled) this.close(false); });
    this.observer.observe(button, {attributes:true, attributeFilter:['disabled']});
  }
  setModels(models, selected) {
    this.models = [...models];
    this.setSelected(selected);
  }
  setSelected(selected) {
    this.selected = selected;
    if (selected?.id && !this.models.some(model => modelKey(model) === modelKey(selected))) this.models.unshift(selected);
    const label = this.models.find(model => selected && modelKey(model) === modelKey(selected));
    document.getElementById('model-selected-name').textContent = label?.name || label?.id || (this.models.length ? '选择模型' : '暂无可用模型');
    this.button.title = label ? `${label.provider} / ${label.id}` : '';
  }
  open() {
    if (this.button.disabled || !this.models.length) return;
    this.search.value = '';
    this.menu.hidden = false;
    this.button.setAttribute('aria-expanded', 'true');
    this.position();
    this.render();
    this.search.focus();
  }
  position() {
    const rect = this.button.getBoundingClientRect();
    const viewport = window.visualViewport;
    const left = viewport?.offsetLeft || 0;
    const top = viewport?.offsetTop || 0;
    const right = left + (viewport?.width || innerWidth);
    const bottom = top + (viewport?.height || innerHeight);
    const width = Math.min(360, right - left - 24);
    this.menu.style.width = `${width}px`;
    this.menu.style.left = `${Math.max(left + 12, Math.min(rect.left, right - width - 12))}px`;
    const anchorTop = Math.min(rect.top, bottom);
    const anchorBottom = Math.max(rect.bottom, top);
    const above = anchorTop - top - 20;
    const below = bottom - anchorBottom - 20;
    const up = above >= below;
    this.menu.style.top = up ? 'auto' : `${anchorBottom + 8}px`;
    this.menu.style.bottom = up ? `${innerHeight - anchorTop + 8}px` : 'auto';
    this.menu.style.maxHeight = `${Math.max(120, up ? above : below)}px`;
  }
  close(restore = true) {
    if (this.menu.hidden) return;
    this.menu.hidden = true;
    this.button.setAttribute('aria-expanded', 'false');
    if (restore && !this.button.disabled) this.button.focus();
  }
  render() {
    this.filtered = filterModels(this.models, this.search.value);
    this.list.replaceChildren();
    this.menu.querySelector('.model-count').textContent = `${this.filtered.length} 个模型`;
    this.menu.querySelector('.model-empty').hidden = this.filtered.length > 0;
    this.filtered.forEach((model, index) => {
      const option = document.createElement('div');
      option.className = 'model-option'; option.id = `model-option-${index}`;
      option.setAttribute('role', 'option');
      const selected = !!this.selected && modelKey(model) === modelKey(this.selected);
      option.setAttribute('aria-selected', String(selected));
      const text = document.createElement('span'); text.className = 'model-option-copy';
      const name = document.createElement('span'); name.className = 'model-option-name'; name.textContent = model.name || model.id;
      const provider = document.createElement('span'); provider.className = 'model-option-provider'; provider.textContent = model.provider;
      text.append(name, provider); option.append(text);
      if (selected) option.insertAdjacentHTML('beforeend', '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true"><path d="m5 12 4 4 10-10"/></svg>');
      option.onclick = () => this.choose(model);
      option.onpointermove = () => this.activate(index, false);
      this.list.append(option);
    });
    const selected = this.filtered.findIndex(model => this.selected && modelKey(model) === modelKey(this.selected));
    this.activate(Math.max(0, selected));
  }
  activate(index, scroll = true) {
    this.active = index;
    [...this.list.children].forEach((item, i) => item.classList.toggle('active', i === index));
    const option = this.list.children[index];
    if (option) {
      this.search.setAttribute('aria-activedescendant', option.id);
      if (scroll) option.scrollIntoView({block:'nearest'});
    } else this.search.removeAttribute('aria-activedescendant');
  }
  choose(model) { this.close(); this.onSelect(model); }
}
