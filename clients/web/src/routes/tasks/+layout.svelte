<script lang="ts">
	import { page } from '$app/stores';

	let { children } = $props();

	const tabs = [
		{ path: '/tasks/backlog', label: 'BACKLOG' },
		{ path: '/tasks/board',   label: 'BOARD'   },
		{ path: '/tasks/scrum',   label: 'SCRUM'   },
	];
</script>

<div class="tasks-shell">
	<nav class="tasks-tabs">
		{#each tabs as tab}
			<a href={tab.path} class="tab" class:active={$page.url.pathname.startsWith(tab.path)}>
				{tab.label}
			</a>
		{/each}
	</nav>
	<div class="tasks-content">
		{@render children()}
	</div>
</div>

<style>
	.tasks-shell { display: flex; flex-direction: column; height: 100%; gap: 0; }
	.tasks-tabs { display: flex; gap: 2px; border-bottom: 1px solid var(--hud-dim); padding-bottom: 0; flex-shrink: 0; }
	.tab {
		padding: 6px 20px;
		font-family: var(--hud-font-hud);
		font-size: 10px;
		letter-spacing: 0.15em;
		text-transform: uppercase;
		color: var(--hud-muted);
		text-decoration: none;
		border: 1px solid transparent;
		border-bottom: none;
		clip-path: polygon(6px 0%, 100% 0%, calc(100% - 6px) 100%, 0% 100%);
		transition: all 0.15s ease;
	}
	.tab:hover, .tab.active {
		color: var(--hud-cyan);
		border-color: var(--hud-cyan);
		background: #00d4ff0a;
		text-shadow: var(--glow-cyan);
	}
	.tasks-content { flex: 1; overflow: auto; padding-top: 16px; }
</style>
