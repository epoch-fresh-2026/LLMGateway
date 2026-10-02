import test from 'node:test'
import assert from 'node:assert/strict'
import { readdir, readFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'

test('breaker configuration is user-scoped while channel health and testing remain independent', async () => {
  const files = ['api/catalog.ts', 'api/paths.ts', 'api/client.ts', 'api/runtime/catalog.ts', 'types/api.ts', 'pages/ChannelsPage.tsx']
  for (const file of files) {
    const source = await readFile(new URL(`../src/${file}`, import.meta.url), 'utf8')
    assert.doesNotMatch(source, /ChannelBreakerConfig|channelBreaker|channel-breaker|\/breaker`|setGlobalBreaker|全局熔断配置|恢复全局默认/, file)
  }
  const catalog = await readFile(new URL('../src/api/catalog.ts', import.meta.url), 'utf8')
  const paths = await readFile(new URL('../src/api/paths.ts', import.meta.url), 'utf8')
  const page = await readFile(new URL('../src/pages/ChannelsPage.tsx', import.meta.url), 'utf8')
  assert.match(paths, /breakerConfig:.*\/admin\/breaker-config/)
  for (const operation of ['getUserBreakerConfig', 'updateUserBreakerConfig', 'deleteUserBreakerConfig']) {
    assert.match(catalog, new RegExp(`export const ${operation} = .*apiPaths\\.breakerConfig\\(\\)`))
    assert.match(page, new RegExp(operation))
  }
  for (const operation of ['listChannelHealth', 'resetChannelHealth', 'testChannel']) {
    assert.match(catalog, new RegExp(`export const ${operation}`))
    assert.match(page, new RegExp(operation))
  }
  assert.match(page, /title="用户熔断配置"/)
  assert.match(page, /setUserBreaker\(true\).*用户熔断配置/)
})

test('pages do not depend on the transport client', async () => {
  const pagesDir = join(dirname(fileURLToPath(import.meta.url)), '..', 'src', 'pages')
  const files = await readdir(pagesDir)
  for (const file of files.filter(name => name.endsWith('.tsx'))) {
    const source = await readFile(join(pagesDir, file), 'utf8')
    assert.doesNotMatch(source, /api\/client|adminGet|adminSend|catalogRequest|accountGet|accountRequest|rateLimitRequest|quotaRequest/, `${file} bypasses domain API modules`)
  }
})
