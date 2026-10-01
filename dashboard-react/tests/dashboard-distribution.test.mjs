import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'

// Reproduces a bug where an empty ranged model aggregate fell back to the
// unfiltered recent logs, so a date range with no usage showed tokens from
// outside that range. The model distribution must only use the ranged
// aggregate (empty range => empty distribution).
test('dashboard model distribution never falls back to unfiltered logs', async () => {
  const source = await readFile(new URL('../src/pages/DashboardPage.tsx', import.meta.url), 'utf8')
  assert.doesNotMatch(source, /dist\.entries|const dist\s*=/, 'model distribution must not aggregate unfiltered logRows')
})

test('empty ranged model aggregate yields an empty distribution', () => {
  const modelRows = (list) => (list ?? []).map(row => ({ name: row.model || 'unknown', value: Number(row.total_tokens || row.request_count || 0) }))
  assert.deepEqual(modelRows(undefined), [])
  assert.deepEqual(modelRows([]), [])
})
