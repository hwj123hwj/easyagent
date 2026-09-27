// Per-tab drafts survive refresh; storage failure retains an in-memory copy.
export class Drafts {
  constructor(storage) { this.storage = storage; this.memory = new Map(); }
  get(id) {
    if (this.memory.has(id)) return this.memory.get(id);
    try { return this.storage?.getItem('ea.draft.' + id) || ''; } catch { return ''; }
  }
  set(id, text) {
    this.memory.set(id, text);
    try { if (text) this.storage?.setItem('ea.draft.' + id, text); else this.storage?.removeItem('ea.draft.' + id); } catch { /* Memory still retains the draft. */ }
  }
  delete(id) { this.set(id, ''); }
}
