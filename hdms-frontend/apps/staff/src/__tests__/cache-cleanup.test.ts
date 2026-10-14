/// <reference types="node" />
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { runInNewContext } from "node:vm";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/service-worker/sw.js"), "utf8");

function runWorker() {
  const scope = "https://staging.example/staff/";
  const handlers: Record<string, (event: { waitUntil: (work: Promise<unknown>) => void }) => void> = {};
  const deleted: string[] = [];
  const navigated: string[] = [];
  let unregistered = false;
  let skippedWaiting = false;
  runInNewContext(source, {
    URL,
    self: {
      addEventListener: (name: string, handler: typeof handlers[string]) => { handlers[name] = handler; },
      skipWaiting: async () => { skippedWaiting = true; },
      registration: { scope, unregister: async () => { unregistered = true; } },
      caches: {
        keys: async () => ["workbox-precache-v2-" + scope, "workbox-precache-v2-https://staging.example/", "unrelated-cache"],
        delete: async (name: string) => { deleted.push(name); },
      },
      clients: {
        matchAll: async () => ["https://staging.example/staff/login", "https://staging.example/admin/users"].map((url) => ({
          url, navigate: async (target: string) => { navigated.push(target); },
        })),
      },
    },
  });
  return { handlers, deleted, navigated, get unregistered() { return unregistered; }, get skippedWaiting() { return skippedWaiting; } };
}

describe("staging staff cache cleanup", () => {
  it("activates immediately to replace an existing caching worker", async () => {
    const worker = runWorker();
    let work: Promise<unknown> | undefined;
    expect(worker.handlers.install).toBeTypeOf("function");
    worker.handlers.install({ waitUntil: (promise) => { work = promise; } });
    await work;
    expect(worker.skippedWaiting).toBe(true);
  });

  it("removes only staff caches, unregisters, and reloads only staff windows", async () => {
    const worker = runWorker();
    let work: Promise<unknown> | undefined;
    expect(worker.handlers.activate).toBeTypeOf("function");
    worker.handlers.activate({ waitUntil: (promise) => { work = promise; } });
    expect(work).toBeDefined();
    await work;
    expect(worker.deleted).toEqual(["workbox-precache-v2-https://staging.example/staff/"]);
    expect(worker.unregistered).toBe(true);
    expect(worker.navigated).toEqual(["https://staging.example/staff/login"]);
    expect(worker.handlers.fetch).toBeUndefined();
  });
});
