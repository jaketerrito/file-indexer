-- Queue queries for the stat indexer worker pool. The lifecycle is:
--
--   (no row)  --seed-->  pending  --claim-->  claimed  --complete-->  done
--                          ^                    |
--                          +------fail----------+--(attempts exhausted)--> error
--
-- Staleness detection: the seed step also re-enqueues done rows whose
-- stored mark no longer matches files.marked_at, so an edited object is
-- automatically re-indexed on the next seed cycle without the crawler
-- needing to know which index types exist.
--
-- Timing policy (backoff, claim TTL, max attempts) lives in the worker
-- framework; these queries only persist the decisions it makes.

-- name: SeedIndexStat :execrows
-- Discover files that have no index_stat row yet and enqueue them as
-- pending. Self-seeding means the crawler does not need to know which index
-- types exist; a new worker type discovers its entire backlog on startup.
INSERT INTO index_stat (file_id)
SELECT f.id
FROM files f
WHERE NOT EXISTS (SELECT 1 FROM index_stat s WHERE s.file_id = f.id)
ON CONFLICT (file_id) DO NOTHING;

-- name: RequeueStaleIndexStat :execrows
-- Re-enqueue done rows whose stored mark no longer matches files.marked_at.
-- This is the second half of seed: new files are handled by SeedIndexStat;
-- edited files (whose crawler re-crawl bumped marked_at past the stored
-- mark) are handled here. Only done rows are re-enqueued — pending/claimed
-- rows are already in-flight; error rows stay parked until manually reset.
UPDATE index_stat s
SET status = 'pending',
    attempts = 0,
    next_attempt_at = now(),
    last_error = NULL,
    updated_at = now()
FROM files f
WHERE s.file_id = f.id
  AND s.status = 'done'
  AND s.mark IS DISTINCT FROM f.marked_at;

-- name: ClaimIndexStat :many
-- Atomically claim a batch of jobs for one worker pool poll. FOR UPDATE SKIP
-- LOCKED lets concurrent claimers grab disjoint batches without blocking.
-- Rows stuck in claimed since before stale_before (a crashed worker's claim
-- TTL cutoff) are reclaimed alongside pending rows.
WITH candidates AS (
    SELECT file_id
    FROM index_stat
    WHERE (status = 'pending' AND next_attempt_at <= now())
       OR (status = 'claimed' AND claimed_at <= sqlc.arg(stale_before)::timestamptz)
    ORDER BY next_attempt_at
    LIMIT sqlc.arg(batch_size)
    FOR UPDATE SKIP LOCKED
),
claimed AS (
    UPDATE index_stat s
    SET status = 'claimed', attempts = s.attempts + 1, claimed_at = now(), updated_at = now()
    FROM candidates c
    WHERE s.file_id = c.file_id
    RETURNING s.file_id, s.attempts
)
SELECT c.file_id, c.attempts, f.key
FROM claimed c
JOIN files f ON f.id = c.file_id;

-- name: ReleaseIndexStat :exec
-- Return a claimed-but-unstarted job to pending without consuming an
-- attempt (worker pool shutdown between claim and dispatch). The status
-- guard keeps a late release from clobbering a row another worker has
-- already reclaimed and completed.
UPDATE index_stat
SET status = 'pending',
    attempts = GREATEST(attempts - 1, 0),
    claimed_at = NULL,
    updated_at = now()
WHERE file_id = $1
  AND status = 'claimed';

-- name: CompleteIndexStat :exec
-- Record a successful run: status flip, mark capture, and stat results in
-- one atomic UPDATE. mark copies files.marked_at at completion time so
-- that an edit arriving mid-run (bumping marked_at) still triggers a
-- re-index on the next seed cycle.
UPDATE index_stat s
SET status = 'done',
    last_error = NULL,
    updated_at = now(),
    mark = f.marked_at,
    content_type = sqlc.arg(content_type),
    size_bytes = sqlc.arg(size_bytes),
    last_modified = sqlc.arg(last_modified)::timestamptz
FROM files f
WHERE s.file_id = sqlc.arg(file_id)
  AND f.id = sqlc.arg(file_id);

-- name: FailIndexStat :exec
-- Record a failed attempt. When the worker framework has exhausted retries it
-- sets exhausted, parking the row as error until manually reset; otherwise
-- the row returns to pending and becomes claimable at next_attempt_at.
UPDATE index_stat
SET status = CASE WHEN sqlc.arg(exhausted)::bool THEN 'error' ELSE 'pending' END,
    next_attempt_at = sqlc.arg(next_attempt_at)::timestamptz,
    last_error = sqlc.arg(last_error),
    updated_at = now()
WHERE file_id = sqlc.arg(file_id);
