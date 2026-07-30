export function archiveBasePath(filename: string) {
  const stem = filename.trim().replace(/\.(?:tar\.gz|tgz|zip|tar)$/i, '').trim();
  return stem && stem !== '.' && stem !== '..' ? stem : 'archive';
}
