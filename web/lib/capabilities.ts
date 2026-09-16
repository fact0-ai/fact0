/** Capabilities of this single-owner release, independent of subscription plans. */
export const capabilities = Object.freeze({
  audit: true,
  executionReplay: true,
  codingSessions: true,
  billing: false,
  spotlight: false,
  sharing: false,
  governance: false,
});
