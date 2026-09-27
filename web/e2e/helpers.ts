import { expect, type Locator, type Page } from '@playwright/test'

/**
 * Best-effort wait for the page to finish loading and become stable after a
 * navigation or reload. TanStack Start/React hydration has no exposed
 * completion signal, so this waits for the DOM and network to settle and then
 * adds a short grace period. Always pair with `clickUntil` on SSR'd
 * interactive elements, because hydration can still race the first click.
 */
export async function waitForHydrated(
  page: Page,
  options: { timeout?: number; settleMs?: number } = {},
) {
  const { timeout = 30_000, settleMs = 200 } = options
  await page.waitForLoadState('domcontentloaded', { timeout })
  await page.waitForLoadState('networkidle', { timeout })
  if (settleMs > 0) {
    await page.waitForTimeout(settleMs)
  }
}

/**
 * Clicks `locator` repeatedly until `predicate` returns true.
 *
 * Use this for SSR'd buttons that are visible before React hydration attaches
 * their event handlers: a click that lands in that window is a no-op, so we
 * retry until the intended effect (navigation, dialog open, etc.) is observed.
 */
export async function clickUntil(
  page: Page,
  locator: Locator,
  predicate: () => Promise<boolean>,
  options: { maxAttempts?: number; intervalMs?: number; label?: string } = {},
) {
  const { maxAttempts = 20, intervalMs = 200, label = 'click' } = options
  for (let attempt = 0; attempt < maxAttempts; attempt++) {
    await locator.click()
    if (await predicate().catch(() => false)) return
    await page.waitForTimeout(intervalMs)
  }
  throw new Error(`${label} did not produce expected effect after ${maxAttempts} attempts`)
}

/**
 * Clicks `locator` until the page URL matches `urlPattern`.
 */
export async function clickUntilURL(
  page: Page,
  locator: Locator,
  urlPattern: RegExp,
  options?: { maxAttempts?: number; intervalMs?: number; label?: string },
) {
  await clickUntil(page, locator, async () => urlPattern.test(page.url()), {
    ...options,
    label: options?.label ?? 'navigation click',
  })
}

/**
 * Clicks `locator` until `target` becomes visible.
 */
export async function clickUntilVisible(
  page: Page,
  locator: Locator,
  target: Locator,
  options?: { maxAttempts?: number; intervalMs?: number; label?: string },
) {
  await clickUntil(page, locator, async () => target.isVisible(), {
    ...options,
    label: options?.label ?? 'dialog click',
  })
}

/**
 * Returns the table row that contains `fileName`.
 */
export function getFileRow(page: Page, fileName: string): Locator {
  return page.getByRole('row').filter({ hasText: fileName })
}

/**
 * Opens the "File actions" ⋯ menu on the row containing `fileName` and
 * returns the menu locator. Retries clicks to survive the hydration gap on
 * SSR'd interactive elements.
 */
export async function openFileActionsMenu(
  page: Page,
  fileName: string,
): Promise<Locator> {
  const row = getFileRow(page, fileName)
  const menuButton = row.getByRole('button', { name: 'File actions' })
  const menu = row.getByRole('menu')
  await clickUntilVisible(page, menuButton, menu, {
    label: `File actions menu for ${fileName}`,
  })
  return menu
}

/**
 * Clicks a menu item inside an already-open actions menu.
 */
export async function selectMenuItem(menu: Locator, itemName: string): Promise<void> {
  await menu.getByRole('menuitem', { name: itemName }).click()
}

/**
 * Reloads `page` until `assertion` passes. Indexing (crawler -> index
 * workers -> search) and the UI's own status polling are asynchronous, so
 * data-dependent expectations retry through reloads instead of fixed sleeps.
 *
 * After each reload we wait for the page to hydrate so React Query can start
 * its data fetches before the assertion runs.
 */
export async function expectAfterReload(
  page: Page,
  assertion: () => Promise<void>,
  timeoutMs = 120_000,
) {
  await expect(async () => {
    await page.reload()
    await waitForHydrated(page)
    await assertion()
  }).toPass({ timeout: timeoutMs, intervals: [1_000, 2_000, 5_000] })
}
