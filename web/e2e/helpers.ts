import { expect, type Page } from '@playwright/test'

/**
 * Reloads `page` until `assertion` passes. Indexing (crawler -> index
 * workers -> search) and the UI's own status polling are asynchronous, so
 * data-dependent expectations retry through reloads instead of fixed sleeps.
 */
export async function expectAfterReload(
  page: Page,
  assertion: () => Promise<void>,
  timeoutMs = 120_000,
) {
  await expect(async () => {
    await page.reload()
    await assertion()
  }).toPass({ timeout: timeoutMs, intervals: [1_000, 2_000, 5_000] })
}
