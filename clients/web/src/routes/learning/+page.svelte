<script lang="ts">
	import { learning, type KnowledgeEntry, type KnowledgeSource, type SearchKnowledgeResponse } from '$lib/api/client';

	let query = $state('');
	let preferredSource = $state<KnowledgeSource>('KNOWLEDGE_SOURCE_UNSPECIFIED');
	let loading = $state(false);
	let error = $state('');
	let results = $state<KnowledgeEntry[]>([]);
	let searchesRemaining = $state<number | null>(null);

	// Modal state
	let showModal = $state(false);
	let pendingQuery = $state('');
	let suggestedSource = $state<KnowledgeSource>('KNOWLEDGE_SOURCE_UNSPECIFIED');

	async function handleSearch(e: Event) {
		e.preventDefault();
		error = ''; results = []; loading = true;
		try {
			const res = await learning.searchKnowledge(query, preferredSource, false);
			handleResponse(res, query);
		} catch (err: unknown) {
			error = err instanceof Error ? err.message : 'Search failed';
		} finally {
			loading = false;
		}
	}

	function handleResponse(res: SearchKnowledgeResponse, q: string) {
		searchesRemaining = res.searchesRemaining ?? null;
		if (res.needsConfirmation) {
			pendingQuery = q;
			suggestedSource = res.suggestedSource ?? preferredSource;
			showModal = true;
		} else {
			results = res.results ?? [];
		}
	}

	async function confirmSearch() {
		showModal = false;
		error = ''; loading = true;
		try {
			const res = await learning.searchKnowledge(pendingQuery, preferredSource, true);
			searchesRemaining = res.searchesRemaining ?? null;
			results = res.results ?? [];
		} catch (err: unknown) {
			error = err instanceof Error ? err.message : 'External search failed';
		} finally {
			loading = false;
		}
	}

	function cancelSearch() {
		showModal = false;
		pendingQuery = '';
	}

	function sourceLabel(src: KnowledgeSource) {
		switch (src) {
			case 'KNOWLEDGE_SOURCE_WEB_SEARCH':  return 'WEB SEARCH';
			case 'KNOWLEDGE_SOURCE_CLAUDE_API':  return 'CLAUDE API';
			case 'KNOWLEDGE_SOURCE_MANUAL':      return 'MANUAL';
			default:                             return 'UNKNOWN';
		}
	}
</script>

<div class="page-layout">
	<div class="hud-label" style="font-size:13px;margin-bottom:16px">KNOWLEDGE SEARCH</div>

	<div class="grid-2">
		<!-- ── Search form ─────────────────────────────────── -->
		<div class="hud-panel form-panel">
			<form onsubmit={handleSearch} class="form">
				<div class="field">
					<label class="hud-label" for="q">QUERY *</label>
					<input id="q" class="hud-input" bind:value={query} required placeholder="What do you want to know?" />
				</div>

				<div class="field">
					<span class="hud-label">FALLBACK SOURCE</span>
					<div class="radio-group">
						<label class="radio-option">
							<input type="radio" bind:group={preferredSource} value="KNOWLEDGE_SOURCE_CLAUDE_API" />
							<span>CLAUDE API</span>
						</label>
						<label class="radio-option">
							<input type="radio" bind:group={preferredSource} value="KNOWLEDGE_SOURCE_WEB_SEARCH" />
							<span>WEB SEARCH</span>
						</label>
					</div>
				</div>

				<button type="submit" class="hud-btn submit-btn" disabled={loading}>
					{loading ? 'SEARCHING...' : 'SEARCH KNOWLEDGE'}
				</button>
			</form>

			{#if searchesRemaining !== null}
				<div class="searches-remaining hud-label">
					EXTERNAL SEARCHES REMAINING: {searchesRemaining}
				</div>
			{/if}
		</div>

		<!-- ── Results ────────────────────────────────────── -->
		<div class="hud-panel result-panel">
			<div class="panel-title hud-label">RESULTS</div>
			{#if error}
				<div class="text-red" style="font-size:12px">{error}</div>
			{:else if results.length > 0}
				<div class="result-list">
					{#each results as entry}
						<div class="result-entry">
							<div class="result-header">
								<span class="result-query">{entry.query}</span>
								<span class="result-badge">{sourceLabel(entry.source)}</span>
								<span class="result-confidence">{Math.round(entry.confidence * 100)}%</span>
							</div>
							<div class="result-summary">{entry.summary}</div>
							{#if entry.tags}
								<div class="result-tags">{entry.tags}</div>
							{/if}
							<div class="result-date hud-label">
								UPDATED: {new Date(entry.updatedAt).toLocaleDateString()}
							</div>
						</div>
					{/each}
				</div>
			{:else if !loading}
				<div class="text-muted" style="font-size:12px">Awaiting query...</div>
			{/if}
		</div>
	</div>
</div>

<!-- ── Confirmation modal ──────────────────────────────────────────── -->
{#if showModal}
	<div class="modal-overlay">
		<div class="modal hud-panel">
			<div class="hud-label" style="font-size:13px;margin-bottom:12px">EXTERNAL SEARCH REQUIRED</div>
			<p class="modal-body">
				No knowledge found for <strong>"{pendingQuery}"</strong>.<br />
				Initiate an external search using <strong>{sourceLabel(preferredSource)}</strong>?
			</p>
			{#if searchesRemaining !== null}
				<p class="modal-remaining hud-label">SEARCHES REMAINING: {searchesRemaining}</p>
			{/if}
			<div class="modal-actions">
				<button class="hud-btn" onclick={confirmSearch}>CONFIRM</button>
				<button class="hud-btn btn-cancel" onclick={cancelSearch}>CANCEL</button>
			</div>
		</div>
	</div>
{/if}

<style>
	.page-layout { display: flex; flex-direction: column; height: 100%; }
	.grid-2 { display: grid; grid-template-columns: 340px 1fr; gap: 16px; flex: 1; min-height: 0; }
	.hud-panel { padding: 16px; }
	.panel-title { display: block; margin-bottom: 12px; }
	.form { display: flex; flex-direction: column; gap: 12px; }
	.field { display: flex; flex-direction: column; gap: 6px; }
	.submit-btn { width: 100%; padding: 10px; margin-top: 4px; }

	.radio-group { display: flex; gap: 16px; }
	.radio-option { display: flex; align-items: center; gap: 6px; cursor: pointer; font-size: 11px; color: var(--hud-text); }
	.radio-option input[type="radio"] { accent-color: var(--hud-cyan); }

	.searches-remaining { font-size: 10px; margin-top: 14px; opacity: 0.6; }

	.result-panel { overflow: auto; }
	.result-list { display: flex; flex-direction: column; gap: 12px; }
	.result-entry { border: 1px solid var(--hud-border); padding: 12px; }
	.result-header { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; }
	.result-query { font-size: 12px; color: var(--hud-cyan); font-weight: bold; flex: 1; }
	.result-badge { font-size: 9px; background: var(--hud-cyan); color: var(--hud-bg); padding: 2px 6px; }
	.result-confidence { font-size: 10px; color: var(--hud-text); opacity: 0.7; }
	.result-summary { font-size: 12px; color: var(--hud-text); line-height: 1.5; }
	.result-tags { font-size: 10px; color: var(--hud-cyan); opacity: 0.7; margin-top: 6px; }
	.result-date { font-size: 9px; margin-top: 8px; opacity: 0.5; }

	/* Modal */
	.modal-overlay { position: fixed; inset: 0; background: rgba(0,0,0,0.7); display: flex; align-items: center; justify-content: center; z-index: 100; }
	.modal { max-width: 420px; width: 100%; }
	.modal-body { font-size: 12px; color: var(--hud-text); line-height: 1.6; margin-bottom: 10px; }
	.modal-body strong { color: var(--hud-cyan); }
	.modal-remaining { font-size: 10px; opacity: 0.6; margin-bottom: 16px; }
	.modal-actions { display: flex; gap: 10px; }
	.modal-actions .hud-btn { flex: 1; padding: 8px; }
	.btn-cancel { opacity: 0.6; }
</style>
