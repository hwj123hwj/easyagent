// Visual-check harness for SkillMarketPanel: renders the real panel against a
// mocked market API and captures PNGs, without needing the Go backend.
//
// Usage (from desktop/):
//   npx esbuild preview/harness.tsx --bundle --outfile=preview/dist/harness.js \
//     --define:process.env.NODE_ENV='"production"' --loader:.css=css --jsx=automatic
//   ./node_modules/.bin/electron preview/shot.js [route ...]
// Routes: dark (market, dark theme), installed, skeleton, dark-skeleton.
// PNGs land in preview/shots/<route>.png.
const { app, BrowserWindow } = require("electron");
const { writeFile, mkdir } = require("node:fs/promises");
const path = require("node:path");

const ROUTES = process.argv.slice(2).length ? process.argv.slice(2) : ["dark", "installed", "skeleton"];
const log = (...a) => console.log("[shot]", ...a);

app.whenReady().then(async () => {
  const win = new BrowserWindow({
    show: false,
    width: 1180,
    height: 1240,
    webPreferences: { offscreen: true, contextIsolation: true, backgroundThrottling: false },
  });
  win.webContents.on("console-message", (_e, level, message) => log("page:", message));
  await mkdir(path.join(__dirname, "shots"), { recursive: true });
  for (const route of ROUTES) {
    log("loading", route);
    await win.loadFile(path.join(__dirname, "index.html"), { search: `?route=${route}` });
    log("loaded", route);
    // Wait until real (non-skeleton) content exists, or 6s max, then settle.
    await win.webContents.executeJavaScript(
      `new Promise((resolve) => {
        const t0 = Date.now();
        const check = () => {
          const real = document.querySelectorAll('.skill-market-card:not(.skel)').length;
          const detail = !!document.querySelector('.skill-market-detail');
          const empty = !!document.querySelector('.skill-market-empty');
          const detailSettled = detail && Date.now() - t0 > 1500;
          if ((real > 0 && Date.now() - t0 > 900) || detailSettled || empty || Date.now() - t0 > 6000) {
            setTimeout(() => resolve(JSON.stringify({ real, detail, empty, ms: Date.now() - t0 })), 400);
          } else setTimeout(check, 100);
        };
        check();
      })`,
    ).then((s) => log("settled", route, s));
    const img = await win.webContents.capturePage();
    await writeFile(path.join(__dirname, "shots", `${route}.png`), img.toPNG());
    log("captured", route, img.getSize());
  }
  app.quit();
});
