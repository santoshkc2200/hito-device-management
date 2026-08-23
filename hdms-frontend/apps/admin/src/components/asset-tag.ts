const assetTagAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";
const assetTagRandomLength = 12;

export function generateAssetTag(categoryName?: string) {
  const prefix =
    categoryName?.replace(/[^a-z0-9]/gi, "").slice(0, 6).toUpperCase() || "DEVICE";
  const randomValues = new Uint8Array(assetTagRandomLength);
  globalThis.crypto.getRandomValues(randomValues);
  const suffix = Array.from(randomValues, (value) => assetTagAlphabet[value % assetTagAlphabet.length]).join("");
  return `${prefix}-${suffix}`;
}
