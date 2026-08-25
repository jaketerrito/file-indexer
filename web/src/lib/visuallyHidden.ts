import type { CSSProperties } from 'react'

// Hides an element visually while keeping it in the render tree — unlike
// `display: none`, which removes it from the render tree entirely. That
// distinction matters for the hidden `<input type="file">` behind our
// upload buttons: WebKit/Safari refuses to open the native file picker from
// a programmatic `.click()` on a `display: none` input (it's simply a
// no-op, with no error), so a real click on the visible "Upload" button
// silently did nothing there. Standard "visually hidden" a11y pattern
// instead — clipped to nothing, off the visual flow, but still a real,
// clickable node in every browser.
export const visuallyHiddenStyle: CSSProperties = {
  position: 'absolute',
  width: 1,
  height: 1,
  padding: 0,
  margin: -1,
  overflow: 'hidden',
  clip: 'rect(0,0,0,0)',
  whiteSpace: 'nowrap',
  border: 0,
}
