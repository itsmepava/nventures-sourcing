import { describe, expect, it } from 'vitest'
import { csvText,readPreference } from './preferences'
describe('workspace preferences and exports',()=>{
 it('falls back safely when browser storage is unavailable',()=>{expect(readPreference('theme','system')).toBe('system')})
 it('quotes commas, newlines and quotes and neutralizes spreadsheet formulas',()=>{const csv=csvText([['=IMPORTXML("secret")','+cmd','@formula','normal, value','two\nlines']]);expect(csv).toContain('"\'=IMPORTXML(""secret"")"');expect(csv).toContain('"\'+cmd"');expect(csv).toContain('"\'@formula"');expect(csv).toContain('"normal, value"');expect(csv).toContain('"two\nlines"')})
})
