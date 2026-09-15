export function normalizePage(value: unknown): number {
  const page = Number(value ?? 1);
  return Number.isFinite(page) ? Math.max(1, Math.trunc(page)) : 1;
}

export function createLatestRequestGuard() {
  let latest = 0;

  return {
    begin() {
      latest += 1;
      return latest;
    },
    isCurrent(requestId: number) {
      return requestId === latest;
    },
    invalidate() {
      latest += 1;
    },
  };
}
