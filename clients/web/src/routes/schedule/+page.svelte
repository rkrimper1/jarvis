<script lang="ts">
	import { business } from '$lib/api/client';
	import { subjectId } from '$lib/stores/auth';

	let title = $state('');
	let description = $state('');
	let location = $state('');
	let attendees = $state('');
	let start = $state('');
	let end = $state('');
	let highPriority = $state(false);
	let loading = $state(false);
	let result = $state<{ eventId: string; conflicts: string[] } | null>(null);
	let error = $state('');

	async function handleSubmit(e: Event) {
		e.preventDefault();
		error = ''; result = null; loading = true;
		try {
			const res = await business.scheduleEvent({
				title, description, location,
				attendees: attendees.split(',').map(a => a.trim()).filter(Boolean),
				start: new Date(start).toISOString(),
				end: new Date(end).toISOString(),
				highPriority
			});
			result = { eventId: res.eventId, conflicts: res.conflicts ?? [] };
		} catch (err: unknown) {
			error = err instanceof Error ? err.message : 'Failed to schedule event';
		} finally {
			loading = false;
		}
	}
</script>

<div class="page-layout">
	<div class="hud-label" style="font-size:13px;margin-bottom:16px">SCHEDULE EVENT</div>

	<div class="grid-2">
		<form class="hud-panel form-panel" onsubmit={handleSubmit}>
			<div class="panel-title hud-label">NEW EVENT</div>

			<div class="fields">
				<div class="field">
					<label class="hud-label" for="title">TITLE *</label>
					<input id="title" class="hud-input" bind:value={title} required placeholder="Stark Expo Press Conference" />
				</div>
				<div class="field">
					<label class="hud-label" for="desc">DESCRIPTION</label>
					<textarea id="desc" class="hud-input" bind:value={description} rows="2" placeholder="Event details..."></textarea>
				</div>
				<div class="field">
					<label class="hud-label" for="loc">LOCATION</label>
					<input id="loc" class="hud-input" bind:value={location} placeholder="stark-expo-pavilion" />
				</div>
				<div class="field">
					<label class="hud-label" for="att">ATTENDEES (comma-separated emails)</label>
					<input id="att" class="hud-input" bind:value={attendees} placeholder="pepper@example.com, happy@example.com" />
				</div>
				<div class="field-row">
					<div class="field">
						<label class="hud-label" for="start">START *</label>
						<input id="start" type="datetime-local" class="hud-input" bind:value={start} required />
					</div>
					<div class="field">
						<label class="hud-label" for="end">END *</label>
						<input id="end" type="datetime-local" class="hud-input" bind:value={end} required />
					</div>
				</div>
				<label class="checkbox-row">
					<input type="checkbox" bind:checked={highPriority} />
					<span class="hud-label" style="font-size:10px">HIGH PRIORITY</span>
				</label>
			</div>

			{#if error}<div class="msg-error text-red">{error}</div>{/if}
			{#if result}
				<div class="msg-ok text-green">
					EVENT SCHEDULED · ID: {result.eventId}
					{#if result.conflicts.length > 0}
						<div class="text-amber">CONFLICTS: {result.conflicts.join(', ')}</div>
					{/if}
				</div>
			{/if}

			<button type="submit" class="hud-btn submit-btn" disabled={loading}>
				{loading ? 'SCHEDULING...' : 'SCHEDULE EVENT'}
			</button>
		</form>

		<div class="hud-panel info-panel">
			<div class="panel-title hud-label">CALENDAR INVITE</div>
			<p class="text-muted info-text">
				When <span class="text-cyan">SMTP_*</span> env vars are configured, JARVIS automatically sends
				iCalendar invite emails to all attendees. Each recipient receives the event as an email attachment —
				open it to add to their calendar.
			</p>
			<div class="info-vars">
				{#each ['SMTP_HOST', 'SMTP_PORT', 'SMTP_USER', 'SMTP_PASS', 'SMTP_TO'] as v}
					<div class="info-var">
						<span class="text-amber">{v}</span>
					</div>
				{/each}
			</div>
		</div>
	</div>
</div>

<style>
	.page-layout { display: flex; flex-direction: column; height: 100%; }
	.grid-2 { display: grid; grid-template-columns: 1fr 280px; gap: 16px; flex: 1; }
	.hud-panel { padding: 16px; }
	.panel-title { display: block; margin-bottom: 16px; }
	.form-panel { display: flex; flex-direction: column; gap: 12px; overflow-y: auto; }
	.fields { display: flex; flex-direction: column; gap: 10px; }
	.field { display: flex; flex-direction: column; gap: 4px; }
	.field-row { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
	.checkbox-row { display: flex; align-items: center; gap: 8px; cursor: pointer; }
	.submit-btn { margin-top: auto; width: 100%; padding: 10px; }
	.msg-error { font-size: 12px; padding: 8px; border: 1px solid var(--hud-red); background: #ff2d5510; }
	.msg-ok { font-size: 12px; padding: 8px; border: 1px solid var(--hud-green); background: #00ff9f10; }
	.info-panel { display: flex; flex-direction: column; gap: 12px; }
	.info-text { font-size: 12px; line-height: 1.6; }
	.info-vars { display: flex; flex-direction: column; gap: 4px; }
	.info-var { font-size: 11px; padding: 4px 0; border-bottom: 1px solid var(--hud-dim); }
</style>
