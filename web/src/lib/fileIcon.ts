export interface FileIcon {
  emoji: string
  label: string
}

/**
 * Returns an emoji icon and accessible label for a file's content type.
 * Image files are treated specially by the table preview and should not
 * use this helper.
 */
export function getFileIcon(contentType: string): FileIcon {
  if (contentType.startsWith('text/')) {
    return { emoji: '📄', label: 'Text file' }
  }
  if (contentType === 'application/pdf') {
    return { emoji: '📕', label: 'PDF' }
  }
  if (contentType.startsWith('video/')) {
    return { emoji: '🎬', label: 'Video' }
  }
  if (contentType.startsWith('audio/')) {
    return { emoji: '🎵', label: 'Audio' }
  }
  if (
    contentType === 'application/zip' ||
    contentType === 'application/x-tar' ||
    contentType === 'application/gzip' ||
    contentType === 'application/x-bzip2' ||
    contentType === 'application/x-7z-compressed' ||
    contentType === 'application/x-rar-compressed' ||
    contentType === 'application/x-zip-compressed'
  ) {
    return { emoji: '📦', label: 'Archive' }
  }
  return { emoji: '📎', label: 'File' }
}
