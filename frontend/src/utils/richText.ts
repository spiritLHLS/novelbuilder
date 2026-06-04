import { marked } from 'marked'

marked.setOptions({
  gfm: true,
  breaks: true,
})

function escapeHtml(input: string): string {
  return input
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

function decodeHtmlAttribute(input: string): string {
  return input
    .replace(/&amp;/gi, '&')
    .replace(/&colon;/gi, ':')
    .replace(/&#0*58;/gi, ':')
    .replace(/&#x0*3a;/gi, ':')
}

function isSafeUrl(raw: string): boolean {
  const decoded = decodeHtmlAttribute(raw).trim()
  if (!decoded) return false
  if (decoded.startsWith('#') || decoded.startsWith('/') || decoded.startsWith('./') || decoded.startsWith('../')) {
    return true
  }
  const compact = decoded.replace(/[\u0000-\u001F\u007F\s]+/g, '')
  const scheme = compact.match(/^([a-z][a-z0-9+.-]*):/i)?.[1]?.toLowerCase()
  if (!scheme) return true
  return scheme === 'http' || scheme === 'https' || scheme === 'mailto'
}

function sanitizeRenderedHtml(html: string): string {
  return html
    .replace(/\s(?:on[a-z]+|style)\s*=\s*(?:"[^"]*"|'[^']*'|[^\s>]+)/gi, '')
    .replace(/\s(href|src)\s*=\s*("([^"]*)"|'([^']*)'|([^\s>]+))/gi, (_match, attr, _value, doubleQuoted, singleQuoted, bare) => {
      const raw = doubleQuoted ?? singleQuoted ?? bare ?? ''
      if (!isSafeUrl(raw)) return ''
      return ` ${String(attr).toLowerCase()}="${escapeHtml(raw)}"`
    })
}

export function toPlainText(value: unknown): string {
  if (value == null) return ''
  if (typeof value === 'string') return value
  if (Array.isArray(value)) return value.map(item => toPlainText(item)).join('\n')
  if (typeof value === 'object') return JSON.stringify(value, null, 2)
  return String(value)
}

export function renderRichText(value: unknown): string {
  const plain = toPlainText(value).replace(/\r\n/g, '\n').trim()
  if (!plain) return ''
  return sanitizeRenderedHtml(String(marked.parse(escapeHtml(plain), { async: false })))
}
