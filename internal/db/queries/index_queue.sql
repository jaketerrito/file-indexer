-- Queue queries shared by every index type. All statements are scoped by
-- index_type so unrelated pipelines never interfere. The lifecycle is:
--
--   (no row)  --seed-->  pending  --claim-->  claimed  --complete-->  done
--                          ^                    |
--                          +------fail----------+--(attempts exhausted)--> error
--
-- Staleness detection: the seed step also re-enqueues done rows whose
-- stored mark no longer matches files.marked_at, so an edited object is
-- automatically re-indexed on the next seed without the crawler needing to
-- know which index types exist.
--
-- Timing policy (backoff, claim TTL, max attempts) lives in the runner
-- package; these queries only persist the decisions it makes.

-- name: SeedIndexQueue :execrows
-- Discover files that have no index_queue row yet for this index type and
-- enqueue them as pending. Self-seeding means the crawler does not need to
-- know which index types exist; a new index type discovers its entire
-- backlog on startup.
INSERT INTO index_queue (index_type, file_id)
SELECT sqlc.arg(index_type)::text, f.id
FROM files f
WHERE NOT EXISTS (
    SELECT 1 FROM index_queue q
    WHERE q.index_type = sqlc.arg(index_type)::text AND q.file_id = f.id
)
ON CONFLICT (index_type, file_id) DO NOTHING;

-- name: RequeueStaleIndexQueue :execrows
-- Re-enqueue done and pending rows whose stored mark is older than
-- files.marked_at. Done rows were completed against a prior snapshot;
-- pending rows may carry a stale mark from an earlier re-enqueue. In both
-- cases resetting the row ensures the next claim re-indexes the current
-- content. Claimed rows are in-flight and left alone; error rows stay
-- parked until manually reset.
UPDATE index_queue q
SET status = 'pending',
    attempts = 0,
    next_attempt_at = now(),
    last_error = NULL,
    mark = NULL,
    updated_at = now()
FROM files f
WHERE q.file_id = f.id
  AND q.index_type = sqlc.arg(index_type)::text
  AND q.status IN ('done', 'pending')
  AND q.mark IS NOT NULL
  AND q.mark < f.marked_at;

-- name: ClaimIndexQueue :many
-- Atomically claim a batch of jobs for one index type. FOR UPDATE SKIP
-- LOCKED lets concurrent claimers (any number of pods) grab disjoint
-- batches without blocking. Rows claimed longer than stale_timeout
-- (a crashed instance's claim TTL) are reclaimed alongside pending rows.
WITH candidates AS (
    SELECT file_id
    FROM index_queue
    WHERE index_type = sqlc.arg(index_type)::text
      AND ((status = 'pending' AND next_attempt_at <= now())
           OR (status = 'claimed' AND now() - claimed_at >= sqlc.arg(stale_timeout)::interval))
    ORDER BY next_attempt_at
    LIMIT sqlc.arg(batch_size)
    FOR UPDATE SKIP LOCKED
),
claimed AS (
    UPDATE index_queue q
    SET status = 'claimed', attempts = q.attempts + 1, claimed_at = now(), updated_at = now()
    FROM candidates c
    WHERE q.index_type = sqlc.arg(index_type)::text AND q.file_id = c.file_id
    RETURNING q.file_id, q.attempts
)
SELECT c.file_id, c.attempts, f.key
FROM claimed c
JOIN files f ON f.id = c.file_id;

-- name: CompleteIndexQueue :exec
-- Record a successful run: status flip and mark capture. mark copies
-- files.marked_at at completion time so that an edit arriving mid-run
-- (bumping marked_at) still triggers a re-index on the next seed. Callers
-- run this in the same transaction as their result-table write so a job is
-- never marked done without its result persisted, or vice versa.
UPDATE index_queue q
SET status = 'done',
    last_error = NULL,
    updated_at = now(),
    mark = f.marked_at
FROM files f
WHERE q.index_type = sqlc.arg(index_type)::text
  AND q.file_id = sqlc.arg(file_id)
  AND f.id = sqlc.arg(file_id);

-- name: FailIndexQueue :exec
-- Record a failed attempt. When the runner has exhausted retries it sets
-- exhausted, parking the row as error until manually reset; otherwise the
-- row returns to pending and becomes claimable at next_attempt_at.
UPDATE index_queue
SET status = CASE WHEN sqlc.arg(exhausted)::bool THEN 'error' ELSE 'pending' END,
    next_attempt_at = sqlc.arg(next_attempt_at)::timestamptz,
    last_error = sqlc.arg(last_error),
    updated_at = now()
WHERE index_type = sqlc.arg(index_type)::text
  AND file_id = sqlc.arg(file_id);
