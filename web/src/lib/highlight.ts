export type HighlightPart = {
  text: string
  highlighted: boolean
}

/** Split text into literal, case-insensitive search matches without using a regex. */
export function splitLiteralMatches(text: string, query: string): HighlightPart[] {
  const normalizedQuery = query.trim()
  if (!normalizedQuery) return [{ text, highlighted: false }]

  const parts: HighlightPart[] = []
  const normalizedText = text.toLocaleLowerCase()
  const normalizedSearch = normalizedQuery.toLocaleLowerCase()
  let cursor = 0

  while (cursor < text.length) {
    const matchIndex = normalizedText.indexOf(normalizedSearch, cursor)
    if (matchIndex < 0) break
    if (matchIndex > cursor) {
      parts.push({ text: text.slice(cursor, matchIndex), highlighted: false })
    }
    parts.push({
      text: text.slice(matchIndex, matchIndex + normalizedQuery.length),
      highlighted: true,
    })
    cursor = matchIndex + normalizedQuery.length
  }

  if (cursor < text.length) {
    parts.push({ text: text.slice(cursor), highlighted: false })
  }

  return parts.length > 0 ? parts : [{ text, highlighted: false }]
}
