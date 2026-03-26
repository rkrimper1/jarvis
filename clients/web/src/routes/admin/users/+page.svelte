<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { users, type User, type UserRole } from '$lib/api/client';
	import { isAdmin } from '$lib/stores/auth';

	let userList = $state<User[]>([]);
	let loading = $state(true);
	let error = $state('');

	// Create modal
	let showCreate = $state(false);
	let newUsername = $state('');
	let newEmail = $state('');
	let newDisplayName = $state('');
	let newPassword = $state('');
	let newRole = $state<UserRole>('ROLE_VIEWER');
	let createError = $state('');
	let creating = $state(false);

	onMount(async () => {
		if (!$isAdmin) { goto('/'); return; }
		await loadUsers();
	});

	async function loadUsers() {
		loading = true; error = '';
		try {
			const res = await users.list();
			userList = res.users ?? [];
		} catch (err: unknown) {
			error = err instanceof Error ? err.message : 'Failed to load users';
		} finally {
			loading = false;
		}
	}

	async function deleteUser(id: string, username: string) {
		if (!confirm(`Delete user "${username}"?`)) return;
		try {
			await users.delete(id);
			userList = userList.filter(u => u.id !== id);
		} catch (err: unknown) {
			error = err instanceof Error ? err.message : 'Delete failed';
		}
	}

	async function createUser(e: Event) {
		e.preventDefault();
		createError = ''; creating = true;
		try {
			const res = await users.create(newUsername, newEmail, newDisplayName, newPassword, newRole);
			userList = [...userList, res.user];
			showCreate = false;
			newUsername = ''; newEmail = ''; newDisplayName = ''; newPassword = ''; newRole = 'ROLE_VIEWER';
		} catch (err: unknown) {
			createError = err instanceof Error ? err.message : 'Create failed';
		} finally {
			creating = false;
		}
	}
</script>

<div class="page-layout">
	<div class="page-header">
		<div class="hud-label" style="font-size:13px">USER MANAGEMENT</div>
		<button class="hud-btn" onclick={() => showCreate = true}>+ CREATE USER</button>
	</div>

	{#if error}
		<div class="text-red" style="font-size:12px;margin-bottom:12px">{error}</div>
	{/if}

	{#if loading}
		<div class="text-muted" style="font-size:12px">Loading...</div>
	{:else}
	<div class="hud-panel">
		<table class="user-table">
			<thead>
				<tr>
					<th class="hud-label">USERNAME</th>
					<th class="hud-label">DISPLAY NAME</th>
					<th class="hud-label">EMAIL</th>
					<th class="hud-label">ROLE</th>
					<th class="hud-label">STATUS</th>
					<th class="hud-label">ACTIONS</th>
				</tr>
			</thead>
			<tbody>
				{#each userList as u (u.id)}
				<tr>
					<td>{u.username}</td>
					<td class="text-muted">{u.displayName || '—'}</td>
					<td class="text-muted">{u.email || '—'}</td>
					<td><span class="role-badge" class:admin={u.role === 'ROLE_ADMIN'}>{u.role}</span></td>
					<td><span class="status-dot" class:online={u.isActive} class:offline={!u.isActive}></span></td>
					<td>
						<button class="hud-btn btn-del" onclick={() => deleteUser(u.id, u.username)}>DELETE</button>
					</td>
				</tr>
				{/each}
				{#if userList.length === 0}
				<tr><td colspan="6" class="text-muted" style="text-align:center;padding:16px">No users found.</td></tr>
				{/if}
			</tbody>
		</table>
	</div>
	{/if}
</div>

<!-- ── Create User Modal ─────────────────────────────────────────────── -->
{#if showCreate}
<div class="modal-overlay">
	<div class="modal hud-panel">
		<div class="hud-label" style="font-size:13px;margin-bottom:16px">CREATE USER</div>
		<form onsubmit={createUser} class="form">
			<div class="field">
				<label class="hud-label" for="cu-user">USERNAME *</label>
				<input id="cu-user" class="hud-input" bind:value={newUsername} required />
			</div>
			<div class="field">
				<label class="hud-label" for="cu-name">DISPLAY NAME</label>
				<input id="cu-name" class="hud-input" bind:value={newDisplayName} />
			</div>
			<div class="field">
				<label class="hud-label" for="cu-email">EMAIL</label>
				<input id="cu-email" type="email" class="hud-input" bind:value={newEmail} />
			</div>
			<div class="field">
				<label class="hud-label" for="cu-pw">PASSWORD *</label>
				<input id="cu-pw" type="password" class="hud-input" bind:value={newPassword} required />
			</div>
			<div class="field">
				<span class="hud-label">ROLE</span>
				<div class="radio-group">
					<label class="radio-option">
						<input type="radio" bind:group={newRole} value="ROLE_VIEWER" />
						<span>VIEWER</span>
					</label>
					<label class="radio-option">
						<input type="radio" bind:group={newRole} value="ROLE_EDITOR" />
						<span>EDITOR</span>
					</label>
					<label class="radio-option">
						<input type="radio" bind:group={newRole} value="ROLE_ADMIN" />
						<span>ADMIN</span>
					</label>
				</div>
			</div>
			{#if createError}<div class="text-red" style="font-size:11px">{createError}</div>{/if}
			<div class="modal-actions">
				<button type="submit" class="hud-btn" disabled={creating}>
					{creating ? 'CREATING...' : 'CREATE'}
				</button>
				<button type="button" class="hud-btn btn-cancel" onclick={() => showCreate = false}>CANCEL</button>
			</div>
		</form>
	</div>
</div>
{/if}

<style>
	.page-layout { display: flex; flex-direction: column; gap: 16px; height: 100%; }
	.page-header { display: flex; align-items: center; justify-content: space-between; }
	.hud-panel { padding: 0; }
	.user-table { width: 100%; border-collapse: collapse; font-size: 12px; }
	.user-table th { padding: 8px 12px; text-align: left; border-bottom: 1px solid var(--hud-cyan); font-size: 10px; }
	.user-table td { padding: 10px 12px; border-bottom: 1px solid var(--hud-dim); color: var(--hud-text); }
	.user-table tr:last-child td { border-bottom: none; }
	.role-badge { font-size: 9px; padding: 2px 6px; background: var(--hud-dim); color: var(--hud-muted); letter-spacing: 0.05em; }
	.role-badge.admin { background: #00d4ff22; color: var(--hud-cyan); }
	.status-dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; }
	.status-dot.online { background: #00ff88; box-shadow: 0 0 4px #00ff88; }
	.status-dot.offline { background: var(--hud-dim); }
	.btn-del { font-size: 9px; padding: 3px 8px; opacity: 0.6; }
	.btn-del:hover { opacity: 1; color: var(--hud-red); border-color: var(--hud-red); }

	/* Modal */
	.modal-overlay { position: fixed; inset: 0; background: rgba(0,0,0,0.7); display: flex; align-items: center; justify-content: center; z-index: 100; }
	.modal { max-width: 440px; width: 100%; padding: 24px; }
	.form { display: flex; flex-direction: column; gap: 12px; }
	.field { display: flex; flex-direction: column; gap: 6px; }
	.radio-group { display: flex; gap: 16px; }
	.radio-option { display: flex; align-items: center; gap: 6px; cursor: pointer; font-size: 11px; color: var(--hud-text); }
	.radio-option input[type="radio"] { accent-color: var(--hud-cyan); }
	.modal-actions { display: flex; gap: 10px; margin-top: 4px; }
	.modal-actions .hud-btn { flex: 1; padding: 8px; }
	.btn-cancel { opacity: 0.6; }
</style>
