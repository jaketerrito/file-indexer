import { createServerFn } from '@tanstack/react-start'
import { getFilesClient, getSearchClient } from './clients'
import {
  commitUploadImpl,
  deleteDirectoryImpl,
  deleteFileImpl,
  getDirectoryStatsImpl,
  getDownloadUrlImpl,
  getFileMetadataImpl,
  getUploadUrlImpl,
  listDirectoryImpl,
  listFilesImpl,
  validateIdInput,
  validateKeyInput,
  validateListDirectoryInput,
  validateListFilesInput,
  validatePathInput,
} from './impl'

// Thin server-function wrappers; all real logic lives in impl.ts where it is
// unit tested with injected fake clients.

export const listFiles = createServerFn({ method: 'GET' })
  .validator(validateListFilesInput)
  .handler(({ data }) => listFilesImpl(getSearchClient(), getFilesClient(), data))

export const getDownloadUrl = createServerFn({ method: 'GET' })
  .validator(validateIdInput)
  .handler(({ data }) => getDownloadUrlImpl(getFilesClient(), data.id))

export const deleteFile = createServerFn({ method: 'POST' })
  .validator(validateIdInput)
  .handler(({ data }) => deleteFileImpl(getFilesClient(), data.id))

export const getFileMetadata = createServerFn({ method: 'GET' })
  .validator(validateIdInput)
  .handler(({ data }) => getFileMetadataImpl(getFilesClient(), data.id))

export const getUploadUrl = createServerFn({ method: 'GET' })
  .validator(validateKeyInput)
  .handler(({ data }) => getUploadUrlImpl(getFilesClient(), data.key))

export const commitUpload = createServerFn({ method: 'POST' })
  .validator(validateKeyInput)
  .handler(({ data }) => commitUploadImpl(getFilesClient(), data.key))

export const listDirectory = createServerFn({ method: 'GET' })
  .validator(validateListDirectoryInput)
  .handler(({ data }) => listDirectoryImpl(getSearchClient(), getFilesClient(), data))

export const getDirectoryStats = createServerFn({ method: 'GET' })
  .validator(validatePathInput)
  .handler(({ data }) => getDirectoryStatsImpl(getFilesClient(), data.path))

export const deleteDirectory = createServerFn({ method: 'POST' })
  .validator(validatePathInput)
  .handler(({ data }) => deleteDirectoryImpl(getFilesClient(), data.path))
