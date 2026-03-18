<script lang="ts">
	import { business } from '$lib/api/client';
	import { subjectId } from '$lib/stores/auth';

	let title = $state('');
	let description = $state('');
	let assignee = $state($subjectId ?? 'tony-stark');
	let loading = $state(false);
	let error = $state('');
	let success = $state('');

	async function handleSubmit(e: Event) {
		e.preventDefault();
		error = ''; success = ''; loading = true;
		try {
			const res = await business.createTask(title, assignee, description) as { taskId: string };
			success = `Task created · ID: ${res.taskId}`;
			title = ''; description = '';
		} catch (err: unknown) {
			error = err instanceof Error ? err.message : 'Failed to create task';
		} finally {
			loading = false;
		}
	}
</script>

<div class="page-layout">
	<div class="hud-label" style="font-size:13px;margin-bottom:16px">TASK MANAGEMENT</div>

	<div class="hud-panel form-panel">
		<div class="panel-title hud-label">CREATE TASK</div>

		<form onsubmit={handleSubmit} class="form">
			<div class="field">
				<label class="hud-label" for="t-title">TITLE *</label>
				<input id="t-title" class="hud-input" bind:value={title} required placeholder="Review arc reactor upgrade specs" />
			</div>
			<div class="field">
				<label class="hud-label" for="t-desc">DESCRIPTION</label>
				<textarea id="t-desc" class="hud-input" bind:value={description} rows="3" placeholder="Task details..."></textarea>
			</div>
			<div class="field">
				<label class="hud-label" for="t-assignee">ASSIGNEE</label>
				<input id="t-assignee" class="hud-input" bind:value={assignee} placeholder="tony-stark" />
			</div>

			{#if error}<div class="msg-error text-red">{error}</div>{/if}
			{#if success}<div class="msg-ok text-green">{success}</div>{/if}

			<button type="submit" class="hud-btn submit-btn" disabled={loading}>
				{loading ? 'CREATING...' : 'CREATE TASK'}
			</button>
		</form>
	</div>
</div>

<style>
	.page-layout { display: flex; flex-direction: column; height: 100%; }
	.form-panel { max-width: 560px; padding: 16px; }
	.panel-title { display: block; margin-bottom: 16px; }
	.form { display: flex; flex-direction: column; gap: 12px; }
	.field { display: flex; flex-direction: column; gap: 4px; }
	.submit-btn { width: 100%; padding: 10px; margin-top: 4px; }
	.msg-error { font-size: 12px; padding: 8px; border: 1px solid var(--hud-red); background: #ff2d5510; }
	.msg-ok { font-size: 12px; padding: 8px; border: 1px solid var(--hud-green); background: #00ff9f10; }
</style>
