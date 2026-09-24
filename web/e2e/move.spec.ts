import { Buffer } from 'node:buffer'
import { expect, type FileChooser, type Locator, type Page, test } from '@playwright/test'
import { expectAfterReload } from './helpers'

async function uploadFile(page: Page, fileName: string, content: Buffer) {
  let chooser: FileChooser | undefined
  for (let attempt = 0; attempt < 20 && chooser === undefined; attempt++) {
    // The listener must register before the click: the event fires
    // synchronously with it and a late listener misses it.
    const opened = page.waitForEvent('filechooser', { timeout: 5_000 }).catch(() => undefined)
    await page.getByRole('button', { name: 'Upload', exact: true }).click()
    chooser = await opened
  }
  if (chooser === undefined) throw new Error('Upload file chooser never opened')
  await chooser.setFiles({
    name: fileName,
    mimeType: 'text/plain',
    buffer: content,
  })

  // Wait for the upload mutation to settle (button returns from "Uploading…"
  // to "Upload") before any reload.
  await expect(page.getByRole('button', { name: 'Upload', exact: true })).toBeVisible({
    timeout: 60_000,
  })
  await expect(page.getByRole('alert')).toHaveCount(0)
}

async function openMoveDialog(page: Page, fileName: string): Promise<Locator> {
  const row = page.getByRole('row').filter({ hasText: fileName })
  const dialog = page.getByRole('alertdialog', { name: 'Move file' })
  for (let attempt = 0; attempt < 20; attempt++) {
    await row.getByRole('button', { name: 'Move' }).click()
    if (await dialog.isVisible().catch(() => false)) return dialog
    await page.waitForTimeout(200)
  }
  throw new Error('Move dialog did not open')
}

async function deleteFileRow(page: Page, fileName: string) {
  const row = page.getByRole('row').filter({ hasText: fileName })
  const confirm = page.getByRole('alertdialog', { name: 'Confirm delete file' })
  for (let attempt = 0; attempt < 20; attempt++) {
    await row.getByRole('button', { name: 'Delete' }).click()
    if (await confirm.isVisible().catch(() => false)) {
      await confirm.getByRole('button', { name: 'Confirm delete' }).click()
      return
    }
    await page.waitForTimeout(200)
  }
  throw new Error('Delete confirmation did not open')
}

test('move file to another folder round-trip', async ({ page }) => {
  const runId = Date.now().toString()
  const fileName = `e2e-move-${runId}.txt`
  const sourcePath = `e2e-move-source-${runId}/`
  const destFolderName = `e2e-move-dest-${runId}`
  const destPath = `${destFolderName}/`
  const collisionPath = `e2e-move-collision-${runId}/`
  const content = Buffer.from('moved by playwright e2e\n')

  const browseUrl = (path: string) => `/?path=${encodeURIComponent(path)}`

  // Upload a uniquely-named fixture into a source folder.
  await page.goto(browseUrl(sourcePath))
  await uploadFile(page, fileName, content)
  await expectAfterReload(page, async () => {
    await expect(page.getByText(fileName, { exact: true })).toBeVisible()
  })

  // Move it to a fresh destination folder created via the dialog.
  let dialog = await openMoveDialog(page, fileName)
  await dialog.getByRole('button', { name: 'Home' }).click()
  await expect(dialog.getByText('Loading…')).toHaveCount(0)
  await dialog.getByPlaceholder('folder name').fill(destFolderName)
  await dialog.getByRole('button', { name: 'Create' }).click()
  await expect(dialog.locator('code')).toContainText(`${destPath}${fileName}`)
  await dialog.getByRole('button', { name: 'Move here' }).click()
  await expect(page.getByRole('alertdialog', { name: 'Move file' })).toHaveCount(0, {
    timeout: 60_000,
  })

  // The file is now under the destination folder and absent from the source.
  await page.goto(browseUrl(destPath))
  await expectAfterReload(page, async () => {
    await expect(page.getByText(fileName, { exact: true })).toBeVisible()
  })

  await page.goto(browseUrl(sourcePath))
  await expectAfterReload(page, async () => {
    await expect(page.getByText(fileName, { exact: true })).toHaveCount(0)
  })

  // Collision case: a second file with the same basename in another folder
  // cannot be moved onto the first file's destination.
  await page.goto(browseUrl(collisionPath))
  await uploadFile(page, fileName, content)
  await expectAfterReload(page, async () => {
    await expect(page.getByText(fileName, { exact: true })).toBeVisible()
  })

  dialog = await openMoveDialog(page, fileName)
  await dialog.getByRole('button', { name: 'Home' }).click()
  await expect(dialog.getByText('Loading…')).toHaveCount(0)
  await dialog.getByPlaceholder('folder name').fill(destFolderName)
  await dialog.getByRole('button', { name: 'Create' }).click()
  await expect(dialog.locator('code')).toContainText(`${destPath}${fileName}`)
  await dialog.getByRole('button', { name: 'Move here' }).click()

  await expect(dialog.getByRole('alert')).toContainText(/already exists/i, {
    timeout: 10_000,
  })
  await expect(dialog).toBeVisible()
  await dialog.getByRole('button', { name: 'Cancel' }).click()
  await expect(dialog).toHaveCount(0)

  // Cleanup: delete both fixture files so the spec is idempotent.
  await page.goto(browseUrl(destPath))
  await expectAfterReload(page, async () => {
    await expect(page.getByText(fileName, { exact: true })).toBeVisible()
  })
  await deleteFileRow(page, fileName)

  await page.goto(browseUrl(collisionPath))
  await expectAfterReload(page, async () => {
    await expect(page.getByText(fileName, { exact: true })).toBeVisible()
  })
  await deleteFileRow(page, fileName)
})
