import { expect, test } from '@playwright/test'
import { expectAfterReload } from './helpers'

// Objects seeded into the bucket by the local overlay
// (deploy/overlays/local/seed/, uploaded by the local-s3 seed sidecar) and
// indexed by the crawler when the Tilt session starts.
const SEED_TEXT_KEYS = ['lorem.txt', 'notes.txt', 'todo.txt']
const SEED_PHOTO_KEYS = [
  'photo-gps-1.jpg',
  'photo-gps-2.jpg',
  'photo-no-exif.jpg',
  'photo-plain.jpg',
]
const SEED_KEYS = [...SEED_TEXT_KEYS, ...SEED_PHOTO_KEYS]

test.beforeEach(async ({ page }) => {
  await page.goto('/')
})

test('seeded files are indexed and listed', async ({ page }) => {
  await expect(page.getByRole('heading', { name: 'Files' })).toBeVisible()
  await expectAfterReload(page, async () => {
    for (const key of SEED_KEYS) {
      await expect(page.getByText(key, { exact: true })).toBeVisible()
    }
  })
})

test('type filter narrows the list to one content-type category', async ({ page }) => {
  // The category options come from the indexed content types, so wait for
  // the full seed set first.
  await expectAfterReload(page, async () => {
    for (const key of SEED_KEYS) {
      await expect(page.getByText(key, { exact: true })).toBeVisible()
    }
  })

  await page.getByLabel('Type').selectOption('image/')

  for (const key of SEED_PHOTO_KEYS) {
    await expect(page.getByText(key, { exact: true })).toBeVisible()
  }
  for (const key of SEED_TEXT_KEYS) {
    await expect(page.getByText(key, { exact: true })).toHaveCount(0)
  }
})

test('header search finds a file and opens its metadata page', async ({ page }) => {
  await expectAfterReload(page, async () => {
    await expect(page.getByText('notes.txt', { exact: true })).toBeVisible()
  })

  await page.getByRole('searchbox', { name: 'Search files by path' }).fill('notes')
  await page.getByRole('button', { name: 'notes.txt' }).click()

  await expect(page).toHaveURL(/\/file\/[^/]+$/)
  await expect(page.getByRole('heading', { name: 'notes.txt' })).toBeVisible()
  // Base stat row from the metadata table (stat indexing must have completed).
  await expect(page.getByRole('row', { name: 'Content type text/plain' })).toBeVisible()
})
