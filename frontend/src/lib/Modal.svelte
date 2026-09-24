<script lang="ts">
  import { onMount } from 'svelte'

  export let label: string
  export let onClose: () => void
  export let role: 'dialog' | 'alertdialog' = 'dialog'

  let dialog: HTMLDialogElement

  onMount(() => {
    const trigger = document.activeElement as HTMLElement | null
    let pressedBackdrop = false
    const cancel = (event: Event) => {
      event.preventDefault()
      onClose()
    }
    const pointerDown = (event: PointerEvent) => {
      pressedBackdrop = event.target === dialog && event.button === 0
    }
    const click = (event: MouseEvent) => {
      if (pressedBackdrop && event.target === dialog) onClose()
      pressedBackdrop = false
    }
    dialog.addEventListener('cancel', cancel)
    dialog.addEventListener('pointerdown', pointerDown)
    dialog.addEventListener('click', click)
    dialog.showModal()
    dialog.querySelector<HTMLElement>('[data-modal-initial-focus]')?.focus({ preventScroll: true })
    return () => {
      dialog.removeEventListener('cancel', cancel)
      dialog.removeEventListener('pointerdown', pointerDown)
      dialog.removeEventListener('click', click)
      dialog.close()
      if (trigger?.isConnected) trigger.focus()
    }
  })
</script>

<dialog bind:this={dialog} {role} aria-label={label} aria-modal="true">
  <slot />
</dialog>

<style>
  dialog { padding: 0; border: 0; background: transparent; color: var(--text); max-width: 95vw; max-height: 95vh; overflow: visible; }
  dialog::backdrop { background: #0c0d0ee0; }
</style>
