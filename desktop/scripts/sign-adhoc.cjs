const { signAsync } = require("@electron/osx-sign");

// Seal the complete test app without implying Developer ID trust or notarization.
module.exports = async function sign(options) {
  await signAsync({
    ...options,
    identity: "-",
    identityValidation: false,
    preAutoEntitlements: false,
    preEmbedProvisioningProfile: false,
    // This installed osx-sign version maps true to invalid --strict=true.
    // Omission retains its default --strict verification.
    strictVerify: undefined,
    optionsForFile: (file) => ({
      ...options.optionsForFile?.(file),
      hardenedRuntime: false,
      timestamp: "none",
    }),
  });
};
