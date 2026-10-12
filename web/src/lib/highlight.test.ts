import { describe, expect, it } from 'vitest'
import { splitLiteralMatches } from './highlight'

describe('splitLiteralMatches', () => {
  it('matches punctuation literally without constructing a regular expression', () => {
    expect(splitLiteralMatches('a [x] and .* and (x)', '[x]')).toEqual([
      { text: 'a ', highlighted: false },
      { text: '[x]', highlighted: true },
      { text: ' and .* and (x)', highlighted: false },
    ])
    expect(splitLiteralMatches('a \\ path', '\\')).toEqual([
      { text: 'a ', highlighted: false },
      { text: '\\', highlighted: true },
      { text: ' path', highlighted: false },
    ])
  })

  it('matches case-insensitively and preserves original text', () => {
    expect(splitLiteralMatches('Error ERROR', 'error')).toEqual([
      { text: 'Error', highlighted: true },
      { text: ' ', highlighted: false },
      { text: 'ERROR', highlighted: true },
    ])
  })
})
