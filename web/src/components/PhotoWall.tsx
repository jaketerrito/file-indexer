import { useInfiniteQuery } from '@tanstack/react-query'
import { useEffect, useRef } from 'react'
import { PreviewStatus } from '../gen/service/v1/files_pb'
import { formatDate } from '../lib/formatDate'
import { useFileStatusPoller } from '../lib/useFileStatusPoller'
import { listFiles } from '../server/files'
import type { FileDto } from '../server/impl'

const PAGE_SIZE = 50

interface PhotoWallProps {
  /** Opens a file's standalone page. */
  onOpenFile: (id: string) => void
}

interface DayGroup {
  date: string
  files: FileDto[]
}

function groupByDay(files: FileDto[]): DayGroup[] {
  const groups: DayGroup[] = []
  for (const file of files) {
    const effectiveDate = file.takenAt ?? file.updatedAt ?? file.createdAt
    const date = formatDate(effectiveDate)
    if (groups.length === 0 || groups[groups.length - 1].date !== date) {
      groups.push({ date, files: [file] })
    } else {
      groups[groups.length - 1].files.push(file)
    }
  }
  return groups
}

export function PhotoWall({ onOpenFile }: PhotoWallProps) {
  const { data, error, isPending, isError, fetchNextPage, hasNextPage, isFetchingNextPage } =
    useInfiniteQuery({
      queryKey: ['photos'],
      queryFn: ({ pageParam }) =>
        listFiles({
          data: {
            pageSize: PAGE_SIZE,
            pageToken: pageParam,
            query: '',
            contentType: 'image/',
            sortField: 'takenAt',
            sortOrder: 'desc',
          },
        }),
      initialPageParam: '',
      getNextPageParam: (lastPage) => lastPage.nextPageToken || undefined,
    })

  useFileStatusPoller(data?.pages, ['photos'])

  const sentinelRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const sentinel = sentinelRef.current
    if (!sentinel) return
    const observer = new IntersectionObserver((entries) => {
      if (entries.some((e) => e.isIntersecting) && hasNextPage && !isFetchingNextPage) {
        void fetchNextPage()
      }
    })
    observer.observe(sentinel)
    return () => observer.disconnect()
  }, [fetchNextPage, hasNextPage, isFetchingNextPage])

  const files = data?.pages.flatMap((page) => page.files) ?? []
  const groups = groupByDay(files)

  if (isPending) {
    return <p>Loading…</p>
  }

  if (isError) {
    return <p role="alert">Failed to load photos: {String(error)}</p>
  }

  if (files.length === 0) {
    return <p>No photos.</p>
  }

  return (
    <div>
      <style>{`.photo-wall-grid {
  display: grid;
  gap: 0.5rem;
  grid-template-columns: repeat(5, 1fr);
}

@media (min-width: 480px) {
  .photo-wall-grid {
    grid-template-columns: repeat(auto-fill, minmax(96px, 1fr));
  }
}

@media (min-width: 768px) {
  .photo-wall-grid {
    grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
  }
}

@media (min-width: 1280px) {
  .photo-wall-grid {
    grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  }
}`}</style>
      {groups.map((group) => (
        <section key={group.date} style={{ marginBottom: '1.5rem' }}>
          <h2 style={{ margin: '0.5rem 0', fontSize: '1.1rem' }}>{group.date}</h2>
          <div className="photo-wall-grid" data-testid="photo-grid">
            {group.files.map((file) => (
              <button
                key={file.id}
                type="button"
                onClick={() => onOpenFile(file.id)}
                aria-label={file.key}
                style={{
                  aspectRatio: '1',
                  overflow: 'hidden',
                  cursor: 'pointer',
                  border: 'none',
                  borderRadius: '4px',
                  background: '#f5f5f5',
                  padding: 0,
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                }}
              >
                <PhotoThumbnail file={file} />
              </button>
            ))}
          </div>
        </section>
      ))}
      <div ref={sentinelRef} data-testid="scroll-sentinel" />
      {isFetchingNextPage ? <p>Loading more…</p> : null}
    </div>
  )
}

function PhotoThumbnail({ file }: { file: FileDto }) {
  if (file.previewUrl) {
    return (
      <img
        src={file.previewUrl}
        alt=""
        role="img"
        width={file.previewWidth ?? undefined}
        height={file.previewHeight ?? undefined}
        loading="lazy"
        style={{ width: '100%', height: '100%', objectFit: 'cover' }}
      />
    )
  }

  const boxStyle: React.CSSProperties = {
    width: '100%',
    height: '100%',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    color: '#666',
    fontSize: '0.85rem',
    textAlign: 'center',
    padding: '0.5rem',
  }

  if (
    file.previewStatus === PreviewStatus.PENDING ||
    file.previewStatus === PreviewStatus.PROCESSING
  ) {
    return <span style={boxStyle}>Processing…</span>
  }

  if (file.previewStatus === PreviewStatus.FAILED) {
    return <span style={boxStyle}>Preview failed</span>
  }

  return <span style={boxStyle}>No preview</span>
}
