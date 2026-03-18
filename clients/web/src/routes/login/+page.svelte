<script lang="ts">
	import { goto } from '$app/navigation';
	import { tick } from 'svelte';
	import { auth } from '$lib/stores/auth';
	import { security } from '$lib/api/client';

	let subjectId = $state('tony-stark');
	let credential = $state('');
	let error = $state('');
	let loading = $state(false);

	async function handleLogin(e: Event) {
		e.preventDefault();
		error = '';
		loading = true;
		try {
			const res = await security.authenticate(subjectId, credential);
			auth.login(res.accessToken, subjectId);
			await tick();
			goto('/');
		} catch (err: unknown) {
			error = err instanceof Error ? err.message : 'Authentication failed';
		} finally {
			loading = false;
		}
	}
</script>

<div class="login-wrap">
	<div class="login-panel hud-panel">
		<div class="login-header">
			<div class="hud-label" style="font-size:11px; letter-spacing:0.4em">J.A.R.V.I.S.</div>
			<div class="login-subtitle text-muted">AUTHENTICATION REQUIRED</div>
		</div>

		<form onsubmit={handleLogin} class="login-form">
			<div class="field">
				<label class="hud-label" for="subject">SUBJECT ID</label>
				<input id="subject" class="hud-input" bind:value={subjectId} autocomplete="username" placeholder="tony-stark" required />
			</div>
			<div class="field">
				<label class="hud-label" for="cred">CREDENTIAL</label>
				<input id="cred" type="password" class="hud-input" bind:value={credential} autocomplete="current-password" placeholder="optional" />
			</div>

			{#if error}
				<div class="error text-red">{error}</div>
			{/if}

			<button type="submit" class="hud-btn login-btn" disabled={loading}>
				{loading ? 'AUTHENTICATING...' : 'AUTHENTICATE'}
			</button>
		</form>
	</div>
</div>

<style>
	.login-wrap { display: flex; align-items: center; justify-content: center; height: 100vh; background: var(--hud-bg); }
	.login-panel { width: 380px; padding: 40px; }
	.login-header { text-align: center; margin-bottom: 32px; }
	.login-subtitle { font-size: 11px; letter-spacing: 0.2em; margin-top: 8px; }
	.login-form { display: flex; flex-direction: column; gap: 16px; }
	.field { display: flex; flex-direction: column; gap: 6px; }
	.error { font-size: 12px; padding: 8px; border: 1px solid var(--hud-red); background: #ff2d5510; }
	.login-btn { width: 100%; padding: 10px; margin-top: 8px; }
</style>
