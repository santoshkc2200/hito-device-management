import { createHmac } from "node:crypto";
import { execSync } from "node:child_process";
import type { Page } from "@playwright/test";

// Local development uses mkcert or self-signed certs for HTTPS
process.env.NODE_TLS_REJECT_UNAUTHORIZED = "0";

export interface TestUser {
  id: string;
  employeeNo: string;
  fullName: string;
  departmentId?: string;
  departmentName?: string;
  token: string;
  credentialId: string;
}

export interface TestDevice {
  id: string;
  assetTag: string;
  name: string;
  categoryId: string;
  categoryName?: string;
  token: string;
  credentialId: string;
}

export interface TestKiosk {
  id: string;
  name: string;
  token: string;
  defaultLocale?: string;
}

/**
 * RFC 6238 TOTP token generator for base32 secrets.
 */
export function generateTotp(secretBase32: string, time = Date.now()): string {
  // Crockford / standard RFC 4648 Base32 decode
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
  const cleanSecret = secretBase32.replace(/[\s-]/g, "").toUpperCase();
  let bits = 0;
  let value = 0;
  const bytes: number[] = [];

  for (const char of cleanSecret) {
    const idx = alphabet.indexOf(char);
    if (idx === -1) continue;
    value = (value << 5) | idx;
    bits += 5;
    if (bits >= 8) {
      bytes.push((value >>> (bits - 8)) & 255);
      bits -= 8;
    }
  }

  const epoch = Math.floor(time / 1000);
  const counter = Math.floor(epoch / 30);
  const buf = Buffer.alloc(8);
  buf.writeBigUInt64BE(BigInt(counter));

  const hmac = createHmac("sha1", Buffer.from(bytes));
  hmac.update(buf);
  const digest = hmac.digest();

  const offset = digest[digest.length - 1] & 0x0f;
  const code =
    ((digest[offset] & 0x7f) << 24) |
    ((digest[offset + 1] & 0xff) << 16) |
    ((digest[offset + 2] & 0xff) << 8) |
    (digest[offset + 3] & 0xff);

  const otp = code % 1000000;
  return otp.toString().padStart(6, "0");
}

let counter = Date.now();
function nextSeq(): number {
  counter += 1;
  return counter;
}

let sharedSessionCookie: string | null = null;
let sharedCsrfToken: string | null = null;
let loginPromise: Promise<void> | null = null;

export class TestApiClient {
  private baseUrl: string;
  private sessionCookie: string | null = sharedSessionCookie;
  private csrfToken: string | null = sharedCsrfToken;

  constructor(baseUrl = process.env.HDMS_API_URL || "https://localhost:8443") {
    this.baseUrl = baseUrl.replace(/\/+$/, "");
  }

  async login(
    email = "admin@example.org",
    password = "correct horse battery staple",
    totpSecret = process.env.HDMS_TEST_ADMIN_TOTP_SECRET || "VIPF7BGMNRBOPVSVYOIAG33Q5NHWVOZ7"
  ): Promise<void> {
    if (sharedSessionCookie) {
      this.sessionCookie = sharedSessionCookie;
      this.csrfToken = sharedCsrfToken;
      return;
    }

    if (!loginPromise) {
      loginPromise = (async () => {
        const totpCode = totpSecret ? generateTotp(totpSecret) : undefined;
        const body: Record<string, string> = { email, password };
        if (totpCode) {
          body.totpCode = totpCode;
        }

        const res = await fetch(`${this.baseUrl}/v1/auth/login`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body),
        });

        if (!res.ok) {
          const errText = await res.text();
          throw new Error(`Login failed with status ${res.status}: ${errText}`);
        }

        const setCookies =
          typeof (res.headers as any).getSetCookie === "function"
            ? (res.headers as any).getSetCookie().join("; ")
            : res.headers.get("set-cookie") || "";

        const sessionMatch = setCookies.match(/hdms_session=([^;,\s]+)/);
        const csrfMatch = setCookies.match(/hdms_csrf=([^;,\s]+)/);

        const cookies: string[] = [];
        if (sessionMatch) cookies.push(`hdms_session=${sessionMatch[1]}`);
        if (csrfMatch) {
          cookies.push(`hdms_csrf=${csrfMatch[1]}`);
          sharedCsrfToken = csrfMatch[1];
        }
        sharedSessionCookie = cookies.join("; ");
      })().finally(() => {
        loginPromise = null;
      });
    }

    await loginPromise;
    this.sessionCookie = sharedSessionCookie;
    this.csrfToken = sharedCsrfToken;
  }

  private async request(path: string, init?: RequestInit): Promise<Response> {
    if (!this.sessionCookie && !sharedSessionCookie && !path.includes("/auth/login")) {
      await this.login();
    }
    if (!this.sessionCookie && sharedSessionCookie) {
      this.sessionCookie = sharedSessionCookie;
      this.csrfToken = sharedCsrfToken;
    }

    const headers = new Headers(init?.headers);
    if (this.sessionCookie) {
      headers.set("Cookie", this.sessionCookie);
    }
    if (this.csrfToken && init?.method && init.method !== "GET" && init.method !== "HEAD") {
      headers.set("X-CSRF-Token", this.csrfToken);
    }
    if (!headers.has("Content-Type") && init?.body) {
      headers.set("Content-Type", "application/json");
    }

    const url = path.startsWith("http") ? path : `${this.baseUrl}${path.startsWith("/") ? "" : "/"}${path}`;
    return fetch(url, {
      ...init,
      headers,
    });
  }

  async seedDepartment(name?: string): Promise<{ id: string; name: string }> {
    const listRes = await this.request("/v1/departments");
    if (listRes.ok) {
      const data = await listRes.json();
      const depts = Array.isArray(data) ? data : data.items || [];
      if (name) {
        const found = depts.find((d: any) => d.name?.toLowerCase() === name.toLowerCase());
        if (found) return found;
      } else if (depts.length > 0) {
        return depts[0];
      }
    }

    const deptName = name || `Dept-${nextSeq()}`;
    const sql = `INSERT INTO departments (id, name) VALUES (gen_random_uuid(), '${deptName.replace(/'/g, "''")}') ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name RETURNING id, name;`;
    try {
      const dbUrl = process.env.DATABASE_URL || "postgres://hdms:hdms@127.0.0.1:5442/hdms?sslmode=disable";
      const out = execSync(`psql "${dbUrl}" -t -A -c "${sql}"`, { encoding: "utf-8" }).trim();
      const [id, n] = out.split("|");
      if (id) {
        return { id, name: n || deptName };
      }
    } catch {
      try {
        const out = execSync(`docker compose exec -T db psql -U hdms -d hdms -t -A -c "${sql}"`, { encoding: "utf-8" }).trim();
        const [id, n] = out.split("|");
        if (id) {
          return { id, name: n || deptName };
        }
      } catch (e) {
        console.error("Failed to seed department via SQL:", e);
      }
    }

    return { id: "01a01e00-0000-0000-0000-000000000001", name: deptName };
  }

  async seedCategory(name?: string): Promise<{ id: string; name: string }> {
    const seq = nextSeq();
    const catName = name || `Category-${seq}`;

    const listRes = await this.request("/v1/categories");
    if (listRes.ok) {
      const data = await listRes.json();
      const cats = Array.isArray(data) ? data : data.items || [];
      const found = cats.find((c: any) => c.name === catName);
      if (found) return found;
    }

    const res = await this.request("/v1/categories", {
      method: "POST",
      body: JSON.stringify({ name: catName }),
    });
    if (!res.ok) {
      throw new Error(`Create category failed: ${await res.text()}`);
    }
    return res.json();
  }

  async seedUser(params?: {
    fullName?: string;
    employeeNo?: string;
    departmentName?: string;
  }): Promise<TestUser> {
    const seq = nextSeq();
    const employeeNo = params?.employeeNo || `E2E-U-${seq}`;
    const fullName = params?.fullName || `E2E User ${seq}`;

    let departmentId: string | undefined;
    let departmentName: string | undefined;
    if (params?.departmentName) {
      const dept = await this.seedDepartment(params.departmentName);
      departmentId = dept.id;
      departmentName = dept.name;
    }

    const userRes = await this.request("/v1/users", {
      method: "POST",
      body: JSON.stringify({
        employeeNo,
        fullName,
        departmentId,
      }),
    });
    if (!userRes.ok) {
      throw new Error(`Create user failed: ${await userRes.text()}`);
    }
    const user = (await userRes.json()) as { id: string; employeeNo: string; fullName: string };

    // Issue QR credential
    const credRes = await this.request("/v1/credentials", {
      method: "POST",
      body: JSON.stringify({
        subjectType: "user",
        subjectId: user.id,
        kind: "qr",
      }),
    });
    if (!credRes.ok) {
      throw new Error(`Issue user credential failed: ${await credRes.text()}`);
    }
    const cred = (await credRes.json()) as { id: string; token: string };

    return {
      id: user.id,
      employeeNo: user.employeeNo,
      fullName: user.fullName,
      departmentId,
      departmentName,
      token: cred.token,
      credentialId: cred.id,
    };
  }

  async seedDevice(params?: {
    name?: string;
    assetTag?: string;
    categoryName?: string;
  }): Promise<TestDevice> {
    const seq = nextSeq();
    const assetTag = params?.assetTag || `E2E-D-${seq}`;
    const name = params?.name || `E2E Device ${seq}`;
    const cat = await this.seedCategory(params?.categoryName);

    const devRes = await this.request("/v1/devices", {
      method: "POST",
      body: JSON.stringify({
        assetTag,
        name,
        categoryId: cat.id,
      }),
    });
    if (!devRes.ok) {
      throw new Error(`Create device failed: ${await devRes.text()}`);
    }
    const device = (await devRes.json()) as { id: string; assetTag: string; name: string };

    // Issue QR credential
    const credRes = await this.request("/v1/credentials", {
      method: "POST",
      body: JSON.stringify({
        subjectType: "device",
        subjectId: device.id,
        kind: "qr",
      }),
    });
    if (!credRes.ok) {
      throw new Error(`Issue device credential failed: ${await credRes.text()}`);
    }
    const cred = (await credRes.json()) as { id: string; token: string };

    return {
      id: device.id,
      assetTag: device.assetTag,
      name: device.name,
      categoryId: cat.id,
      categoryName: cat.name,
      token: cred.token,
      credentialId: cred.id,
    };
  }

  async seedUnboundCard(): Promise<{ id: string; token: string }> {
    const res = await this.request("/v1/credentials/blank-batch", {
      method: "POST",
      body: JSON.stringify({
        count: 1,
        kind: "qr",
      }),
    });
    if (!res.ok) {
      throw new Error(`Issue blank batch failed: ${await res.text()}`);
    }
    const data = (await res.json()) as { items: Array<{ id: string; token: string }> };
    return data.items[0];
  }

  async revokeCredential(credentialId: string, reason = "e2e test revocation"): Promise<void> {
    const res = await this.request(`/v1/credentials/${credentialId}/revoke`, {
      method: "POST",
      body: JSON.stringify({ reason }),
    });
    if (!res.ok) {
      throw new Error(`Revoke credential failed: ${await res.text()}`);
    }
  }

  async registerKiosk(name?: string, location = "E2E Station", defaultLocale = "ja"): Promise<TestKiosk> {
    const seq = nextSeq();
    const kioskName = name || `E2E Kiosk ${seq}`;

    const res = await this.request("/v1/kiosks", {
      method: "POST",
      body: JSON.stringify({
        name: kioskName,
        location,
        defaultLocale,
      }),
    });
    if (!res.ok) {
      throw new Error(`Register kiosk failed: ${await res.text()}`);
    }
    const data = (await res.json()) as { id: string; name: string; token: string; defaultLocale?: string };
    return {
      id: data.id,
      name: data.name,
      token: data.token,
      defaultLocale: data.defaultLocale ?? defaultLocale,
    };
  }

  async issuePairingCode(kioskId: string): Promise<string> {
    const res = await this.request(`/v1/kiosks/${kioskId}/pairing-code`, {
      method: "POST",
    });
    if (!res.ok) {
      throw new Error(`Issue pairing code failed: ${await res.text()}`);
    }
    const data = (await res.json()) as { code: string };
    return data.code;
  }

  async seedLoanViaKiosk(kioskToken: string, deviceToken: string, userToken: string): Promise<any> {
    // Open session
    const sessRes = await fetch(`${this.baseUrl}/v1/sessions`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${kioskToken}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({}),
    });
    if (!sessRes.ok) {
      throw new Error(`Kiosk session open failed: ${await sessRes.text()}`);
    }
    const session = (await sessRes.json()) as { id: string };

    // Scan device
    const scan1 = await fetch(`${this.baseUrl}/v1/sessions/${session.id}/scan`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${kioskToken}`,
        "Content-Type": "application/json",
        "Idempotency-Key": `seed:${session.id}:1`,
      },
      body: JSON.stringify({ token: deviceToken, source: "scanner" }),
    });
    if (!scan1.ok) {
      throw new Error(`Kiosk scan 1 failed: ${await scan1.text()}`);
    }

    // Scan user
    const scan2 = await fetch(`${this.baseUrl}/v1/sessions/${session.id}/scan`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${kioskToken}`,
        "Content-Type": "application/json",
        "Idempotency-Key": `seed:${session.id}:2`,
      },
      body: JSON.stringify({ token: userToken, source: "scanner" }),
    });
    if (!scan2.ok) {
      throw new Error(`Kiosk scan 2 failed: ${await scan2.text()}`);
    }

    // Close session
    await fetch(`${this.baseUrl}/v1/sessions/${session.id}/close`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${kioskToken}`,
        "Content-Type": "application/json",
      },
    });

    return scan2.json();
  }

  async getLoans(): Promise<any[]> {
    const res = await this.request("/v1/loans");
    if (!res.ok) {
      throw new Error(`Get loans failed: ${await res.text()}`);
    }
    const data = await res.json();
    return data.items || data;
  }

  async getDevice(deviceId: string): Promise<any> {
    const res = await this.request(`/v1/devices/${deviceId}`);
    if (!res.ok) {
      throw new Error(`Get device failed: ${await res.text()}`);
    }
    return res.json();
  }

  async getDeviceLoans(deviceId: string): Promise<any[]> {
    const res = await this.request(`/v1/devices/${deviceId}/loans`);
    if (!res.ok) {
      throw new Error(`Get device loans failed: ${await res.text()}`);
    }
    const data = await res.json();
    return data.items || data;
  }

  async getUserCount(): Promise<number> {
    const res = await this.request("/v1/users");
    if (!res.ok) {
      throw new Error(`Get users failed: ${await res.text()}`);
    }
    const data = await res.json();
    const items = data.items || data;
    return items.length;
  }
}

/**
 * Injects paired kiosk credentials into localStorage before any page scripts execute.
 */
export async function prePairKiosk(
  page: Page,
  kiosk: { id: string; name: string; token: string; defaultLocale?: string },
  enabledSources: string[] = ["hid", "camera", "manual"],
  defaultLocale: "ja" | "en" = "ja"
): Promise<void> {
  await page.addInitScript(
    (cfg) => {
      localStorage.setItem(
        "hdms_kiosk_config",
        JSON.stringify({
          schemaVersion: 1,
          kioskId: cfg.id,
          kioskName: cfg.name,
          token: cfg.token,
          defaultLocale: cfg.defaultLocale,
          muteEnabled: true, // Mute audio in automated tests to prevent noisy audio contexts
          enabledSources,
        })
      );
    },
    { id: kiosk.id, name: kiosk.name, token: kiosk.token, defaultLocale: kiosk.defaultLocale ?? defaultLocale }
  );
}
