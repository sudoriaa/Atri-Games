import { useCallback, useEffect, useState } from "react";

export function useAsync<T>(load: () => Promise<T>, dependencies: readonly unknown[]) {
  const [data, setData] = useState<T | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [revision, setRevision] = useState(0);
  const reload = useCallback(() => setRevision((value) => value + 1), []);
  useEffect(() => {
    let active = true;
    setLoading(true); setError("");
    load().then((value) => { if (active) setData(value); }).catch((caught: unknown) => { if (active) setError(caught instanceof Error ? caught.message : "载入失败"); }).finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...dependencies, revision]);
  return { data, setData, loading, error, reload };
}
