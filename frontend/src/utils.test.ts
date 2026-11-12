import { describe, it, expect } from 'vitest'
import { money, safeUrl } from './utils'
describe('company display safety', () => {
  it('does not display unknown funding as zero', () => { expect(money(null)).toBe('Not verified'); expect(money(3000000)).toBe('$3M') })
  it('blocks executable or invalid research links', () => { expect(safeUrl('javascript:alert(1)')).toBeUndefined(); expect(safeUrl('not a url')).toBeUndefined(); expect(safeUrl('https://example.com')).toBe('https://example.com/') })
})
