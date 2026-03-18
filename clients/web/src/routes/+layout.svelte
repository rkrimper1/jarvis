<script lang="ts">
	import '../app.css';
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { auth, isAuthenticated, subjectId } from '$lib/stores/auth';
	import { setToken } from '$lib/api/client';

	let { children } = $props();
	let hydrated = $state(false);

	auth.subscribe(($auth) => setToken($auth.token));

	const navItems = [
		{ path: '/',         label: 'System',   icon: '◈' },
		{ path: '/dialogue', label: 'Dialogue', icon: '◉' },
		{ path: '/schedule', label: 'Schedule', icon: '◷' },
		{ path: '/tasks',    label: 'Tasks',    icon: '◧' },
		{ path: '/intel',    label: 'Intel',    icon: '◎' },
		{ path: '/security', label: 'Security', icon: '◬' },
	];

	function handleLogout() {
		auth.logout();
		goto('/login');
	}

	$effect(() => {
		hydrated = true;
		if (!$isAuthenticated && $page.url.pathname !== '/login') {
			goto('/login');
		}
	});
</script>

{#if !hydrated}
	<!-- Blank screen while localStorage is read — prevents flash -->
	<div style="background:#050508;height:100vh"></div>
{:else if $isAuthenticated}
<div class="hud-shell">
	<header class="hud-topbar">
		<div class="hud-brand font-hud">J.A.R.V.I.S.</div>
		<nav class="hud-nav">
			{#each navItems as item}
				<a href={item.path} class="hud-nav-link" class:active={$page.url.pathname === item.path}>
					<span class="nav-icon">{item.icon}</span>
					<span class="nav-label">{item.label}</span>
				</a>
			{/each}
		</nav>
		<div class="hud-topbar-right">
			<span class="text-muted">{$subjectId}</span>
			<button class="hud-btn" onclick={handleLogout}>LOGOUT</button>
		</div>
	</header>

	<main class="hud-main">{@render children()}</main>

	<footer class="hud-statusbar">
		<span class="status-dot online"></span>
		<span class="text-cyan">SYSTEMS NOMINAL</span>
		<span class="text-muted" style="margin-left:auto">JARVIS v2.0 · REST :8080 · gRPC :50051</span>
	</footer>
</div>
{:else}
	{@render children()}
{/if}

<style>
	.hud-shell { display: grid; grid-template-rows: 48px 1fr 28px; height: 100vh; overflow: hidden; }
	.hud-topbar { display: flex; align-items: center; gap: 24px; padding: 0 20px; border-bottom: 1px solid var(--hud-cyan); box-shadow: 0 1px 0 #00d4ff44; background: var(--hud-panel); }
	.hud-brand { font-size: 16px; font-weight: 900; letter-spacing: 0.3em; color: var(--hud-cyan); text-shadow: var(--glow-cyan); white-space: nowrap; }
	.hud-nav { display: flex; gap: 4px; flex: 1; }
	.hud-nav-link { display: flex; align-items: center; gap: 6px; padding: 6px 12px; font-family: var(--hud-font-hud); font-size: 10px; letter-spacing: 0.15em; text-transform: uppercase; color: var(--hud-muted); text-decoration: none; border: 1px solid transparent; clip-path: polygon(6px 0%, 100% 0%, calc(100% - 6px) 100%, 0% 100%); transition: all 0.15s ease; }
	.hud-nav-link:hover, .hud-nav-link.active { color: var(--hud-cyan); border-color: var(--hud-cyan); background: #00d4ff0a; text-shadow: var(--glow-cyan); }
	.nav-icon { font-size: 12px; }
	.hud-topbar-right { display: flex; align-items: center; gap: 12px; margin-left: auto; font-size: 12px; }
	.hud-main { overflow: auto; padding: 20px; }
	.hud-statusbar { display: flex; align-items: center; gap: 10px; padding: 0 20px; font-size: 11px; border-top: 1px solid var(--hud-dim); background: var(--hud-panel); }
</style>
