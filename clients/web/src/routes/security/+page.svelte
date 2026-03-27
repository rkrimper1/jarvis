<script lang="ts">
	import { security, type AnalyzeFacesResponse } from '$lib/api/client';

	type Mode = 'threat' | 'audit' | 'faces';
	let mode = $state<Mode>('threat');

	// Threat
	let threatSubject = $state('');
	let threatLocation = $state('');
	let signals = $state('');

	// Faces
	let selectedFile = $state<File | null>(null);
	let previewUrl = $state('');
	let facesResult = $state<AnalyzeFacesResponse | null>(null);
	let showFacesModal = $state(false);

	let loading = $state(false);
	let result = $state<unknown>(null);
	let error = $state('');

	function handleFileChange(e: Event) {
		const input = e.currentTarget as HTMLInputElement;
		const file = input.files?.[0] ?? null;
		selectedFile = file;
		if (previewUrl) URL.revokeObjectURL(previewUrl);
		previewUrl = file ? URL.createObjectURL(file) : '';
		facesResult = null;
		error = '';
	}

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
			} else if (mode === 'faces') {
				if (!selectedFile) { error = 'Select an image first.'; loading = false; return; }
				const buf = await selectedFile.arrayBuffer();
				const bytes = new Uint8Array(buf);
				let binary = '';
				for (let i = 0; i < bytes.byteLength; i++) binary += String.fromCharCode(bytes[i]);
				const b64 = btoa(binary);
				facesResult = await security.analyzeFaces(b64, selectedFile.name);
				showFacesModal = true;
			}
		} catch (err: unknown) {
			error = err instanceof Error ? err.message : 'Security request failed';
		} finally {
			loading = false;
		}
	}

	function closeFacesModal() { showFacesModal = false; }
</script>

<div class="page-layout">
	<div class="hud-label" style="font-size:13px;margin-bottom:16px;color:var(--hud-red);text-shadow:var(--glow-red)">
		SECURITY PANEL
	</div>

	<div class="grid-2">
		<div class="hud-panel form-panel" style="border-color:var(--hud-red);box-shadow:var(--glow-red),inset 0 0 20px #ff2d5508">
			<div class="mode-tabs">
				{#each (['threat', 'audit', 'faces'] as Mode[]) as m}
					<button class="hud-btn mode-tab" class:active={mode === m} onclick={() => { mode = m; result = null; error = ''; }}>
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
				{:else if mode === 'audit'}
					<p class="text-muted" style="font-size:12px">Retrieve the last 20 audit log entries.</p>
				{:else if mode === 'faces'}
					<div class="field">
						<label class="hud-label" for="img-upload">SELECT IMAGE</label>
						<label class="hud-file-label" for="img-upload">
							{selectedFile ? selectedFile.name : 'CHOOSE FILE...'}
						</label>
						<input id="img-upload" type="file" accept="image/*" class="hud-file-input" onchange={handleFileChange} />
					</div>
					{#if previewUrl}
						<div class="preview-wrap">
							<img src={previewUrl} alt="Preview" class="preview-img" />
						</div>
					{/if}
				{/if}

				<button type="submit" class="hud-btn submit-btn hud-btn-red" disabled={loading}>
					{#if loading}
						PROCESSING...
					{:else if mode === 'audit'}
						RETRIEVE AUDIT LOG
					{:else if mode === 'faces'}
						ANALYZE FACES
					{:else}
						ASSESS THREAT
					{/if}
				</button>
			</form>
		</div>

		<div class="hud-panel result-panel" style="border-color:var(--hud-red)">
			<div class="panel-title hud-label" style="color:var(--hud-red)">ASSESSMENT RESULTS</div>
			{#if loading && mode === 'faces'}
				<div class="scan-wrap">
					<div class="scan-bar"></div>
					<div class="scan-label hud-label">SCANNING BIOMETRIC DATA...</div>
				</div>
			{:else if error}
				<div class="text-red" style="font-size:12px">{error}</div>
			{:else if mode === 'faces' && facesResult}
				<div class="faces-summary">
					<div class="hud-label" style="margin-bottom:8px">{facesResult.faceCount} FACE(S) DETECTED</div>
					{#each facesResult.faces as face}
						<div class="face-card">
							<span class="face-idx">#{face.faceIndex}</span>
							<span class="face-sentiment">{face.sentiment}</span>
							<span class="face-commentary">{face.commentary}</span>
						</div>
					{/each}
					<button class="hud-btn hud-btn-red" style="margin-top:12px;font-size:10px;padding:4px 12px"
						onclick={() => showFacesModal = true}>
						VIEW ANNOTATED IMAGE
					</button>
				</div>
			{:else if result}
				<pre class="result-pre">{JSON.stringify(result, null, 2)}</pre>
			{:else}
				<div class="text-muted" style="font-size:12px">No active assessment.</div>
			{/if}
		</div>
	</div>
</div>

{#if showFacesModal && facesResult}
	<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
	<div class="modal-backdrop" role="presentation" onclick={closeFacesModal}>
		<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
		<div class="modal-box" role="dialog" aria-modal="true" tabindex="-1" onclick={(e) => e.stopPropagation()}>
			<div class="modal-header">
				<span class="hud-label" style="color:var(--hud-red)">FACE ANALYSIS — {facesResult.faceCount} TARGET(S)</span>
				<div style="display:flex;gap:8px;align-items:center">
					<a class="hud-btn download-btn" href={facesResult.imageUrl} download>DOWNLOAD</a>
					<button class="hud-btn close-btn" onclick={closeFacesModal}>✕</button>
				</div>
			</div>
			<div class="modal-body">
				<img src={facesResult.imageUrl} alt="Annotated" class="annotated-img" />
				<div class="faces-list">
					{#each facesResult.faces as face}
						<div class="face-card">
							<span class="face-idx">FACE #{face.faceIndex}</span>
							<span class="face-sentiment">{face.sentiment}</span>
							<p class="face-commentary">{face.commentary}</p>
						</div>
					{/each}
				</div>
			</div>
		</div>
	</div>
{/if}

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

	/* File input */
	.hud-file-input { display: none; }
	.hud-file-label {
		display: block;
		padding: 6px 10px;
		border: 1px solid var(--hud-red);
		color: var(--hud-red);
		font-family: var(--font-mono);
		font-size: 11px;
		cursor: pointer;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		background: transparent;
	}
	.hud-file-label:hover { background: var(--hud-red); color: var(--hud-bg); }

	/* Preview */
	.preview-wrap { max-height: 400px; overflow: hidden; border: 1px solid #ff2d5540; background: #000; }
	.preview-img { width: 100%; max-height: 400px; object-fit: contain; display: block; }

	/* Faces result summary */
	.faces-summary { display: flex; flex-direction: column; gap: 8px; }
	.face-card {
		display: flex;
		flex-direction: column;
		gap: 2px;
		padding: 8px;
		border: 1px solid #00d4ff33;
		background: #00d4ff08;
	}
	.face-idx { font-size: 9px; color: #00d4ff88; font-family: var(--font-mono); letter-spacing: 0.1em; }
	.face-sentiment { font-size: 12px; color: #00d4ff; font-family: var(--font-mono); font-weight: bold; text-transform: uppercase; }
	.face-commentary { font-size: 11px; color: var(--hud-text); font-family: var(--font-mono); margin: 0; }

	/* Modal */
	.modal-backdrop {
		position: fixed;
		inset: 0;
		background: rgba(0,0,0,0.85);
		display: flex;
		align-items: center;
		justify-content: center;
		z-index: 1000;
	}
	.modal-box {
		background: var(--hud-bg);
		border: 1px solid var(--hud-red);
		box-shadow: var(--glow-red);
		width: min(90vw, 800px);
		max-height: 90vh;
		display: flex;
		flex-direction: column;
	}
	.modal-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		padding: 12px 16px;
		border-bottom: 1px solid #ff2d5540;
	}
	.close-btn { padding: 2px 8px; font-size: 12px; }
	.modal-body {
		display: flex;
		gap: 16px;
		padding: 16px;
		overflow: auto;
		flex: 1;
	}
	.annotated-img {
		max-width: 60%;
		object-fit: contain;
		align-self: flex-start;
		border: 1px solid #00d4ff40;
	}
	.faces-list {
		flex: 1;
		display: flex;
		flex-direction: column;
		gap: 10px;
		overflow: auto;
	}

	/* Download button */
	.download-btn { font-size: 10px; padding: 2px 10px; text-decoration: none; color: var(--hud-red); border-color: var(--hud-red); }
	.download-btn:hover { background: var(--hud-red); color: var(--hud-bg); }

	/* Scan bar */
	.scan-wrap { display: flex; flex-direction: column; gap: 12px; padding-top: 8px; }
	.scan-bar { height: 2px; background: var(--hud-cyan); animation: scan-sweep 1.2s ease-in-out infinite; transform-origin: left; }
	@keyframes scan-sweep {
		0%   { transform: scaleX(0); opacity: 1; }
		60%  { transform: scaleX(1); opacity: 1; }
		100% { transform: scaleX(1); opacity: 0; }
	}
	.scan-label { font-size: 10px; opacity: 0.6; animation: blink 1.2s step-end infinite; }
	@keyframes blink { 0%, 100% { opacity: 0.6; } 50% { opacity: 0.2; } }
</style>
