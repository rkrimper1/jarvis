<script lang="ts">
	import { onMount } from 'svelte';
	import { users, type User } from '$lib/api/client';
	import { auth, subjectId, userId } from '$lib/stores/auth';

	let user = $state<User | null>(null);
	let loading = $state(true);
	let saveError = $state('');
	let saveSuccess = $state('');
	let saving = $state(false);

	// Profile fields
	let displayName = $state('');
	let email = $state('');

	// Password fields
	let currentPassword = $state('');
	let newPassword = $state('');
	let confirmPassword = $state('');
	let pwError = $state('');
	let pwSuccess = $state('');
	let pwSaving = $state(false);

	onMount(async () => {
		try {
			const res = await users.getMe($subjectId ?? '');
			user = res.user;
			displayName = user.displayName ?? '';
			email = user.email ?? '';
		} catch (err: unknown) {
			saveError = err instanceof Error ? err.message : 'Failed to load profile';
		} finally {
			loading = false;
		}
	});

	async function saveProfile(e: Event) {
		e.preventDefault();
		saveError = ''; saveSuccess = ''; saving = true;
		try {
			const res = await users.updateProfile($userId ?? user?.id ?? '', email, displayName);
			user = res.user;
			displayName = user.displayName;
			email = user.email;
			saveSuccess = 'Profile updated.';
		} catch (err: unknown) {
			saveError = err instanceof Error ? err.message : 'Save failed';
		} finally {
			saving = false;
		}
	}

	async function changePassword(e: Event) {
		e.preventDefault();
		pwError = ''; pwSuccess = '';
		if (newPassword !== confirmPassword) {
			pwError = 'New passwords do not match.';
			return;
		}
		pwSaving = true;
		try {
			await users.changePassword($userId ?? user?.id ?? '', currentPassword, newPassword);
			pwSuccess = 'Password changed.';
			currentPassword = ''; newPassword = ''; confirmPassword = '';
		} catch (err: unknown) {
			pwError = err instanceof Error ? err.message : 'Password change failed';
		} finally {
			pwSaving = false;
		}
	}
</script>

<div class="page-layout">
	<div class="hud-label" style="font-size:13px;margin-bottom:16px">USER PROFILE</div>

	{#if loading}
		<div class="text-muted" style="font-size:12px">Loading...</div>
	{:else}
	<div class="grid-2">
		<!-- ── Profile info ─────────────────────────────────── -->
		<div class="hud-panel">
			<div class="hud-label panel-title">IDENTITY</div>

			<div class="meta-row">
				<span class="hud-label">USERNAME</span>
				<span class="meta-val">{user?.username}</span>
			</div>
			<div class="meta-row">
				<span class="hud-label">ROLE</span>
				<span class="meta-val role-badge">{user?.role}</span>
			</div>

			<form onsubmit={saveProfile} class="form" style="margin-top:16px">
				<div class="field">
					<label class="hud-label" for="displayName">DISPLAY NAME</label>
					<input id="displayName" class="hud-input" bind:value={displayName} />
				</div>
				<div class="field">
					<label class="hud-label" for="email">EMAIL</label>
					<input id="email" type="email" class="hud-input" bind:value={email} />
				</div>
				{#if saveError}<div class="text-red msg">{saveError}</div>{/if}
				{#if saveSuccess}<div class="text-cyan msg">{saveSuccess}</div>{/if}
				<button type="submit" class="hud-btn" disabled={saving}>
					{saving ? 'SAVING...' : 'SAVE PROFILE'}
				</button>
			</form>
		</div>

		<!-- ── Change password ──────────────────────────────── -->
		<div class="hud-panel">
			<div class="hud-label panel-title">CHANGE PASSWORD</div>
			<form onsubmit={changePassword} class="form">
				<div class="field">
					<label class="hud-label" for="curPw">CURRENT PASSWORD</label>
					<input id="curPw" type="password" class="hud-input" bind:value={currentPassword} required />
				</div>
				<div class="field">
					<label class="hud-label" for="newPw">NEW PASSWORD</label>
					<input id="newPw" type="password" class="hud-input" bind:value={newPassword} required />
				</div>
				<div class="field">
					<label class="hud-label" for="confPw">CONFIRM NEW PASSWORD</label>
					<input id="confPw" type="password" class="hud-input" bind:value={confirmPassword} required />
				</div>
				{#if pwError}<div class="text-red msg">{pwError}</div>{/if}
				{#if pwSuccess}<div class="text-cyan msg">{pwSuccess}</div>{/if}
				<button type="submit" class="hud-btn" disabled={pwSaving}>
					{pwSaving ? 'UPDATING...' : 'CHANGE PASSWORD'}
				</button>
			</form>
		</div>
	</div>
	{/if}
</div>

<style>
	.page-layout { display: flex; flex-direction: column; height: 100%; }
	.grid-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
	.hud-panel { padding: 16px; }
	.panel-title { display: block; margin-bottom: 16px; }
	.form { display: flex; flex-direction: column; gap: 12px; }
	.field { display: flex; flex-direction: column; gap: 6px; }
	.meta-row { display: flex; justify-content: space-between; align-items: center; padding: 6px 0; border-bottom: 1px solid var(--hud-dim); font-size: 12px; }
	.meta-val { color: var(--hud-text); }
	.role-badge { color: var(--hud-cyan); font-size: 10px; letter-spacing: 0.1em; }
	.msg { font-size: 11px; padding: 6px; }
	.text-cyan { color: var(--hud-cyan); }
</style>
