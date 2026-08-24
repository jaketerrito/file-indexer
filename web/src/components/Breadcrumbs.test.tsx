import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { Breadcrumbs } from './Breadcrumbs'

describe('Breadcrumbs', () => {
  it('renders only Home (disabled) at the root', () => {
    const onNavigate = vi.fn()
    render(<Breadcrumbs path="" onNavigate={onNavigate} />)

    const home = screen.getByRole('button', { name: 'Home' }) as HTMLButtonElement
    expect(home.disabled).toBe(true)
    expect(screen.queryByText('/')).toBeNull()
  })

  it('renders a clickable segment per path component, with the last as current', () => {
    const onNavigate = vi.fn()
    render(<Breadcrumbs path="docs/sub/" onNavigate={onNavigate} />)

    const home = screen.getByRole('button', { name: 'Home' }) as HTMLButtonElement
    expect(home.disabled).toBe(false)
    expect(screen.getByRole('button', { name: 'docs' })).toBeDefined()
    // The last segment is not a link.
    expect(screen.queryByRole('button', { name: 'sub' })).toBeNull()
    expect(screen.getByText('sub').getAttribute('aria-current')).toBe('page')
  })

  it('navigates to the root when Home is clicked', () => {
    const onNavigate = vi.fn()
    render(<Breadcrumbs path="docs/sub/" onNavigate={onNavigate} />)

    fireEvent.click(screen.getByRole('button', { name: 'Home' }))
    expect(onNavigate).toHaveBeenCalledWith('')
  })

  it('navigates to the cumulative path of an ancestor segment', () => {
    const onNavigate = vi.fn()
    render(<Breadcrumbs path="docs/sub/deep/" onNavigate={onNavigate} />)

    fireEvent.click(screen.getByRole('button', { name: 'docs' }))
    expect(onNavigate).toHaveBeenCalledWith('docs/')

    fireEvent.click(screen.getByRole('button', { name: 'sub' }))
    expect(onNavigate).toHaveBeenCalledWith('docs/sub/')
  })
})
