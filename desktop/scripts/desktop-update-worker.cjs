const { net } = require('electron');
const { createUpdater } = require('./desktop-update.cjs');
let updater;
process.parentPort.on('message', async ({ data }) => {
  const { id, command, args } = data;
  try {
    if (command === 'init') updater = createUpdater({ ...args[0], fetch: net.fetch.bind(net), publish: state => process.parentPort.postMessage({ state }) });
    const result = command === 'init' ? updater.status() : await updater[command](...args);
    process.parentPort.postMessage({ id, result });
  } catch { process.parentPort.postMessage({ id, error: '自动更新暂时不可用，当前应用和授权未改动' }); }
});
