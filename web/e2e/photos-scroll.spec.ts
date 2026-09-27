import { expect, test } from '@playwright/test'
import { waitForHydrated } from './helpers'

const FIRST_PAGE_SIZE = 24
const TOTAL_PHOTOS = 34
const SECOND_PAGE_PHOTO = 'photo-extra-001.jpg'

test('Photos page infinite scroll loads the next page', async ({ page }) => {
  await page.goto('/photos')
  await waitForHydrated(page)

  // Wait for the first page of thumbnails to render.
  const firstThumbnail = page.getByRole('img').first()
  await expect(firstThumbnail).toBeVisible()

  await expect
    .poll(async () => page.getByTestId('photo-grid').getByRole('button').count(), {
      message: 'first page of photos is rendered',
    })
    .toBe(FIRST_PAGE_SIZE)

  // Scroll the sentinel into view to trigger the next page fetch.
  const sentinel = page.getByTestId('scroll-sentinel')
  await sentinel.scrollIntoViewIfNeeded()

  // The second page should load and append the remaining photos.
  await expect
    .poll(async () => page.getByTestId('photo-grid').getByRole('button').count(), {
      message: 'second page of photos is loaded',
    })
    .toBe(TOTAL_PHOTOS)

  const secondPagePhoto = page.getByRole('button', {
    name: SECOND_PAGE_PHOTO,
    exact: true,
  })
  await expect(secondPagePhoto).toBeVisible()
})
