import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, test } from 'vitest'
import { BrandLogo } from './BrandLogo'
import logoSVG from '../../public/logo.svg?raw'
import faviconSVG from '../../public/favicon.svg?raw'

afterEach(cleanup)

describe('MizuPanel vector mark', () => {
  test('uses a lightweight SVG while preserving the accessible logo contract', () => {
    render(<BrandLogo />)
    const logo = screen.getByRole('img', { name: 'MizuPanel logo' })
    expect(logo).toHaveAttribute('src', '/logo.svg')
    expect(logo).toHaveAttribute('data-testid', 'mizupanel-logo-mark')
    expect(logo).toHaveAttribute('width', '32')
    expect(logo).toHaveAttribute('height', '32')
    expect(logo).toHaveAttribute('draggable', 'false')
    expect(logo).toHaveClass('h-9', 'w-9')
  })

  test('keeps supplied sidebar dimensions without cropping or a background tile', () => {
    render(<BrandLogo className="h-7 w-7" />)
    const logo = screen.getByTestId('mizupanel-logo-mark')
    expect(logo).toHaveClass('h-7', 'w-7', 'shrink-0')
    expect(logo).not.toHaveClass('rounded-lg', 'object-cover')
  })

  test('is actual vector geometry with light, dark and forced-color treatments', () => {
    const svg = new DOMParser().parseFromString(logoSVG, 'image/svg+xml')
    expect(svg.querySelector('parsererror')).toBeNull()
    expect(svg.documentElement.getAttribute('viewBox')).toBe('0 0 32 32')
    expect(svg.querySelector('title')?.textContent).toBe('MizuPanel')
    expect(svg.querySelectorAll('path')).toHaveLength(2)
    expect(svg.querySelector('image, script, foreignObject, filter, linearGradient, radialGradient, rect')).toBeNull()
    expect(logoSVG).not.toMatch(/base64|https?:\/\/(?!www\.w3\.org\/2000\/svg)/)
    expect(logoSVG).toContain('prefers-color-scheme: dark')
    expect(logoSVG).toContain('forced-colors: active')
    expect(logoSVG.length).toBeLessThan(1024)
  })

  test('keeps the previous favicon URL compatible without a second mark design', () => {
    expect(faviconSVG).toBe(logoSVG)
  })
})
