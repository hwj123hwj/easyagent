import { InMemoryJournalStore } from '@zcode/dynamic-workflow';
import { openSync, readFileSync, writeSync, fsyncSync, closeSync, ftruncateSync } from 'node:fs';

// The upstream engine owns state transitions. Persist its port mutations before
// returning, so successful asks cannot be acknowledged before their journal.
export function openJournal(path) {
  const base = new InMemoryJournalStore();
  const timestamps = new Map();
  const fd = openSync(path, 'a+', 0o600);
  try {
    const data = readFileSync(path), end = data.lastIndexOf(10) + 1;
    if (end < data.length) ftruncateSync(fd, end); // only an incomplete final WAL record
    for (const line of data.subarray(0, end).toString().split('\n').filter(Boolean)) {
      const { method, args, timeCreated } = JSON.parse(line);
      if (!writes.has(method)) throw Error('invalid journal operation');
      const value = base[method](...args);
      if (method === 'appendEvent') timestamps.set(`${args[0]}:${value.sequence}`, timeCreated ?? value.timeCreated);
    }
  } catch (err) {
    closeSync(fd);
    throw err;
  }
  const port = new Proxy(base, { get(target, key) {
    const fn = target[key];
    if (typeof fn !== 'function') return fn;
    return (...args) => {
      const value = fn.apply(target, args);
      if (writes.has(key)) {
        const timeCreated = key === 'appendEvent' ? value.timeCreated : undefined;
        const data = Buffer.from(JSON.stringify({ method: key, args, timeCreated }) + '\n');
        let offset = 0;
        while (offset < data.length) offset += writeSync(fd, data, offset, data.length - offset);
        fsyncSync(fd);
        if (key === 'appendEvent') timestamps.set(`${args[0]}:${value.sequence}`, timeCreated);
      }
      if (key === 'listEvents') return value.map(event => ({
        ...event, timeCreated: timestamps.get(`${args[0]}:${event.sequence}`) ?? event.timeCreated,
      }));
      return value;
    };
  } });
  return { port, close: () => closeSync(fd) };
}
const writes = new Set(['createRun', 'updateRunStatus', 'updateRunUsage', 'updateRunCaps', 'putActor', 'putNode', 'appendEvent']);
