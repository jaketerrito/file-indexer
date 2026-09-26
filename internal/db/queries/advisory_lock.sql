-- name: AdvisoryLock :exec
-- Acquire a session-scoped advisory lock on a namespaced 32-bit hash of
-- key. The files-service move path uses these locks (two per move, taken
-- in deterministic byte order) so moves are safe across multiple replicas.
-- A session lock is used rather than a transaction-scoped lock because the
-- critical section spans S3 I/O outside any database transaction. The lock
-- is automatically released when the session disconnects.
SELECT pg_advisory_lock(sqlc.arg(namespace)::int, hashtext(sqlc.arg(key))::int);

-- name: AdvisoryUnlock :exec
-- Release a session-scoped advisory lock acquired by AdvisoryLock. This is
-- best-effort cleanup before returning the pinned connection to the pool;
-- the real safety guarantee comes from the lock being bound to the session.
SELECT pg_advisory_unlock(sqlc.arg(namespace)::int, hashtext(sqlc.arg(key))::int);
