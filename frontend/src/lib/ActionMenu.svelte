<script lang="ts">
  import { onMount } from 'svelte'
  import { ChevronDown } from '@lucide/svelte'

  let menu: HTMLDetailsElement
  onMount(() => {
    const outside = (event: PointerEvent) => {
      if (!menu.contains(event.target as Node)) menu.open = false
    }
    const escape = (event: KeyboardEvent) => {
      if (menu.open && event.key === 'Escape') {
        event.preventDefault()
        event.stopPropagation()
        menu.open = false
        menu.querySelector('summary')?.focus()
      }
    }
    const choose = (event: MouseEvent) => {
      if ((event.target as Element).closest('button')) menu.open = false
    }
    document.addEventListener('pointerdown', outside)
    menu.addEventListener('keydown', escape)
    menu.addEventListener('click', choose)
    return () => {
      document.removeEventListener('pointerdown', outside)
      menu.removeEventListener('keydown', escape)
      menu.removeEventListener('click', choose)
    }
  })
</script>

<details bind:this={menu} class="action-menu">
  <summary>More actions <ChevronDown size={12}/></summary>
  <div class="action-menu-items"><slot /></div>
</details>

<style>
  .action-menu { position: relative; }
  summary { display: flex; align-items: center; gap: 6px; min-height: var(--control-compact); padding: 6px 8px; color: var(--text-action); font-size: var(--control-font-size); line-height: 20px; cursor: pointer; list-style: none; border-radius: var(--control-radius); }
  summary::-webkit-details-marker { display: none; }
  summary:hover { background: var(--raised); color: var(--text); }
  summary:focus-visible { outline: 2px solid var(--signal); outline-offset: 2px; }
  .action-menu-items { position: absolute; right: 0; top: calc(100% + 6px); z-index: 2; min-width: 180px; padding: 5px; border: 1px solid var(--rule); border-radius: 4px; background: var(--raised); box-shadow: 0 8px 24px #0006; }
  .action-menu-items :global(button) { display: block; width: 100%; min-height: var(--control-compact); padding: 6px 10px; border: 0; border-radius: var(--control-radius); background: transparent; color: var(--text); text-align: left; font-size: var(--control-font-size); }
  .action-menu-items :global(button:hover:not(:disabled)) { background: var(--control-hover); }
  .action-menu-items :global(button.danger-text) { color: var(--danger-text); }
</style>
