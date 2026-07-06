import { createFileRoute } from '@tanstack/react-router'
import { FileList } from '../components/FileList'

export const Route = createFileRoute('/')({
  component: Home,
})

function Home() {
  return (
    <main>
      <h1>Files</h1>
      <FileList />
    </main>
  )
}
