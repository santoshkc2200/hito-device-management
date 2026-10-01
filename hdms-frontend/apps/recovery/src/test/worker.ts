import { vi } from "vitest";

export type Reply = { status?: number; body?: unknown; headers?: Record<string, string> };

/**
 * A fake /recovery/api. Each "METHOD /path" (path without the /recovery/api
 * prefix) maps to one reply, or to a queue of replies that is consumed in
 * order with the last one repeating.
 */
export function fakeWorker(routes: Record<string, Reply | Reply[]>) {
  const calls: { route: string; body: unknown }[] = [];
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = new URL(String(input), "https://hdms.test");
    const route = `${init?.method ?? "GET"} ${url.pathname.replace(/^\/recovery\/api/, "")}`;
    const body = init?.body ? JSON.parse(String(init.body)) : undefined;
    calls.push({ route, body });
    const entry = routes[route];
    if (!entry) return new Response(JSON.stringify({ error: "not_found" }), { status: 404 });
    const reply = Array.isArray(entry) ? (entry.length > 1 ? entry.shift()! : entry[0]) : entry;
    return new Response(JSON.stringify(reply.body ?? {}), {
      status: reply.status ?? 200,
      headers: { "Content-Type": "application/json", ...reply.headers },
    });
  });
  return { calls, called: (route: string) => calls.filter((c) => c.route === route) };
}
