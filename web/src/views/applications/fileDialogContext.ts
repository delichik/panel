export interface FileDialogContext {
  generation: number;
  fileKey: string;
}

export function isSameFileDialogContext(started: FileDialogContext, current: FileDialogContext) {
  return started.generation === current.generation && started.fileKey === current.fileKey;
}
