import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  type FileLike,
  fileFingerprint,
  type MultipartUploadDeps,
  PART_SIZE_BYTES,
  planParts,
  UploadCancelledError,
  uploadFileMultipart,
} from './multipartUpload'

function fakeFile(size: number): FileLike {
  return {
    name: 'movie.mp4',
    size,
    lastModified: 1700000000000,
    slice: (start: number, end: number) => ({ start, end }) as unknown as Blob,
  }
}

function makeDeps(overrides: Partial<MultipartUploadDeps> = {}): MultipartUploadDeps {
  return {
    createMultipartUpload: vi.fn().mockResolvedValue({ uploadId: 'upload-1' }),
    getUploadPartUrl: vi.fn(({ partNumber }: { partNumber: number }) =>
      Promise.resolve({ url: `https://s3/part-${partNumber}` }),
    ),
    listUploadedParts: vi.fn().mockResolvedValue({ parts: [] }),
    completeMultipartUpload: vi.fn().mockResolvedValue(undefined),
    abortMultipartUpload: vi.fn().mockResolvedValue(undefined),
    putPart: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  }
}

beforeEach(() => {
  localStorage.clear()
})

describe('planParts', () => {
  it('returns no parts for an empty file', () => {
    expect(planParts(0, 10)).toEqual([])
  })

  it('splits a file into fixed-size parts with the remainder last', () => {
    expect(planParts(25, 10)).toEqual([
      { partNumber: 1, start: 0, end: 10 },
      { partNumber: 2, start: 10, end: 20 },
      { partNumber: 3, start: 20, end: 25 },
    ])
  })

  it('produces exactly one part when the file is smaller than the part size', () => {
    expect(planParts(5, 10)).toEqual([{ partNumber: 1, start: 0, end: 5 }])
  })
})

describe('fileFingerprint', () => {
  it('combines name, size, and lastModified', () => {
    expect(fileFingerprint(fakeFile(100))).toBe('movie.mp4:100:1700000000000')
  })
})

describe('uploadFileMultipart', () => {
  it('creates an upload, PUTs every part, and completes it', async () => {
    const file = fakeFile(PART_SIZE_BYTES * 2 + 10)
    const deps = makeDeps()

    await uploadFileMultipart(file, 'big/movie.mp4', 'video/mp4', deps)

    expect(deps.createMultipartUpload).toHaveBeenCalledWith({
      key: 'big/movie.mp4',
      contentType: 'video/mp4',
    })
    expect(deps.putPart).toHaveBeenCalledTimes(3)
    expect(deps.completeMultipartUpload).toHaveBeenCalledWith({
      key: 'big/movie.mp4',
      uploadId: 'upload-1',
    })
    expect(localStorage.getItem('multipart-upload:big/movie.mp4')).toBeNull()
  })

  it('reports progress after each part', async () => {
    const file = fakeFile(PART_SIZE_BYTES * 2)
    const deps = makeDeps()
    const onProgress = vi.fn()

    await uploadFileMultipart(file, 'k', 'video/mp4', deps, { onProgress })

    expect(onProgress).toHaveBeenNthCalledWith(1, 1, 2)
    expect(onProgress).toHaveBeenNthCalledWith(2, 2, 2)
  })

  it('resumes a prior session and skips parts S3 already has', async () => {
    const file = fakeFile(PART_SIZE_BYTES * 2)
    localStorage.setItem(
      'multipart-upload:k',
      JSON.stringify({ uploadId: 'upload-old', fingerprint: fileFingerprint(file) }),
    )
    const deps = makeDeps({
      listUploadedParts: vi
        .fn()
        .mockResolvedValue({ parts: [{ partNumber: 1, size: PART_SIZE_BYTES }] }),
    })

    await uploadFileMultipart(file, 'k', 'video/mp4', deps)

    expect(deps.createMultipartUpload).not.toHaveBeenCalled()
    expect(deps.putPart).toHaveBeenCalledTimes(1)
    expect(deps.getUploadPartUrl).toHaveBeenCalledWith({
      key: 'k',
      uploadId: 'upload-old',
      partNumber: 2,
    })
  })

  it('starts fresh when the resumed upload id is no longer valid server-side', async () => {
    const file = fakeFile(PART_SIZE_BYTES)
    localStorage.setItem(
      'multipart-upload:k',
      JSON.stringify({ uploadId: 'upload-gone', fingerprint: fileFingerprint(file) }),
    )
    const deps = makeDeps({
      listUploadedParts: vi.fn().mockRejectedValue(new Error('not found')),
    })

    await uploadFileMultipart(file, 'k', 'video/mp4', deps)

    expect(deps.createMultipartUpload).toHaveBeenCalledWith({ key: 'k', contentType: 'video/mp4' })
    expect(deps.putPart).toHaveBeenCalledTimes(1)
  })

  it('ignores a persisted session recorded for a different file', async () => {
    localStorage.setItem(
      'multipart-upload:k',
      JSON.stringify({ uploadId: 'upload-old', fingerprint: 'other-file:1:1' }),
    )
    const deps = makeDeps()

    await uploadFileMultipart(fakeFile(PART_SIZE_BYTES), 'k', 'video/mp4', deps)

    expect(deps.createMultipartUpload).toHaveBeenCalled()
    expect(deps.listUploadedParts).not.toHaveBeenCalled()
  })

  it('retries a failing part before giving up', async () => {
    let attempts = 0
    const deps = makeDeps({
      putPart: vi.fn().mockImplementation(() => {
        attempts++
        return attempts < 3 ? Promise.reject(new Error('network blip')) : Promise.resolve()
      }),
    })

    await uploadFileMultipart(fakeFile(10), 'k', 'video/mp4', deps)

    expect(attempts).toBe(3)
    expect(deps.completeMultipartUpload).toHaveBeenCalled()
  })

  it('gives up after exhausting retries and leaves the session for a later retry', async () => {
    const deps = makeDeps({
      putPart: vi.fn().mockRejectedValue(new Error('network down')),
    })

    await expect(uploadFileMultipart(fakeFile(10), 'k', 'video/mp4', deps)).rejects.toThrow(
      /part 1/,
    )

    expect(deps.completeMultipartUpload).not.toHaveBeenCalled()
    expect(localStorage.getItem('multipart-upload:k')).not.toBeNull()
  })

  it('aborts and clears the session when cancelled between parts', async () => {
    const deps = makeDeps()
    const controller = new AbortController()
    controller.abort()

    await expect(
      uploadFileMultipart(fakeFile(PART_SIZE_BYTES), 'k', 'video/mp4', deps, {
        signal: controller.signal,
      }),
    ).rejects.toThrow(UploadCancelledError)

    expect(deps.abortMultipartUpload).toHaveBeenCalledWith({ key: 'k', uploadId: 'upload-1' })
    expect(deps.putPart).not.toHaveBeenCalled()
    expect(localStorage.getItem('multipart-upload:k')).toBeNull()
  })

  it('aborts a part transfer already in flight and clears the session', async () => {
    const controller = new AbortController()
    const deps = makeDeps({
      putPart: vi.fn(
        (): Promise<void> =>
          new Promise((_resolve, reject) => {
            controller.signal.addEventListener('abort', () => reject(new Error('aborted')))
          }),
      ),
    })

    const promise = uploadFileMultipart(fakeFile(PART_SIZE_BYTES), 'k', 'video/mp4', deps, {
      signal: controller.signal,
    })
    controller.abort()

    await expect(promise).rejects.toThrow(UploadCancelledError)
    expect(deps.abortMultipartUpload).toHaveBeenCalledWith({ key: 'k', uploadId: 'upload-1' })
    expect(localStorage.getItem('multipart-upload:k')).toBeNull()
  })
})
