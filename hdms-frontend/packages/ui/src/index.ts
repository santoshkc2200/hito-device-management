import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

// The standard shadcn/ui class-merging helper, shared so every copied-in
// component behaves the same way in both apps.
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export {
  EnvironmentBanner,
  EnvironmentProvider,
  useEnvironment,
  type DeploymentEnvironment,
  type EnvironmentBannerProps,
} from "./environment";

export {
  MAINTENANCE_RECHECK_MS,
  clearMaintenance,
  installMaintenanceInterceptor,
  isMaintenanceResponse,
  isUnderMaintenance,
  reportMaintenance,
  resetMaintenanceForTesting,
  subscribeMaintenance,
  useMaintenance,
} from "./maintenance";

