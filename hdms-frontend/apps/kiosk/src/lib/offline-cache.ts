export const CACHE_STALENESS_BOUND_MS = 5 * 60 * 1000; // 5 minutes
export const STALENESS_BOUND_MS = CACHE_STALENESS_BOUND_MS;
export const OFFLINE_CACHE_STALENESS_BOUND_MS = CACHE_STALENESS_BOUND_MS;

export const DEVICE_CACHE_KEY = "hdms_offline_cached_devices";
export const USER_CACHE_KEY = "hdms_offline_cached_users";

export interface CacheEntry<T> {
  data: T;
  cachedAt: number;
  ageMs: number;
  isFresh: boolean;
  isStale: boolean;
  isLastKnown: boolean;
}

export interface StoredEntry<T> {
  data: T;
  cachedAt: number;
}

export interface CachedDevice {
  id: string;
  assetTag: string;
  name: string;
  status: string;
  categoryId?: string;
  condition?: string;
  [key: string]: unknown;
}

export interface CachedUser {
  id: string;
  fullName: string;
  department?: string;
  employeeNo?: string;
  openLoanCount?: number;
  [key: string]: unknown;
}

function loadMapFromStorage<T>(key: string): Map<string, StoredEntry<T>> {
  const map = new Map<string, StoredEntry<T>>();
  try {
    const raw = localStorage.getItem(key);
    if (!raw) return map;
    const obj = JSON.parse(raw) as Record<string, StoredEntry<T>>;
    for (const [k, v] of Object.entries(obj)) {
      if (v && typeof v.cachedAt === "number" && v.data) {
        map.set(k, v);
      }
    }
  } catch {
    // Ignore storage parse errors
  }
  return map;
}

function saveMapToStorage<T>(key: string, map: Map<string, StoredEntry<T>>): void {
  try {
    const obj: Record<string, StoredEntry<T>> = {};
    for (const [k, v] of map.entries()) {
      obj[k] = v;
    }
    localStorage.setItem(key, JSON.stringify(obj));
  } catch {
    // Ignore storage quota errors
  }
}

let deviceMemoryCache: Map<string, StoredEntry<CachedDevice>> | null = null;
let userMemoryCache: Map<string, StoredEntry<CachedUser>> | null = null;

function getDeviceCache(): Map<string, StoredEntry<CachedDevice>> {
  if (!deviceMemoryCache) {
    deviceMemoryCache = loadMapFromStorage<CachedDevice>(DEVICE_CACHE_KEY);
  }
  return deviceMemoryCache;
}

function getUserCache(): Map<string, StoredEntry<CachedUser>> {
  if (!userMemoryCache) {
    userMemoryCache = loadMapFromStorage<CachedUser>(USER_CACHE_KEY);
  }
  return userMemoryCache;
}

export function cacheDevice(
  device: CachedDevice,
  cachedAt = Date.now()
): void {
  const cache = getDeviceCache();
  const entry: StoredEntry<CachedDevice> = {
    data: device,
    cachedAt,
  };
  if (device.id) {
    cache.set(device.id, entry);
  }
  if (device.assetTag) {
    cache.set(device.assetTag, entry);
  }
  saveMapToStorage(DEVICE_CACHE_KEY, cache);
}

export function getCachedDevice(
  idOrAssetTag: string,
  now = Date.now()
): CacheEntry<CachedDevice> | null {
  const cache = getDeviceCache();
  const stored = cache.get(idOrAssetTag);
  if (!stored) return null;

  const ageMs = Math.max(0, now - stored.cachedAt);
  const isFresh = ageMs <= CACHE_STALENESS_BOUND_MS;

  return {
    data: stored.data,
    cachedAt: stored.cachedAt,
    ageMs,
    isFresh,
    isStale: !isFresh,
    isLastKnown: true,
  };
}

export function cacheUser(
  user: CachedUser,
  cachedAt = Date.now()
): void {
  const cache = getUserCache();
  const entry: StoredEntry<CachedUser> = {
    data: user,
    cachedAt,
  };
  if (user.id) {
    cache.set(user.id, entry);
  }
  if (user.employeeNo) {
    cache.set(user.employeeNo, entry);
  }
  saveMapToStorage(USER_CACHE_KEY, cache);
}

export function getCachedUser(
  idOrEmployeeNo: string,
  now = Date.now()
): CacheEntry<CachedUser> | null {
  const cache = getUserCache();
  const stored = cache.get(idOrEmployeeNo);
  if (!stored) return null;

  const ageMs = Math.max(0, now - stored.cachedAt);
  const isFresh = ageMs <= CACHE_STALENESS_BOUND_MS;

  return {
    data: stored.data,
    cachedAt: stored.cachedAt,
    ageMs,
    isFresh,
    isStale: !isFresh,
    isLastKnown: true,
  };
}

export function clearOfflineCache(): void {
  deviceMemoryCache = new Map();
  userMemoryCache = new Map();
  try {
    localStorage.removeItem(DEVICE_CACHE_KEY);
    localStorage.removeItem(USER_CACHE_KEY);
  } catch {
    // Ignore
  }
}
