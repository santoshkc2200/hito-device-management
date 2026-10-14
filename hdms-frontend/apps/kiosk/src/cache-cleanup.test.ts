/// <reference types="node" />
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { runInNewContext } from "node:vm";
import { describe, expect, it } from "vitest";

function runWorker() {
  const source = readFileSync(resolve(process.cwd(), "src/service-worker/sw.js"), "utf8");
  const scope = "https://staging.example/";
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
        keys: async () => [
          "workbox-precache-v2-" + scope,
          "workbox-precache-v2-https://staging.example/staff/",
          "workbox-precache-v2-https://staging.example/admin/",
          "unrelated-cache",
        ],
        delete: async (name: string) => { deleted.push(name); },
      },
      clients: {
        matchAll: async () => [
          scope, scope + "index.html", scope + "staff/login", scope + "admin/users",
          scope + "recovery/", scope + "staff", scope + "admin", scope + "recovery",
        ].map((url) => ({ url, navigate: async (target: string) => { navigated.push(target); } })),
      },
    },
  });
  return { handlers, deleted, navigated, get unregistered() { return unregistered; }, get skippedWaiting() { return skippedWaiting; } };
}

describe("staging kiosk cache cleanup", () => {
  it("activates immediately to replace an existing caching worker", async () => {
    const worker = runWorker();
    let work: Promise<unknown> | undefined;
    worker.handlers.install({ waitUntil: (promise) => { work = promise; } });
    await work;
    expect(worker.skippedWaiting).toBe(true);
  });

  it("removes only kiosk caches, unregisters, and reloads only kiosk windows", async () => {
    const worker = runWorker();
    let work: Promise<unknown> | undefined;
    worker.handlers.activate({ waitUntil: (promise) => { work = promise; } });
    expect(work).toBeDefined();
    await work;
    expect(worker.deleted).toEqual(["workbox-precache-v2-https://staging.example/"]);
    expect(worker.unregistered).toBe(true);
    expect(worker.navigated).toEqual(["https://staging.example/", "https://staging.example/index.html"]);
    expect(worker.handlers.fetch).toBeUndefined();
  });
});
