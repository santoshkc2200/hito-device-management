type Problem = { type?: string };

// problemIs matches an RFC 9457 problem by the last segment of its type URI.
export const problemIs = (err: unknown, type: string) =>
  (err as Problem | null)?.type?.endsWith(`/${type}`) ?? false;
