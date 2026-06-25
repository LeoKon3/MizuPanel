import { render, screen } from '@testing-library/react'
import { describe, expect, test } from 'vitest'

import { K8sStatusBadge } from './K8sStatusBadge'

describe('K8sStatusBadge', () => {
  test('keeps long status text inside compact table and drawer cells', () => {
    render(<K8sStatusBadge status="ContainerCreatingWithAVeryLongReason" />)

    const badge = screen.getByText('ContainerCreatingWithAVeryLongReason')
    expect(badge).toHaveClass('max-w-full')
    expect(badge).toHaveClass('truncate')
    expect(badge).toHaveAttribute('title', 'ContainerCreatingWithAVeryLongReason')
  })
})
