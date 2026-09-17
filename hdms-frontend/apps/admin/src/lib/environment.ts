export function isStagingEnvironment(): boolean {
  if (typeof import.meta === "undefined" || !import.meta.env) {
    return false;
  }
  const env = (import.meta.env.VITE_ENV || import.meta.env.MODE || "").toLowerCase();
  const stagingFlag = import.meta.env.VITE_STAGING === "true";
  return env === "staging" || stagingFlag;
}
