import { expect, test } from 'vitest'

import indexHTML from '../index.html?raw'
import logoSVG from '../public/logo.svg?raw'
import prototypeHTML from '../prototype/index.html?raw'
import config from '../vite.config'

test('api proxy preserves browser host for same-origin protected operations', () => {
  const proxy = config.server?.proxy
  const apiProxy = proxy && !Array.isArray(proxy) ? proxy['/api'] : undefined

  expect(apiProxy).toMatchObject({
    target: 'http://localhost:8080',
    changeOrigin: false,
    ws: true,
  })
})

test('dev proxy serves backend script and download assets used by SSH install', () => {
  const proxy = config.server?.proxy
  const scriptProxy = proxy && !Array.isArray(proxy) ? proxy['/scripts'] : undefined
  const downloadProxy = proxy && !Array.isArray(proxy) ? proxy['/downloads'] : undefined

  expect(scriptProxy).toMatchObject({ target: 'http://localhost:8080', changeOrigin: false })
  expect(downloadProxy).toMatchObject({ target: 'http://localhost:8080', changeOrigin: false })
})

test('production and prototype use the same MizuPanel vector mark as the favicon', () => {
  const icon = '<link rel="icon" type="image/svg+xml" sizes="any" href="/logo.svg" />'
  expect(indexHTML).toContain(icon)
  expect(prototypeHTML).toContain(icon)
  expect(logoSVG).toContain('MizuPanel')
  expect(logoSVG).not.toContain('data:image')
})
