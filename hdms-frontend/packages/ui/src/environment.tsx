import * as React from "react";
import { getHealthz } from "@hdms/api-client";

// Which deployment this page is talking to. Staging ships the same frontend
// build as production (deploy/production/Dockerfile.caddy), so the answer has
// to come from the API at runtime — never from a build-time variable.
export type DeploymentEnvironment = "development" | "test" | "staging" | "production";

// Anything unknown or unreachable reads as production: a missing banner on a
// non-production stack is an annoyance, a banner on production is a lie.
const EnvironmentContext = React.createContext<DeploymentEnvironment>("production");

export function useEnvironment(): DeploymentEnvironment {
  return React.useContext(EnvironmentContext);
}

type Marked = Exclude<DeploymentEnvironment, "production">;

const MARKS: Record<Marked, { tag: string; letter: string; background: string; foreground: string }> = {
  development: { tag: "[DEV]", letter: "D", background: "#1d4ed8", foreground: "#ffffff" },
  test: { tag: "[TEST]", letter: "T", background: "#1d4ed8", foreground: "#ffffff" },
  staging: { tag: "[STG]", letter: "S", background: "#f59e0b", foreground: "#000000" },
};

function markFor(env: DeploymentEnvironment) {
  return env === "production" ? null : MARKS[env];
}

function faviconFor(letter: string, background: string, foreground: string): string {
  const svg =
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">` +
    `<rect width="32" height="32" rx="6" fill="${background}"/>` +
    `<text x="16" y="23" font-family="sans-serif" font-size="20" font-weight="700" ` +
    `text-anchor="middle" fill="${foreground}">${letter}</text></svg>`;
  return `data:image/svg+xml,${encodeURIComponent(svg)}`;
}

// Fetches the environment once per page load, then marks the browser tab
// (title prefix and a coloured favicon) so a non-production tab is
// recognisable even when it is not the one in front.
export function EnvironmentProvider({ children }: { children: React.ReactNode }) {
  const [env, setEnv] = React.useState<DeploymentEnvironment>("production");

  React.useEffect(() => {
    let active = true;
    getHealthz()
      .then(({ data }) => {
        if (active && data?.environment) setEnv(data.environment);
      })
      .catch(() => {});
    return () => {
      active = false;
    };
  }, []);

  // Layout effect: the tab is marked in the same commit that shows the banner.
  React.useLayoutEffect(() => {
    const mark = markFor(env);
    if (!mark) return;

    const title = document.title;
    document.title = `${mark.tag} ${title}`;

    const icon = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
    const href = icon?.getAttribute("href") ?? null;
    icon?.setAttribute("href", faviconFor(mark.letter, mark.background, mark.foreground));

    return () => {
      document.title = title;
      if (icon && href !== null) icon.setAttribute("href", href);
    };
  }, [env]);

  return <EnvironmentContext.Provider value={env}>{children}</EnvironmentContext.Provider>;
}

export interface EnvironmentBannerProps {
  // Each app supplies its own translated text; test deployments reuse the
  // development wording.
  labels: { development: string; staging: string };
}

// A sticky strip across the top of every screen on non-production stacks.
// Styled inline, not with Tailwind classes, because the apps' Tailwind builds
// do not scan this package.
export function EnvironmentBanner({ labels }: EnvironmentBannerProps) {
  const env = useEnvironment();
  const mark = markFor(env);
  if (!mark) return null;

  return (
    <div
      role="status"
      data-testid="environment-banner"
      data-environment={env}
      style={{
        position: "sticky",
        top: 0,
        zIndex: 50,
        padding: "4px 16px",
        background: mark.background,
        color: mark.foreground,
        fontSize: "12px",
        fontWeight: 700,
        letterSpacing: "0.05em",
        lineHeight: "20px",
        textAlign: "center",
        pointerEvents: "none",
      }}
    >
      {env === "staging" ? labels.staging : labels.development}
    </div>
  );
}
