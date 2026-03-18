<script lang="ts">
	import { intel } from '$lib/api/client';

	type Mode = 'query' | 'artifact' | 'crossref';
	let mode = $state<Mode>('query');

	// Query
	let query = $state('');
	let depth = $state('ANALYSIS_DEPTH_STANDARD');

	// Artifact
	let artifactId = $state('');
	let artifactDesc = $state('');

	// Cross-reference
	let subjects = $state('');
	let hint = $state('');

	let loading = $state(false);
	let result = $state<unknown>(null);
	let error = $state('');

	async function handleSubmit(e: Event) {
		e.preventDefault();
		error = ''; result = null; loading = true;
		try {
			if (mode === 'query') {
				result = await intel.query(query, 'SUBJECT_TYPE_UNKNOWN', depth);
			} else if (mode === 'artifact') {
				result = await intel.analyzeArtifact(artifactId, artifactDesc);
			} else {
				result = await intel.crossReference(
					subjects.split(',').map(s => s.trim()).filter(Boolean),
					hint
				);
			}
		} catch (err: unknown) {
			error = err instanceof Error ? err.message : 'Intel query failed';
		} finally {
			loading = false;
		}
	}
</script>

<div class="page-layout">
	<div class="hud-label" style="font-size:13px;margin-bottom:16px">INTELLIGENCE SERVICE</div>

	<div class="grid-2">
		<div class="hud-panel form-panel">
			<!-- Mode tabs -->
			<div class="mode-tabs">
				{#each (['query', 'artifact', 'crossref'] as Mode[]) as m}
					<button class="hud-btn mode-tab" class:active={mode === m} onclick={() => { mode = m; result = null; }}>
						{m.toUpperCase()}
					</button>
				{/each}
			</div>

			<form onsubmit={handleSubmit} class="form">
				{#if mode === 'query'}
					<div class="field">
						<label class="hud-label" for="q">QUERY *</label>
						<input id="q" class="hud-input" bind:value={query} required placeholder="ivan-vanko" />
					</div>
					<div class="field">
						<label class="hud-label" for="depth">DEPTH</label>
						<select id="depth" class="hud-input" bind:value={depth}>
							<option value="ANALYSIS_DEPTH_SURFACE">SURFACE</option>
							<option value="ANALYSIS_DEPTH_STANDARD">STANDARD</option>
							<option value="ANALYSIS_DEPTH_DEEP">DEEP</option>
						</select>
					</div>
				{:else if mode === 'artifact'}
					<div class="field">
						<label class="hud-label" for="aid">ARTIFACT ID *</label>
						<input id="aid" class="hud-input" bind:value={artifactId} required placeholder="unknown-device-x7" />
					</div>
					<div class="field">
						<label class="hud-label" for="adesc">DESCRIPTION *</label>
						<textarea id="adesc" class="hud-input" bind:value={artifactDesc} rows="3" required placeholder="Describe the artifact..."></textarea>
					</div>
				{:else}
					<div class="field">
						<label class="hud-label" for="subs">SUBJECT IDs (comma-separated) *</label>
						<input id="subs" class="hud-input" bind:value={subjects} required placeholder="ivan-vanko, hammer-industries" />
					</div>
					<div class="field">
						<label class="hud-label" for="hint">RELATIONSHIP HINT</label>
						<input id="hint" class="hud-input" bind:value={hint} placeholder="allied" />
					</div>
				{/if}

				<button type="submit" class="hud-btn submit-btn" disabled={loading}>
					{loading ? 'QUERYING...' : 'EXECUTE QUERY'}
				</button>
			</form>
		</div>

		<div class="hud-panel result-panel">
			<div class="panel-title hud-label">RESULTS</div>
			{#if error}
				<div class="text-red" style="font-size:12px">{error}</div>
			{:else if result}
				<pre class="result-pre">{JSON.stringify(result, null, 2)}</pre>
			{:else}
				<div class="text-muted" style="font-size:12px">Awaiting query execution...</div>
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
	.mode-tab.active { background: var(--hud-cyan); color: var(--hud-bg); }
	.form { display: flex; flex-direction: column; gap: 12px; }
	.field { display: flex; flex-direction: column; gap: 4px; }
	.submit-btn { width: 100%; padding: 10px; margin-top: 4px; }
	.result-panel { overflow: auto; }
	.result-pre { font-size: 11px; color: var(--hud-text); white-space: pre-wrap; word-break: break-all; line-height: 1.5; }
</style>
