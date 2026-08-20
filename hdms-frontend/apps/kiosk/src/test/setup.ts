import "@testing-library/jest-dom/vitest";
import * as matchers from "vitest-axe/matchers";
import { afterEach, expect } from "vitest";

expect.extend(matchers);

const storeMap = new WeakMap<Storage, Map<string, string>>();

function getStore(storage: Storage): Map<string, string> {
  let store = storeMap.get(storage);
  if (!store) {
    store = new Map<string, string>();
    storeMap.set(storage, store);
  }
  return store;
}

if (typeof Storage !== "undefined") {
  Storage.prototype.getItem = function (key: string) {
    return getStore(this).get(key) ?? null;
  };
  Storage.prototype.setItem = function (key: string, value: string) {
    getStore(this).set(key, String(value));
  };
  Storage.prototype.removeItem = function (key: string) {
    getStore(this).delete(key);
  };
  Storage.prototype.clear = function () {
    getStore(this).clear();
  };
  Storage.prototype.key = function (index: number) {
    return Array.from(getStore(this).keys())[index] ?? null;
  };
  Object.defineProperty(Storage.prototype, "length", {
    get: function () {
      return getStore(this).size;
    },
    configurable: true,
  });
}

function createStorage(): Storage {
  return Object.create(Storage.prototype) as Storage;
}

const mockLocal = createStorage();
const mockSession = createStorage();

Object.defineProperty(globalThis, "localStorage", {
  value: mockLocal,
  writable: true,
  configurable: true,
});

Object.defineProperty(globalThis, "sessionStorage", {
  value: mockSession,
  writable: true,
  configurable: true,
});



if (typeof window !== "undefined") {
  Object.defineProperty(window, "localStorage", {
    value: mockLocal,
    writable: true,
    configurable: true,
  });
  Object.defineProperty(window, "sessionStorage", {
    value: mockSession,
    writable: true,
    configurable: true,
  });
}

afterEach(() => {
  mockLocal.clear();
  mockSession.clear();
});


