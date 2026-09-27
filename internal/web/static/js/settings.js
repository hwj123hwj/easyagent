import { api } from './api.js';

export class SettingsPage {
  constructor() {
    this.form = document.getElementById('feishu-form');
    this.notice = document.getElementById('feishu-notice');
    this.id = document.getElementById('feishu-app-id');
    this.secret = document.getElementById('feishu-app-secret');
    this.pairing = document.getElementById('feishu-pairing');
    this.form.onsubmit = event => { event.preventDefault(); this.save(); };
    document.getElementById('settings-refresh').onclick = () => this.activate();
    document.getElementById('feishu-show-pairing').onclick = () => this.showPairing();
    document.getElementById('feishu-copy-pairing').onclick = async () => {
      const field = document.getElementById('feishu-pair-command');
      try { await navigator.clipboard.writeText(field.value); this.message('配对指令已复制，请私聊机器人发送。'); }
      catch { field.focus(); field.select(); this.message('指令已选中，请按 Ctrl/Cmd+C 复制。'); }
    };
  }
  message(text, error = false) {
    this.notice.textContent = text;
    this.notice.classList.toggle('is-error', error);
  }
  busy(value) {
    this.form.querySelectorAll('input,button').forEach(el => { el.disabled = value; });
    document.getElementById('settings-refresh').disabled = value;
    document.getElementById('feishu-show-pairing').disabled = value;
  }
  render(data) {
    const managed = data.managed;
    document.getElementById('feishu-unmanaged').hidden = managed;
    this.form.hidden = !managed;
    document.getElementById('feishu-access').hidden = !managed;
    document.getElementById('service-status').hidden = !managed;
    if (!managed) return;
    this.id.value = data.app_id || '';
    this.id.readOnly = Boolean(data.app_id);
    this.secret.value = '';
    this.secret.placeholder = data.secret_configured ? '已保存；留空保留现有密钥' : '输入应用密钥';
    this.secret.required = !data.secret_configured;
    document.getElementById('feishu-owner-status').textContent = data.pairing_unavailable ? '配对状态暂不可用。保存配置启动桥接后，再刷新查看。' : data.paired ? '已绑定使用者。只有该使用者的消息和卡片操作会被接受。' : '尚未配对。机器人在配对完成前不会执行开发指令。';
    document.getElementById('feishu-show-pairing').hidden = data.paired || data.pairing_unavailable;
    const labels = {active:'运行中',inactive:'未运行',failed:'启动失败',activating:'启动中',deactivating:'正在停止',unknown:'状态未知'};
    for (const name of ['core','bridge']) {
      const service = data.services[name];
      document.getElementById(`service-${name}`).textContent = labels[service.state] || service.state;
      document.getElementById(`autostart-${name}`).textContent = service.autostart === 'enabled' ? '已启用' : service.autostart === 'disabled' ? '未启用' : '待确认';
    }
    document.getElementById('service-boot-note').textContent = data.linger === 'yes' ? '无需登录主机，开机即可启动已启用的服务。' : '主机尚未确认开启免登录启动，请检查用户服务设置。';
  }
  async activate() {
    this.hidePairing(); this.busy(true); this.message('正在读取配置…');
    try { this.render(await api.get('/settings/feishu')); this.message(''); }
    catch (error) { this.message(`加载失败：${error.message}`, true); }
    finally { this.busy(false); }
  }
  async save() {
    this.hidePairing(); this.busy(true); this.message('正在保存并重启飞书桥接…');
    try {
      this.render(await api.put('/settings/feishu', {app_id:this.id.value.trim(),app_secret:this.secret.value.trim()}));
      this.message('已保存并重启桥接。长连接正在建立；请稍后刷新状态。');
    } catch (error) { this.message(`保存失败：${error.message}`, true); }
    finally { this.secret.value = ''; this.busy(false); }
  }
  hidePairing() {
    this.pairing.hidden = true;
    document.getElementById('feishu-pair-command').value = '';
  }
  async showPairing() {
    const button = document.getElementById('feishu-show-pairing'); button.disabled = true;
    try {
      const data = await api.get('/settings/feishu/pairing');
      document.getElementById('feishu-pair-command').value = data.command;
      document.getElementById('feishu-pair-expiry').textContent = `有效至 ${new Date(data.expires).toLocaleString()}，仅可使用一次。请勿发到群聊。`;
      this.pairing.hidden = false; this.message('');
    } catch (error) { this.message(error.message, true); }
    finally { button.disabled = false; }
  }
  deactivate() { this.hidePairing(); this.secret.value = ''; }
}
