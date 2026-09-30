import assert from 'node:assert/strict'
import test from 'node:test'
import { mergeFileDeployments } from '../src/lib/processes.ts'

const file = (version, id = 'order') => ({
  bpmnProcessId: id, name: `Local ${id}`, version, processDefinitionKey: `key-${id}-${version}`,
})

test('an empty or failed Operate refresh retains every local version', () => {
  const [process] = mergeFileDeployments([], [file(1), file(2)])
  assert.equal(process.latestVersion, 2)
  assert.deepEqual(process.versions, [
    { version: 2, key: 'key-order-2' },
    { version: 1, key: 'key-order-1' },
  ])
})

test('a partial Operate refresh retains all versions of missing local processes', () => {
  const remote = { bpmnProcessId: 'remote', name: 'Remote', latestVersion: 1, versions: [{ version: 1, key: 'remote-1' }] }
  const merged = mergeFileDeployments([remote], [file(2), file(1), file(1, 'shipping')])
  assert.deepEqual(merged.map(process => process.bpmnProcessId), ['remote', 'order', 'shipping'])
  assert.equal(merged[1].latestVersion, 2)
  assert.equal(merged[1].versions.length, 2)
  assert.equal(merged[2].versions[0].key, 'key-shipping-1')
})

test('overlapping versions appear once and newer Operate versions remain latest', () => {
  const remote = { bpmnProcessId: 'order', name: 'Remote order', latestVersion: 3, versions: [
    { version: 3, key: 'remote-3' }, { version: 2, key: 'key-order-2' },
  ] }
  const files = [file(1), file(2), file(2)]
  const original = structuredClone({ remote, files })
  const [process] = mergeFileDeployments([remote], files)
  assert.equal(process.name, 'Remote order')
  assert.equal(process.latestVersion, 3)
  assert.deepEqual(process.versions.map(version => version.version), [3, 2, 1])
  assert.deepEqual({ remote, files }, original)
})

test('local process names fill missing Operate names', () => {
  const [process] = mergeFileDeployments([
    { bpmnProcessId: 'order', name: '', latestVersion: 1, versions: [{ version: 1, key: 'key-order-1' }] },
  ], [file(2)])
  assert.equal(process.name, 'Local order')
})
