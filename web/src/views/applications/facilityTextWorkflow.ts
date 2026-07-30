export interface FacilityTextWorkflowState {
  open: boolean;
  error: string;
  conflict: boolean;
}

export async function saveFacilityTextWorkflow(
  state: FacilityTextWorkflowState,
  operation: () => Promise<void>,
  errorMessage: (error: unknown) => string,
) {
  state.error = '';
  state.conflict = false;
  try {
    await operation();
    state.open = false;
    return true;
  } catch (error) {
    state.error = errorMessage(error);
    state.conflict = (error as { code?: string })?.code === 'edit_session_revision_conflict';
    return false;
  }
}

export async function reloadFacilityTextWorkflow(
  state: FacilityTextWorkflowState,
  sessionId: string | undefined,
  discard: (sessionId: string) => Promise<void>,
  loadAndReopen: () => Promise<void>,
  errorMessage: (error: unknown) => string,
) {
  try {
    if (sessionId) await discard(sessionId);
    await loadAndReopen();
    state.open = false;
    state.error = '';
    state.conflict = false;
    return true;
  } catch (error) {
    state.error = errorMessage(error);
    return false;
  }
}
