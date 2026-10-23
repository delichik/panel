type UpdateQuery = (values: Record<string, string | undefined>, reset?: boolean) => void;

export function initializeActivityList(
  hasExplicitFrom: boolean,
  initialFrom: string,
  updateQuery: UpdateQuery,
  load: () => void | Promise<void>,
) {
  if (!hasExplicitFrom) updateQuery({ from: initialFrom }, false);
  void load();
}
