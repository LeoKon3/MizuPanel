import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useEffect, useState } from 'react'
import { afterEach, beforeEach, describe, expect, test } from 'vitest'
import { usePanelNavigation } from './usePanelNavigation'
import { useUnsavedChangesGuard } from '../components/UnsavedChangesDialog'

function Harness() {
  const [path, setPath] = useState(() => window.location.pathname)
  const [draft, setDraft] = useState('')
  const { navigate, registerGuard } = usePanelNavigation(() => setPath(window.location.pathname))
  const { request, dialog } = useUnsavedChangesGuard(Boolean(draft), () => setDraft(''))
  useEffect(() => {
    registerGuard(draft ? request : undefined)
    return () => registerGuard(undefined)
  }, [draft, request, registerGuard])
  return <>
    <output data-testid="current-path">{path}</output>
    <input aria-label="草稿" value={draft} onChange={event => setDraft(event.target.value)} />
    <button onClick={() => navigate('/nodes/alpha')}>Alpha</button>
    <button onClick={() => navigate('/nodes/beta')}>Beta</button>
    {dialog}
  </>
}

beforeEach(() => window.history.replaceState({}, '', '/nodes'))
afterEach(cleanup)

describe('panel navigation safety', () => {
  test('prompts before route changes and discards only on explicit confirmation', () => {
    render(<Harness />)
    fireEvent.click(screen.getByText('Alpha'))
    fireEvent.change(screen.getByLabelText('草稿'), { target: { value: 'alpha only' } })
    fireEvent.click(screen.getByText('Beta'))
    expect(window.location.pathname).toBe('/nodes/alpha')
    expect(screen.getByRole('dialog', { name: '未保存的修改' })).toBeInTheDocument()
    fireEvent.click(screen.getByText('继续编辑'))
    expect(screen.getByLabelText('草稿')).toHaveValue('alpha only')
    fireEvent.click(screen.getByText('Beta'))
    fireEvent.click(screen.getByText('丢弃并离开'))
    expect(screen.getByTestId('current-path')).toHaveTextContent('/nodes/beta')
    expect(window.location.pathname).toBe('/nodes/beta')
    expect(screen.getByLabelText('草稿')).toHaveValue('')
  })
  test('restores URL on cancelled browser back, then permits leaving without mismatched state', async () => {
    render(<Harness />)
    fireEvent.click(screen.getByText('Alpha'))
    fireEvent.click(screen.getByText('Beta'))
    fireEvent.change(screen.getByLabelText('草稿'), { target: { value: 'beta only' } })
    act(() => window.history.back())
    await screen.findByRole('dialog', { name: '未保存的修改' })
    await waitFor(() => expect(window.location.pathname).toBe('/nodes/beta'))
    fireEvent.click(screen.getByText('继续编辑'))
    expect(screen.getByLabelText('草稿')).toHaveValue('beta only')
    act(() => window.history.back())
    await screen.findByRole('dialog', { name: '未保存的修改' })
    await waitFor(() => expect(window.location.pathname).toBe('/nodes/beta'))
    fireEvent.click(screen.getByText('丢弃并离开'))
    await waitFor(() => expect(window.location.pathname).toBe('/nodes/alpha'))
    expect(screen.getByTestId('current-path')).toHaveTextContent('/nodes/alpha')
    expect(screen.getByLabelText('草稿')).toHaveValue('')
  })
  test('Escape cancels and focus returns to the triggering control', () => {
    render(<Harness />)
    fireEvent.change(screen.getByLabelText('草稿'), { target: { value: 'keep' } })
    screen.getByText('Alpha').focus()
    fireEvent.click(screen.getByText('Alpha'))
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.getByText('Alpha')).toHaveFocus()
    expect(window.location.pathname).toBe('/nodes')
  })
})
