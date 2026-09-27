export interface FileViewerProps {
  contentType: string
  openUrl: string
}

const mediaContainerStyle: React.CSSProperties = {
  flex: 1,
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  minHeight: 0,
  overflow: 'hidden',
  width: '100%',
}

const iframeContainerStyle: React.CSSProperties = {
  flex: 1,
  display: 'flex',
  flexDirection: 'column',
  minHeight: 0,
  overflow: 'hidden',
  width: '100%',
}

const mediaStyle: React.CSSProperties = {
  maxWidth: '100%',
  maxHeight: '100%',
}

export function FileViewer({ contentType, openUrl }: FileViewerProps) {
  if (contentType.startsWith('image/')) {
    return (
      <div style={mediaContainerStyle}>
        <img src={openUrl} alt="" style={mediaStyle} />
      </div>
    )
  }

  if (contentType.startsWith('video/')) {
    return (
      <div style={mediaContainerStyle}>
        {/* biome-ignore lint/a11y/useMediaCaption: preview videos do not have caption tracks available */}
        <video src={openUrl} controls style={mediaStyle} />
      </div>
    )
  }

  return (
    <div style={iframeContainerStyle}>
      <iframe
        src={openUrl}
        title="File preview"
        style={{ width: '100%', height: '100%', border: 'none' }}
      />
    </div>
  )
}
