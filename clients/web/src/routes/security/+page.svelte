<script lang="ts">
	import { security } from '$lib/api/client';

	type Mode = 'threat' | 'protocol' | 'audit';
	let mode = $state<Mode>('threat');

	// Threat
	let threatSubject = $state('');
	let threatLocation = $state('');
	let signals = $state('');

	// Protocol
	let protocol = $state('PROTOCOL_TYPE_LOCKDOWN');
	let reason = $state('');
	let requiresConfirmation = $state(false);

	let loading = $state(false);
	let result = $state<unknown>(null);
	let error = $state('');

	const protocols = [
		'PROTOCOL_TYPE_LOCKDOWN',
		'PROTOCOL_TYPE_EVACUATION',
		'PROTOCOL_TYPE_ALERT',
		'PROTOCOL_TYPE_STANDBY'
	];

	async function handleSubmit(e: Event) {
		e.preventDefault();
		error = ''; result = null; loading = true;
		try {
			if (mode === 'threat') {
				result = await security.assessThreat(
					threatSubject,
					threatLocation,
					signals.split(',').map(s => s.trim()).filter(Boolean)
				);
			} else if (mode === 'audit') {
				result = await security.auditLog(20);
			}
		} catch (err: unknown) {
			error = err instanceof Error ? err.message : 'Security request failed';
		} finally {
			loading = false;
		}
	}
</script>

<div class="page-layout">
	<div class="hud-label" style="font-size:13px;margin-bottom:16px;color:var(--hud-red);text-shadow:var(--glow-red)">
		SECURITY PANEL
	</div>

	<div class="grid-2">
		<div class="hud-panel form-panel" style="border-color:var(--hud-red);box-shadow:var(--glow-red),inset 0 0 20px #ff2d5508">
			<div class="mode-tabs">
				{#each (['threat', 'audit'] as Mode[]) as m}
					<button class="hud-btn mode-tab" class:active={mode === m} onclick={() => { mode = m; result = null; }}>
						{m.toUpperCase()}
					</button>
				{/each}
			</div>

			<form onsubmit={handleSubmit} class="form">
				{#if mode === 'threat'}
					<div class="field">
						<label class="hud-label" for="ts">SUBJECT ID *</label>
						<input id="ts" class="hud-input" bind:value={threatSubject} required placeholder="ivan-vanko" />
					</div>
					<div class="field">
						<label class="hud-label" for="tl">LOCATION</label>
						<input id="tl" class="hud-input" bind:value={threatLocation} placeholder="monaco-circuit" />
					</div>
					<div class="field">
						<label class="hud-label" for="sig">OBSERVED SIGNALS (comma-separated)</label>
						<input id="sig" class="hud-input" bind:value={signals} placeholder="energy_signature, weapons_detected" />
					</div>
				{:else}
					<p class="text-muted" style="font-size:12px">Retrieve the last 20 audit log entries.</p>
				{/if}

				<button type="submit" class="hud-btn submit-btn hud-btn-red" disabled={loading}>
					{loading ? 'PROCESSING...' : mode === 'audit' ? 'RETRIEVE AUDIT LOG' : 'ASSESS THREAT'}
				</button>
			</form>
		</div>

		<div class="hud-panel result-panel" style="border-color:var(--hud-red)">
			<div class="panel-title hud-label" style="color:var(--hud-red)">ASSESSMENT RESULTS</div>
			{#if error}
				<div class="text-red" style="font-size:12px">{error}</div>
			{:else if result}
				<pre class="result-pre">{JSON.stringify(result, null, 2)}</pre>
			{:else}
				<div class="text-muted" style="font-size:12px">No active assessment.</div>
			{/if}
		</div>
	</div>
</div>

<style>
	.page-layout { display: flex; flex-direction: column; height: 100%; }
	.grid-2 { display: grid; grid-template-columns: 380px 1fr; gap: 16px; flex: 1; min-height: 0; }
	.hud-panel { padding: 16px; }
	.panel-title { display: block; margin-bottom: 12px; }
	.mode-tabs { display: flex; gap: 6px; margin-bottom: 16px; }
	.mode-tab { font-size: 10px; padding: 4px 10px; }
	.mode-tab.active { background: var(--hud-red); color: var(--hud-bg); border-color: var(--hud-red); }
	.form { display: flex; flex-direction: column; gap: 12px; }
	.field { display: flex; flex-direction: column; gap: 4px; }
	.submit-btn { width: 100%; padding: 10px; margin-top: 4px; }
	.hud-btn-red { color: var(--hud-red); border-color: var(--hud-red); }
	.hud-btn-red:hover { background: var(--hud-red); color: var(--hud-bg); }
	.result-panel { overflow: auto; }
	.result-pre { font-size: 11px; color: var(--hud-text); white-space: pre-wrap; word-break: break-all; line-height: 1.5; }
</style>
