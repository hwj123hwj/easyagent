const { signAsync } = require("@electron/osx-sign");
const path = require("node:path");
const { configuration, requirement, withKeychain, verify } = require("./macos-signing.cjs");

// Local updates reuse a persistent certificate. Public CI defaults to ad hoc.
module.exports = async function sign(options) {
  const config = options.config || configuration();
  const app = path.resolve(options.app);
  const helper = path.join(app, "Contents/Resources/cua-helper/EasyAgent Computer Use.app");
  verify(helper, "com.easyagent.cua-helper", config);
  await withKeychain(config, () => signAsync({
    ...options,
    config: undefined,
    identity: config.identity,
    keychain: config.keychain,
    identityValidation: false,
    preAutoEntitlements: false,
    preEmbedProvisioningProfile: false,
    // This installed osx-sign version maps true to invalid --strict=true.
    // Omission retains its default --strict verification.
    strictVerify: undefined,
    // Keep the independently signed permission helper intact, as ZCode does.
    ignore: (file) => path.resolve(file) === helper || path.resolve(file).startsWith(helper + path.sep),
    optionsForFile: (file) => ({
      ...options.optionsForFile?.(file),
      hardenedRuntime: false,
      timestamp: "none",
      ...(config.mode !== "adhoc" && path.resolve(file) === app
        ? { requirements: "=designated => " + requirement("com.easyagent.desktop", config) }
        : {}),
    }),
  }));
  verify(app, "com.easyagent.desktop", config);
};
