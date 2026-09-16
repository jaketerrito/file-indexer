import { Buffer } from 'node:buffer'
import { expect, test } from '@playwright/test'
import { expectAfterReload } from './helpers'

// Full write-path round-trip against the deployed stack: presigned PUT to
// MinIO through the gateway, CommitUpload, async indexing, browse listing,
// and the two-step delete. The key is unique per run so repeats never
// collide with leftovers from earlier runs.
test('upload, browse, and delete round-trip', async ({ page }) => {
  const key = `e2e-upload-${Date.now()}.txt`

  // path= (empty) is browse mode at the bucket root.
  await page.goto('/?path=')
  await expect(page.getByRole('button', { name: 'Upload', exact: true })).toBeVisible()

  // The file input is visually hidden; drive it through the chooser the
  // Upload button opens.
  const chooserPromise = page.waitForEvent('filechooser')
  await page.getByRole('button', { name: 'Upload', exact: true }).click()
  const chooser = await chooserPromise
  await chooser.setFiles({
    name: key,
    mimeType: 'text/plain',
    buffer: Buffer.from('uploaded by playwright e2e\n'),
  })

  // Wait for the upload mutation to settle (button returns from "Uploading…"
  // to "Upload") before any reload — navigating away mid-flight aborts the
  // presigned PUT.
  await expect(page.getByRole('button', { name: 'Upload', exact: true })).toBeVisible({
    timeout: 60_000,
  })
  // A failed upload surfaces an inline alert rather than rejecting the wait above.
  await expect(page.getByRole('alert')).toHaveCount(0)

  // The upload is committed and indexed asynchronously; poll until the
  // browse list shows it.
  await expectAfterReload(page, async () => {
    await expect(page.getByText(key, { exact: true })).toBeVisible()
  })

  // Open the file's page and delete it there (two-step confirm).
  await page
    .getByRole('listitem')
    .filter({ hasText: key })
    .getByRole('button', { name: 'Metadata' })
    .click()
  await expect(page.getByRole('heading', { name: key })).toBeVisible()
  await page.getByRole('button', { name: 'Delete', exact: true }).click()
  await page
    .getByRole('alertdialog', { name: 'Confirm delete file' })
    .getByRole('button', { name: 'Confirm delete' })
    .click()

  // Delete navigates back to the parent folder (bucket root); the file is gone.
  await expect(page).toHaveURL(/\?path=&?$/)
  await expectAfterReload(page, async () => {
    await expect(page.getByText(key, { exact: true })).toHaveCount(0)
  })
})
