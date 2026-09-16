<script lang="ts">
  import { onDestroy, onMount } from 'svelte'
  import { basicSetup } from 'codemirror'
  import { EditorState } from '@codemirror/state'
  import { EditorView } from '@codemirror/view'
  import { HighlightStyle, syntaxHighlighting } from '@codemirror/language'
  import { json, jsonParseLinter } from '@codemirror/lang-json'
  import { linter } from '@codemirror/lint'
  import { tags } from '@lezer/highlight'

  export let value = ''
  export let ariaLabel = 'JSON editor'

  let host: HTMLDivElement
  let view: EditorView | null = null
  let applyingExternalChange = false

  const jsonHighlighting = HighlightStyle.define([
    { tag: tags.propertyName, color: '#8fc4bd' },
    { tag: tags.string, color: '#b5c99a' },
    { tag: [tags.number, tags.bool, tags.null], color: '#d6b475' },
    { tag: [tags.bracket, tags.punctuation], color: '#aeb4b8' },
    { tag: tags.invalid, color: '#d77a7e' }
  ])

  const appTheme = EditorView.theme({
    '&': {
      color: '#dfe2e3',
      backgroundColor: '#17191b',
      border: '1px solid #3b4044',
      borderRadius: '3px',
      fontSize: '14px'
    },
    '&.cm-focused': {
      borderColor: '#669e98',
      outline: '1px solid #669e98'
    },
    '.cm-scroller': {
      minHeight: '210px',
      maxHeight: '320px',
      overflow: 'auto',
      fontFamily: 'SFMono-Regular, SF Mono, Menlo, Consolas, monospace',
      lineHeight: '1.55'
    },
    '.cm-content': { padding: '10px 0', caretColor: '#e5e7e8' },
    '.cm-line': { padding: '0 12px' },
    '.cm-gutters': {
      color: '#697177',
      backgroundColor: '#1b1e20',
      border: '0',
      borderRight: '1px solid #303438'
    },
    '.cm-gutterElement': { padding: '0 9px 0 7px' },
    '.cm-activeLine': { backgroundColor: '#202427' },
    '.cm-activeLineGutter': { color: '#aeb4b8', backgroundColor: '#24282b' },
    '&.cm-focused .cm-selectionBackground, .cm-selectionBackground, ::selection': {
      backgroundColor: '#31514e'
    },
    '.cm-cursor, .cm-dropCursor': { borderLeftColor: '#dfe2e3' },
    '.cm-matchingBracket': { color: '#f0f2f2', backgroundColor: '#345b56' },
    '.cm-foldPlaceholder': {
      color: '#9ca3a8',
      backgroundColor: '#262a2e',
      border: '1px solid #3a3f44'
    },
    '.cm-tooltip': {
      color: '#dfe2e3',
      backgroundColor: '#262a2e',
      border: '1px solid #4a5054'
    },
    '.cm-diagnostic-error': { borderLeftColor: '#d77a7e' },
    '.cm-lintRange-error': { backgroundImage: 'none', borderBottom: '1px solid #d77a7e' }
  }, { dark: true })

  onMount(() => {
    view = new EditorView({
      parent: host,
      state: EditorState.create({
        doc: value,
        extensions: [
          basicSetup,
          json(),
          linter(jsonParseLinter()),
          syntaxHighlighting(jsonHighlighting),
          appTheme,
          EditorView.lineWrapping,
          EditorView.contentAttributes.of({ 'aria-label': ariaLabel }),
          EditorView.updateListener.of(update => {
            if (update.docChanged && !applyingExternalChange) value = update.state.doc.toString()
          })
        ]
      })
    })
  })

  $: if (view && value !== view.state.doc.toString()) {
    applyingExternalChange = true
    view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: value } })
    applyingExternalChange = false
  }

  onDestroy(() => view?.destroy())
</script>

<div class="json-editor" bind:this={host}></div>
