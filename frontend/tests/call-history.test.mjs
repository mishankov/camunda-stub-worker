import assert from 'node:assert/strict'
import fs from 'node:fs'
import test from 'node:test'
import vm from 'node:vm'
import ts from 'typescript'

// Exercise the actual card-loading and confirmation handlers with deferred
// backend responses, without requiring a desktop Wails session.
const app = fs.readFileSync(new URL('../src/App.svelte', import.meta.url), 'utf8')
const script = app.match(/<script lang="ts">([\s\S]*?)<\/script>/)[1]
const ast = ts.createSourceFile('App.ts', script, ts.ScriptTarget.Latest, true)
const handlerNames = ['loadJobCallHistory', 'requestClearHistory']
const handlers = ast.statements
  .filter(node => ts.isFunctionDeclaration(node) && handlerNames.includes(node.name?.text))
  .map(node => node.getText(ast)).join('\n')
assert.equal(handlers.includes('function loadJobCallHistory'), true)
assert.equal(handlers.includes('function requestClearHistory'), true)
const compiled = ts.transpileModule(handlers, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText

function setup(call) {
  const state = {
    selectedProfileId: 'profile', jobTypes: [{ id: 'type-a' }, { id: 'type-b' }],
    jobCallHistory: { 'type-a': [{ activationId: 'deleted' }], 'type-b': [], other: [{ activationId: 'other-profile' }] },
    jobCallHistoryErrors: {}, jobCallHistoryRequests: {}, confirmation: null,
    call, historyPages: [],
    loadHistory: async page => { state.historyPages.push(page) },
  }
  const methods = vm.runInNewContext(`${compiled}\n({loadJobCallHistory, requestClearHistory})`, state)
  return { state, ...methods }
}

test('clearing completed history refreshes every card and preserves active calls', async () => {
  const active = [{ activationId: 'active' }]
  const requests = []
  let cleared = false
  const { state, requestClearHistory } = setup(async (name, ...args) => {
    requests.push([name, ...args])
    if (name === 'ClearHistory') { cleared = true; return 1 }
    assert.equal(cleared, true)
    return args[0] === 'type-a' ? active : []
  })
  requestClearHistory()
  await state.confirmation.action()
  assert.deepEqual(requests, [['ClearHistory', 'profile', true], ['JobCallHistory', 'type-a'], ['JobCallHistory', 'type-b']])
  assert.deepEqual(state.historyPages, [1])
  assert.deepEqual(state.jobCallHistory['type-a'], active)
  assert.deepEqual(state.jobCallHistory['type-b'], [])
  assert.equal(state.jobCallHistory.other[0].activationId, 'other-profile')
})

test('a response started before clearing cannot restore deleted rows', async () => {
  let finishOldLoad
  let loads = 0
  const { state, loadJobCallHistory, requestClearHistory } = setup(async (name, id) => {
    if (name === 'ClearHistory') return 1
    if (id === 'type-a' && loads++ === 0) return new Promise(resolve => { finishOldLoad = resolve })
    return []
  })
  const oldLoad = loadJobCallHistory('type-a')
  requestClearHistory()
  await state.confirmation.action()
  finishOldLoad([{ activationId: 'deleted' }])
  await oldLoad
  assert.deepEqual(state.jobCallHistory['type-a'], [])
})

test('a failed refresh after clearing hides deleted rows and exposes retry error', async () => {
  const { state, requestClearHistory } = setup(async name => {
    if (name === 'ClearHistory') return 1
    throw new Error('storage unavailable')
  })
  requestClearHistory()
  await state.confirmation.action()
  assert.equal(state.jobCallHistory['type-a'], undefined)
  assert.equal(state.jobCallHistoryErrors['type-a'], 'storage unavailable')
})

test('a failed deletion keeps the existing card history', async () => {
  const { state, requestClearHistory } = setup(async () => { throw new Error('deletion failed') })
  requestClearHistory()
  await assert.rejects(state.confirmation.action(), /deletion failed/)
  assert.equal(state.jobCallHistory['type-a'][0].activationId, 'deleted')
  assert.deepEqual(state.historyPages, [])
})
