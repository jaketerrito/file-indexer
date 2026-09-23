import { configure } from '@testing-library/react'

// Full-suite runs starve individual workers (14 parallel jsdom environments
// on a laptop CPU), so the 1s default findBy/waitFor timeout flakes on a
// test's first render wait — the mock resolves on a microtask, but the
// starved worker never gets to process it in time. 3s only extends polling
// patience; the assertions themselves are unchanged.
configure({ asyncUtilTimeout: 3000 })
