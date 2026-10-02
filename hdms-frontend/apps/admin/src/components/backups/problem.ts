type Problem = { type?: string };

// problemIs matches an RFC 9457 problem by the last segment of its type URI.
export const problemIs = (err: unknown, type: string) =>
  (err as Problem | null)?.type?.endsWith(`/${type}`) ?? false;

// Problem type → backups.restore.problems key.
const PROBLEM_KEYS: Record<string, string> = {
  "reauth-failed": "reauthFailed",
  "account-locked": "accountLocked",
  "restore-running": "restoreRunning",
  "nothing-to-undo": "nothingToUndo",
  "nothing-to-discard": "nothingToDiscard",
  "database-server-down": "databaseServerDown",
  "restore-source-unsupported": "sourceUnsupported",
  "repository-unreadable": "repositoryUnreadable",
  "worker-unavailable": "workerUnavailable",
  "not-found": "notFound",
};

export function restoreProblemKey(err: unknown): string {
  const hit = Object.keys(PROBLEM_KEYS).find((type) => problemIs(err, type));
  return `backups.restore.problems.${hit ? PROBLEM_KEYS[hit] : "failed"}`;
}
