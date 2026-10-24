import { useEffect, useMemo, useState } from "react";
import { fetchGitRelatedFileDiff } from "../services/git";

export type RelatedFileStat = {
  status: string;
  additions: number;
  deletions: number;
};

export type RelatedFileStatTarget = {
  path: string;
  head?: string;
  repo_path?: string;
  repo_kind?: string;
};

// Each request makes the server run git commands. Keep the batch small so a
// long related-file list does not turn into a process storm.
const RELATED_FILE_STAT_CONCURRENCY = 3;

async function mapWithConcurrency<T, R>(
  items: T[],
  limit: number,
  fn: (item: T) => Promise<R>,
): Promise<R[]> {
  const results = new Array<R>(items.length);
  let nextIndex = 0;
  const workers = Array.from(
    { length: Math.min(Math.max(1, limit), items.length) },
    async () => {
      while (nextIndex < items.length) {
        const index = nextIndex;
        nextIndex += 1;
        results[index] = await fn(items[index]);
      }
    },
  );
  await Promise.all(workers);
  return results;
}

export function relatedFileStatKey(file: RelatedFileStatTarget): string {
  return [
    file.repo_kind || "",
    file.repo_path || "",
    file.head || "",
    file.path,
  ].join("\0");
}

export function useRelatedFileStats(
  rootId: string | null | undefined,
  files: RelatedFileStatTarget[],
  refreshKey = "",
): Record<string, RelatedFileStat> {
  const filesSignature = useMemo(
    () =>
      files
        .filter((file) => file.path && file.repo_kind !== "plain")
        .map((file) => relatedFileStatKey(file))
        .sort()
        .join("\n"),
    [files],
  );
  const [statsByKey, setStatsByKey] = useState<Record<string, RelatedFileStat>>({});

  useEffect(() => {
    const targets = Array.from(
      new Map(
        files
          .filter(
            (file) =>
              file.path &&
              file.repo_kind !== "plain" &&
              // A recorded base or repository is required to recover a diff
              // after the main working tree becomes clean.
              Boolean(file.head || file.repo_path),
          )
          .map((file) => [relatedFileStatKey(file), file]),
      ).entries(),
    );
    if (!rootId || targets.length === 0) {
      setStatsByKey({});
      return;
    }

    let cancelled = false;
    void mapWithConcurrency(
      targets,
      RELATED_FILE_STAT_CONCURRENCY,
      async ([key, file]) => {
        try {
          const diff = await fetchGitRelatedFileDiff(rootId, file);
          return [
            key,
            {
              status: diff.status,
              additions: diff.additions,
              deletions: diff.deletions,
            },
          ] as const;
        } catch {
          return null;
        }
      },
    ).then((results) => {
      if (cancelled) return;
      setStatsByKey(
        Object.fromEntries(
          results.filter(
            (entry): entry is NonNullable<typeof entry> => entry !== null,
          ),
        ),
      );
    });

    return () => {
      cancelled = true;
    };
  }, [filesSignature, refreshKey, rootId]);

  return statsByKey;
}
