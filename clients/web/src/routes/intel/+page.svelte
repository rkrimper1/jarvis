<script lang="ts">
	import { intel, learning } from '$lib/api/client';
	import type { IntelCard, IntelCardStatus, OpportunityType, KnowledgeEntry } from '$lib/api/client';

	// ── Top-level mode ────────────────────────────────────────────────
	type Mode = 'hunt' | 'query' | 'crossref';
	let mode = $state<Mode>('hunt');

	// ── Hunt — ingest ─────────────────────────────────────────────────
	type IngestTab = 'manual' | 'file';
	let ingestTab = $state<IngestTab>('manual');

	let rawContent    = $state('');
	let sourceUri     = $state('');
	let fileInput     = $state<HTMLInputElement | null>(null);
	let selectedFile  = $state<File | null>(null);
	let fileSourceUri = $state('');
	let ingesting     = $state(false);
	let ingestResult  = $state<{ title: string; type: string; score: number; id: string } | null>(null);
	let ingestError   = $state('');

	function onFileChange(e: Event) {
		const input = e.target as HTMLInputElement;
		selectedFile = input.files?.[0] ?? null;
		ingestResult = null;
		ingestError = '';
	}

	async function handleManualIngest(e: Event) {
		e.preventDefault();
		ingestError = ''; ingestResult = null; ingesting = true;
		try {
			const resp = await intel.ingestSignal(rawContent, 'SOURCE_TYPE_MANUAL', sourceUri);
			ingestResult = {
				title: resp.card.title,
				type:  resp.card.opportunityType,
				score: resp.card.confidenceScore,
				id:    resp.card.id
			};
			rawContent = ''; sourceUri = '';
			await loadCards();
		} catch (err) {
			ingestError = err instanceof Error ? err.message : 'Ingest failed';
		} finally {
			ingesting = false;
		}
	}

	async function handleFileIngest(e: Event) {
		e.preventDefault();
		if (!selectedFile) return;
		ingestError = ''; ingestResult = null; ingesting = true;
		try {
			const resp = await intel.ingestFile(selectedFile, fileSourceUri);
			ingestResult = {
				title: resp.card.title,
				type:  resp.card.opportunityType,
				score: resp.card.confidenceScore,
				id:    resp.card.id
			};
			selectedFile = null; fileSourceUri = '';
			if (fileInput) fileInput.value = '';
			await loadCards();
		} catch (err) {
			ingestError = err instanceof Error ? err.message : 'File ingest failed';
		} finally {
			ingesting = false;
		}
	}

	// ── Hunt — card queue ─────────────────────────────────────────────
	type StatusTab = 'all' | 'pending' | 'confirmed' | 'dismissed';
	type OppFilter = 'all' | 'OPPORTUNITY_TYPE_TACTICAL' | 'OPPORTUNITY_TYPE_STRATEGIC' | 'OPPORTUNITY_TYPE_RESOURCE' | 'OPPORTUNITY_TYPE_THREAT_MITIGATION';
	let statusTab    = $state<StatusTab>('pending');
	let oppTypeFilter = $state<OppFilter>('all');
	let cards        = $state<IntelCard[]>([]);
	let totalCards   = $state(0);
	let pageToken    = $state('');
	let prevTokens   = $state<string[]>([]);
	let cardsError   = $state('');
	let cardsLoading = $state(false);
	let confirmingId = $state('');

	const visibleCards = $derived(
		oppTypeFilter === 'all'
			? cards
			: cards.filter(c => c.opportunityType === oppTypeFilter)
	);

	const STATUS_MAP: Record<StatusTab, string> = {
		all:       'INTEL_CARD_STATUS_UNSPECIFIED',
		pending:   'INTEL_CARD_STATUS_PENDING_REVIEW',
		confirmed: 'INTEL_CARD_STATUS_CONFIRMED',
		dismissed: 'INTEL_CARD_STATUS_DISMISSED'
	};

	async function loadCards(token = '') {
		cardsError = ''; cardsLoading = true;
		try {
			const resp = await intel.listCards(STATUS_MAP[statusTab], 10, token);
			cards      = resp.cards ?? [];
			totalCards = resp.totalCount ?? 0;
			pageToken  = resp.nextPageToken ?? '';
		} catch (err) {
			cardsError = err instanceof Error ? err.message : 'Failed to load cards';
		} finally {
			cardsLoading = false;
		}
	}

	async function confirm(cardId: string, newStatus: IntelCardStatus) {
		confirmingId = cardId;
		try {
			await intel.confirmAction(cardId, newStatus);
			await loadCards(prevTokens.length ? prevTokens[prevTokens.length - 1] : '');
		} catch (err) {
			cardsError = err instanceof Error ? err.message : 'Action failed';
		} finally {
			confirmingId = '';
		}
	}

	function nextPage() {
		prevTokens = [...prevTokens, pageToken === '' ? '' : (prevTokens[prevTokens.length - 1] ?? '')];
		loadCards(pageToken);
	}

	function prevPage() {
		const tokens = [...prevTokens];
		tokens.pop();
		prevTokens = tokens;
		loadCards(tokens[tokens.length - 1] ?? '');
	}

	function switchStatusTab(t: StatusTab) {
		statusTab = t;
		cards = []; pageToken = ''; prevTokens = [];
		loadCards();
	}

	// Load cards on mount and when switching to hunt mode
	$effect(() => {
		if (mode === 'hunt') loadCards();
	});

	// ── Query — intel card search ─────────────────────────────────────
	let searchQuery   = $state('');
	let searchResults = $state<IntelCard[]>([]);
	let searchLoading = $state(false);
	let searchError   = $state('');
	let searchDone    = $state(false);

	async function handleSearch(e: Event) {
		e.preventDefault();
		searchError = ''; searchResults = []; searchLoading = true; searchDone = false;
		try {
			const resp = await intel.searchCards(searchQuery.trim(), 50);
			searchResults = resp.cards ?? [];
			searchDone = true;
		} catch (err) {
			searchError = err instanceof Error ? err.message : 'Search failed';
		} finally {
			searchLoading = false;
		}
	}

	// ── Cross-reference mode ─────────────────────────────────────────
	let crossrefQuery       = $state('');
	let crossrefCards       = $state<IntelCard[]>([]);
	let crossrefKnowledge   = $state<KnowledgeEntry[]>([]);
	let crossrefLoading     = $state(false);
	let crossrefError       = $state('');
	let crossrefDone        = $state(false);
	let crossrefNeedsConfirm = $state(false);
	let crossrefConfirming  = $state(false);

	async function handleCrossref(e: Event) {
		e.preventDefault();
		const q = crossrefQuery.trim();
		if (!q) return;
		crossrefError = ''; crossrefCards = []; crossrefKnowledge = [];
		crossrefLoading = true; crossrefDone = false; crossrefNeedsConfirm = false;
		try {
			const [intelResp, knowledgeResp] = await Promise.all([
				intel.searchCards(q, 20),
				learning.searchKnowledge(q)
			]);
			crossrefCards     = intelResp.cards ?? [];
			crossrefKnowledge = knowledgeResp.results ?? [];
			crossrefNeedsConfirm = knowledgeResp.needsConfirmation ?? false;
			crossrefDone = true;
		} catch (err) {
			crossrefError = err instanceof Error ? err.message : 'Cross-reference failed';
		} finally {
			crossrefLoading = false;
		}
	}

	async function confirmCrossrefSearch() {
		crossrefConfirming = true; crossrefError = '';
		try {
			const resp = await learning.searchKnowledge(crossrefQuery.trim(), 'KNOWLEDGE_SOURCE_CLAUDE_API', true);
			crossrefKnowledge = resp.results ?? [];
			crossrefNeedsConfirm = false;
		} catch (err) {
			crossrefError = err instanceof Error ? err.message : 'Claude search failed';
		} finally {
			crossrefConfirming = false;
		}
	}

	// ── Helpers ───────────────────────────────────────────────────────
	const OPP_LABELS: Record<string, string> = {
		OPPORTUNITY_TYPE_TACTICAL:          'TACTICAL',
		OPPORTUNITY_TYPE_STRATEGIC:         'STRATEGIC',
		OPPORTUNITY_TYPE_RESOURCE:          'RESOURCE',
		OPPORTUNITY_TYPE_THREAT_MITIGATION: 'THREAT'
	};

	const OPP_CLASSES: Record<string, string> = {
		OPPORTUNITY_TYPE_TACTICAL:          'badge-tactical',
		OPPORTUNITY_TYPE_STRATEGIC:         'badge-strategic',
		OPPORTUNITY_TYPE_RESOURCE:          'badge-resource',
		OPPORTUNITY_TYPE_THREAT_MITIGATION: 'badge-threat'
	};

	function confClass(score: number): string {
		if (score >= 0.9)  return 'conf-immediate';
		if (score >= 0.7)  return 'conf-verify';
		if (score >= 0.5)  return 'conf-review';
		return 'conf-low';
	}

	function fmtScore(score: number): string {
		return (score * 100).toFixed(0) + '%';
	}

	function fmtTime(iso: string): string {
		if (!iso) return '';
		try {
			return new Date(iso).toLocaleString(undefined, {
				month: 'short', day: 'numeric',
				hour: '2-digit', minute: '2-digit'
			});
		} catch { return iso; }
	}

	function oppLabel(t: OpportunityType): string {
		return OPP_LABELS[t] ?? t;
	}

	function oppClass(t: OpportunityType): string {
		return OPP_CLASSES[t] ?? '';
	}
</script>

<div class="page-layout">
	<div class="hud-label page-title">INTELLIGENCE SERVICE</div>

	<!-- Mode tabs -->
	<div class="mode-tabs">
		{#each (['hunt', 'query', 'crossref'] as Mode[]) as m}
			<button
				class="hud-btn mode-tab"
				class:active={mode === m}
				onclick={() => { mode = m; searchDone = false; crossrefDone = false; crossrefError = ''; searchError = ''; }}
			>
				{m === 'hunt' ? '⬡ INTEL HUNT' : m.toUpperCase()}
			</button>
		{/each}
	</div>

	<!-- ── INTEL HUNT ───────────────────────────────────────────────── -->
	{#if mode === 'hunt'}
	<div class="hunt-layout">

		<!-- Left: ingest panel -->
		<div class="hud-panel ingest-panel">
			<div class="hud-label panel-title">SIGNAL INGEST</div>

			<div class="sub-tabs">
				{#each (['manual', 'file'] as IngestTab[]) as t}
					<button
						class="hud-btn sub-tab"
						class:active={ingestTab === t}
						onclick={() => { ingestTab = t; ingestResult = null; ingestError = ''; }}
					>
						{t === 'manual' ? 'MANUAL TEXT' : 'FILE UPLOAD'}
					</button>
				{/each}
			</div>

			{#if ingestTab === 'manual'}
			<form onsubmit={handleManualIngest} class="ingest-form">
				<div class="field">
					<label class="hud-label" for="raw">RAW SIGNAL CONTENT *</label>
					<textarea
						id="raw"
						class="hud-input"
						bind:value={rawContent}
						rows="6"
						required
						placeholder="Paste a market signal, news item, or competitive intelligence note..."
					></textarea>
				</div>
				<div class="field">
					<label class="hud-label" for="suri">SOURCE URI <span class="optional">(optional)</span></label>
					<input id="suri" class="hud-input" bind:value={sourceUri} placeholder="https://example.com/article" />
				</div>
				<button class="hud-btn submit-btn" type="submit" disabled={ingesting || !rawContent.trim()}>
					{ingesting ? 'PROCESSING...' : 'INGEST SIGNAL'}
				</button>
			</form>

			{:else}
			<form onsubmit={handleFileIngest} class="ingest-form">
				<div class="field">
					<div class="hud-label">FILE <span class="optional">(.txt · .csv · .tsv · .pdf)</span></div>
					<label class="file-drop" class:has-file={!!selectedFile} for="file-pick">
						<input
							id="file-pick"
							bind:this={fileInput}
							type="file"
							accept=".txt,.csv,.tsv,.pdf"
							onchange={onFileChange}
							class="file-input-hidden"
						/>
						{#if selectedFile}
							<span class="file-name text-cyan">▶ {selectedFile.name}</span>
							<span class="file-size text-muted">({(selectedFile.size / 1024).toFixed(1)} KB)</span>
						{:else}
							<span class="file-placeholder">Click or drop file here</span>
						{/if}
					</label>
				</div>
				<div class="field">
					<label class="hud-label" for="fsuri">SOURCE URI <span class="optional">(optional)</span></label>
					<input id="fsuri" class="hud-input" bind:value={fileSourceUri} placeholder="https://example.com/report.pdf" />
				</div>
				<button class="hud-btn submit-btn" type="submit" disabled={ingesting || !selectedFile}>
					{ingesting ? 'EXTRACTING & PROCESSING...' : 'UPLOAD & INGEST'}
				</button>
			</form>
			{/if}

			<!-- Ingest feedback -->
			{#if ingestError}
				<div class="ingest-feedback error-box">
					<span class="text-red">✕ {ingestError}</span>
				</div>
			{:else if ingestResult}
				<div class="ingest-feedback success-box">
					<div class="hud-label" style="color:var(--hud-green);font-size:10px">✔ SIGNAL INGESTED</div>
					<div class="result-title">{ingestResult.title}</div>
					<div class="result-meta">
						<span class="badge {oppClass(ingestResult.type as OpportunityType)}">{oppLabel(ingestResult.type as OpportunityType)}</span>
						<span class="conf-pill {confClass(ingestResult.score)}">{fmtScore(ingestResult.score)}</span>
					</div>
					<div class="text-muted" style="font-size:10px;margin-top:4px">Card added to review queue</div>
				</div>
			{/if}
		</div>

		<!-- Right: card queue -->
		<div class="hud-panel cards-panel">
			<div class="cards-header">
				<span class="hud-label panel-title">INTEL CARDS</span>
				<span class="total-count text-muted">{totalCards} total</span>
				<button class="hud-btn refresh-btn" onclick={() => loadCards()} disabled={cardsLoading}>↺</button>
			</div>

			<!-- Opportunity type filter -->
			<div class="opp-filter-row">
				{#each ([
					['all',                                  'ALL'],
					['OPPORTUNITY_TYPE_TACTICAL',            'TACTICAL'],
					['OPPORTUNITY_TYPE_STRATEGIC',           'STRATEGIC'],
					['OPPORTUNITY_TYPE_RESOURCE',            'RESOURCE'],
					['OPPORTUNITY_TYPE_THREAT_MITIGATION',  'THREAT'],
				] as [OppFilter, string][]) as [val, label]}
					<button
						class="hud-btn opp-filter-btn opp-{val}"
						class:active={oppTypeFilter === val}
						onclick={() => { oppTypeFilter = val; }}
					>{label}</button>
				{/each}
			</div>

			<div class="status-tabs">
				{#each (['pending', 'all', 'confirmed', 'dismissed'] as StatusTab[]) as t}
					<button
						class="hud-btn status-tab"
						class:active={statusTab === t}
						onclick={() => switchStatusTab(t)}
					>
						{t.toUpperCase()}
					</button>
				{/each}
			</div>

			{#if cardsError}
				<div class="text-red" style="font-size:12px;padding:8px">{cardsError}</div>
			{:else if cardsLoading}
				<div class="loading-row">
					<span class="text-muted">SCANNING...</span>
				</div>
			{:else if visibleCards.length === 0}
				<div class="empty-state">
					<span class="text-muted">
						{#if oppTypeFilter !== 'all'}No {OPP_LABELS[oppTypeFilter]} cards{statusTab !== 'all' ? ' with status ' + statusTab : ''}
						{:else}No {statusTab === 'all' ? '' : statusTab + ' '}cards found{/if}
					</span>
				</div>
			{:else}
				<div class="card-list">
					{#each visibleCards as card (card.id)}
					<div class="intel-card" class:card-confirmed={card.status === 'INTEL_CARD_STATUS_CONFIRMED'} class:card-dismissed={card.status === 'INTEL_CARD_STATUS_DISMISSED'}>
						<div class="card-header">
							<span class="card-title">{card.title}</span>
							<span class="badge {oppClass(card.opportunityType)}">{oppLabel(card.opportunityType)}</span>
						</div>

						<div class="conf-row">
							<div class="conf-bar-track">
								<div class="conf-bar-fill {confClass(card.confidenceScore)}" style="width:{card.confidenceScore * 100}%"></div>
							</div>
							<span class="conf-label {confClass(card.confidenceScore)}">{fmtScore(card.confidenceScore)}</span>
						</div>

						<p class="card-summary">{card.summary}</p>

						{#if card.suggestedAction}
						<div class="action-row">
							<span class="action-icon">▶</span>
							<span class="card-action">{card.suggestedAction}</span>
						</div>
						{/if}

						<div class="card-footer">
							<span class="text-muted card-time">{fmtTime(card.createdAt)}</span>
							<span class="text-muted">·</span>
							<span class="text-muted">{card.rawSignalIds?.length ?? 0} signal{(card.rawSignalIds?.length ?? 0) !== 1 ? 's' : ''}</span>

							{#if card.status === 'INTEL_CARD_STATUS_PENDING_REVIEW'}
							<div class="card-actions">
								<button
									class="hud-btn action-btn confirm-btn"
									onclick={() => confirm(card.id, 'INTEL_CARD_STATUS_CONFIRMED')}
									disabled={confirmingId === card.id}
								>✔ CONFIRM</button>
								<button
									class="hud-btn action-btn dismiss-btn"
									onclick={() => confirm(card.id, 'INTEL_CARD_STATUS_DISMISSED')}
									disabled={confirmingId === card.id}
								>✕ DISMISS</button>
							</div>
							{:else}
							<span class="status-badge" class:status-confirmed={card.status === 'INTEL_CARD_STATUS_CONFIRMED'} class:status-dismissed={card.status === 'INTEL_CARD_STATUS_DISMISSED'}>
								{card.status === 'INTEL_CARD_STATUS_CONFIRMED' ? '✔ CONFIRMED' : '✕ DISMISSED'}
							</span>
							{/if}
						</div>
					</div>
					{/each}
				</div>

				<!-- Pagination -->
				{#if prevTokens.length > 0 || pageToken}
				<div class="pagination">
					<button class="hud-btn page-btn" onclick={prevPage} disabled={prevTokens.length === 0}>← PREV</button>
					<span class="text-muted page-info">{cards.length} of {totalCards}</span>
					<button class="hud-btn page-btn" onclick={nextPage} disabled={!pageToken}>NEXT →</button>
				</div>
				{/if}
			{/if}
		</div>
	</div>

	<!-- ── QUERY — intel card search ──────────────────────────────── -->
	{:else if mode === 'query'}
	<div class="query-layout">
		<form onsubmit={handleSearch} class="search-bar-form">
			<input
				class="hud-input search-input"
				bind:value={searchQuery}
				placeholder="Search intel cards by keyword..."
				autocomplete="off"
			/>
			<button class="hud-btn search-btn" type="submit" disabled={searchLoading}>
				{searchLoading ? 'SEARCHING...' : 'SEARCH'}
			</button>
		</form>

		{#if searchError}
			<div class="text-red search-msg">{searchError}</div>
		{:else if searchLoading}
			<div class="loading-row"><span class="text-muted">SCANNING INTEL DB...</span></div>
		{:else if searchDone && searchResults.length === 0}
			<div class="empty-state"><span class="text-muted">No intel cards matched "{searchQuery}"</span></div>
		{:else if searchResults.length > 0}
			<div class="hud-label search-count text-muted">{searchResults.length} card{searchResults.length !== 1 ? 's' : ''} found</div>
			<div class="card-list search-results">
				{#each searchResults as card (card.id)}
				<div class="intel-card" class:card-confirmed={card.status === 'INTEL_CARD_STATUS_CONFIRMED'} class:card-dismissed={card.status === 'INTEL_CARD_STATUS_DISMISSED'}>
					<div class="card-header">
						<span class="card-title">{card.title}</span>
						<span class="badge {oppClass(card.opportunityType)}">{oppLabel(card.opportunityType)}</span>
					</div>

					<div class="conf-row">
						<div class="conf-bar-track">
							<div class="conf-bar-fill {confClass(card.confidenceScore)}" style="width:{card.confidenceScore * 100}%"></div>
						</div>
						<span class="conf-label {confClass(card.confidenceScore)}">{fmtScore(card.confidenceScore)}</span>
					</div>

					<p class="card-summary">{card.summary}</p>

					{#if card.suggestedAction}
					<div class="action-row">
						<span class="action-icon">▶</span>
						<span class="card-action">{card.suggestedAction}</span>
					</div>
					{/if}

					<div class="card-footer">
						<span class="text-muted card-time">{fmtTime(card.createdAt)}</span>
						<span class="text-muted">·</span>
						<span class="status-badge" class:status-confirmed={card.status === 'INTEL_CARD_STATUS_CONFIRMED'} class:status-dismissed={card.status === 'INTEL_CARD_STATUS_DISMISSED'} class:status-pending={card.status === 'INTEL_CARD_STATUS_PENDING_REVIEW'}>
							{#if card.status === 'INTEL_CARD_STATUS_CONFIRMED'}✔ CONFIRMED
							{:else if card.status === 'INTEL_CARD_STATUS_DISMISSED'}✕ DISMISSED
							{:else}● PENDING{/if}
						</span>
					</div>
				</div>
				{/each}
			</div>
		{:else}
			<div class="empty-state"><span class="text-muted">Enter a keyword to search ingested intel cards</span></div>
		{/if}
	</div>

	<!-- ── CROSSREF ────────────────────────────────────────────────── -->
	{:else}
	<div class="crossref-layout">
		<form onsubmit={handleCrossref} class="search-bar-form">
			<input
				class="hud-input search-input"
				bind:value={crossrefQuery}
				placeholder="Search term to cross-reference intel cards against knowledge..."
				autocomplete="off"
			/>
			<button class="hud-btn search-btn" type="submit" disabled={crossrefLoading}>
				{crossrefLoading ? 'SCANNING...' : 'CROSS-REFERENCE'}
			</button>
		</form>

		{#if crossrefError}
			<div class="text-red search-msg">{crossrefError}</div>
		{:else if crossrefLoading}
			<div class="loading-row"><span class="text-muted">CROSS-REFERENCING...</span></div>
		{:else if crossrefDone}
		<div class="crossref-columns">

			<!-- Left: intel cards -->
			<div class="crossref-col">
				<div class="hud-label crossref-col-title">
					INTEL CARDS <span class="text-muted crossref-count">({crossrefCards.length})</span>
				</div>
				{#if crossrefCards.length === 0}
					<div class="crossref-empty text-muted">No matching intel cards</div>
				{:else}
					<div class="card-list">
						{#each crossrefCards as card (card.id)}
						<div class="intel-card" class:card-confirmed={card.status === 'INTEL_CARD_STATUS_CONFIRMED'} class:card-dismissed={card.status === 'INTEL_CARD_STATUS_DISMISSED'}>
							<div class="card-header">
								<span class="card-title">{card.title}</span>
								<span class="badge {oppClass(card.opportunityType)}">{oppLabel(card.opportunityType)}</span>
							</div>
							<div class="conf-row">
								<div class="conf-bar-track">
									<div class="conf-bar-fill {confClass(card.confidenceScore)}" style="width:{card.confidenceScore * 100}%"></div>
								</div>
								<span class="conf-label {confClass(card.confidenceScore)}">{fmtScore(card.confidenceScore)}</span>
							</div>
							<p class="card-summary">{card.summary}</p>
							{#if card.suggestedAction}
							<div class="action-row">
								<span class="action-icon">▶</span>
								<span class="card-action">{card.suggestedAction}</span>
							</div>
							{/if}
						</div>
						{/each}
					</div>
				{/if}
			</div>

			<!-- Right: knowledge entries -->
			<div class="crossref-col">
				<div class="hud-label crossref-col-title">
					KNOWLEDGE BASE <span class="text-muted crossref-count">({crossrefKnowledge.length})</span>
				</div>

				{#if crossrefNeedsConfirm}
					<div class="confirm-search-box">
						<div class="text-muted confirm-msg">No local knowledge found for "{crossrefQuery}". Search with Claude?</div>
						<button
							class="hud-btn confirm-claude-btn"
							onclick={confirmCrossrefSearch}
							disabled={crossrefConfirming}
						>
							{crossrefConfirming ? 'SEARCHING...' : '⬡ ASK CLAUDE'}
						</button>
					</div>
				{:else if crossrefKnowledge.length === 0}
					<div class="crossref-empty text-muted">No matching knowledge entries</div>
				{:else}
					<div class="knowledge-list">
						{#each crossrefKnowledge as entry (entry.id)}
						<div class="knowledge-card">
							<div class="knowledge-query">{entry.query}</div>
							<p class="knowledge-summary">{entry.summary}</p>
							<div class="knowledge-footer">
								<span class="source-badge source-{entry.source}">{entry.source.replace('KNOWLEDGE_SOURCE_', '').replace(/_/g, ' ')}</span>
								<span class="text-muted card-time">{fmtTime(entry.updatedAt)}</span>
							</div>
						</div>
						{/each}
					</div>
				{/if}
			</div>

		</div>
		{:else}
			<div class="empty-state"><span class="text-muted">Enter a keyword to cross-reference intel cards against the knowledge base</span></div>
		{/if}
	</div>
	{/if}
</div>

<style>
	/* ── Layout ───────────────────────────────────────────────────── */
	.page-layout   { display: flex; flex-direction: column; height: 100%; gap: 12px; }
	.page-title    { font-size: 13px; }

	.mode-tabs     { display: flex; gap: 6px; flex-shrink: 0; }
	.mode-tab      { font-size: 10px; padding: 5px 14px; }
	.mode-tab.active { background: var(--hud-cyan); color: var(--hud-bg); }

	/* Hunt layout: two columns */
	.hunt-layout   { display: grid; grid-template-columns: 380px 1fr; gap: 16px; flex: 1; min-height: 0; overflow: hidden; }

	.hud-panel     { padding: 16px; overflow: hidden; display: flex; flex-direction: column; }
	.panel-title   { display: block; margin-bottom: 10px; }
	.optional      { color: var(--hud-muted, #666); font-size: 9px; }

	/* ── Ingest panel ─────────────────────────────────────────────── */
	.ingest-panel  { gap: 0; }

	.sub-tabs      { display: flex; gap: 4px; margin-bottom: 14px; }
	.sub-tab       { font-size: 10px; padding: 4px 10px; }
	.sub-tab.active { background: rgba(0, 212, 255, 0.15); color: var(--hud-cyan); border-color: var(--hud-cyan); }

	.ingest-form   { display: flex; flex-direction: column; gap: 10px; }

	.file-drop {
		display: flex; flex-direction: column; align-items: center; justify-content: center;
		gap: 4px; padding: 20px 12px; cursor: pointer;
		border: 1px dashed rgba(0, 212, 255, 0.35);
		background: rgba(0, 212, 255, 0.03);
		transition: border-color 0.2s, background 0.2s;
	}
	.file-drop:hover       { border-color: var(--hud-cyan); background: rgba(0, 212, 255, 0.07); }
	.file-drop.has-file    { border-color: var(--hud-cyan); border-style: solid; }
	.file-input-hidden     { display: none; }
	.file-placeholder      { font-size: 11px; color: var(--hud-muted, #555); }
	.file-name             { font-size: 11px; }
	.file-size             { font-size: 10px; }

	.ingest-feedback { margin-top: 12px; padding: 10px 12px; border-left: 2px solid; }
	.error-box       { border-color: var(--hud-red, #ff2d55); background: rgba(255, 45, 85, 0.06); font-size: 12px; }
	.success-box     { border-color: var(--hud-green, #00ff9f); background: rgba(0, 255, 159, 0.05); }
	.result-title    { font-size: 12px; color: var(--hud-text); margin: 4px 0; }
	.result-meta     { display: flex; align-items: center; gap: 8px; }

	/* ── Cards panel ──────────────────────────────────────────────── */
	.cards-panel   { gap: 0; }

	.cards-header  { display: flex; align-items: center; gap: 8px; margin-bottom: 10px; }
	.total-count   { font-size: 10px; margin-left: 4px; }
	.refresh-btn   { padding: 3px 8px; font-size: 12px; margin-left: auto; }

	/* Opportunity type filter */
	.opp-filter-row { display: flex; gap: 4px; margin-bottom: 8px; flex-shrink: 0; flex-wrap: wrap; }
	.opp-filter-btn { font-size: 9px; padding: 3px 9px; color: var(--hud-muted, #666); border-color: #333; }

	.opp-filter-btn.opp-all.active                                   { color: var(--hud-text);  border-color: var(--hud-text); background: rgba(255,255,255,0.07); }
	.opp-filter-btn.opp-OPPORTUNITY_TYPE_TACTICAL.active             { color: var(--hud-cyan);  border-color: var(--hud-cyan);  background: rgba(0,212,255,0.12); }
	.opp-filter-btn.opp-OPPORTUNITY_TYPE_STRATEGIC.active            { color: #a78bfa;           border-color: #a78bfa;          background: rgba(167,139,250,0.12); }
	.opp-filter-btn.opp-OPPORTUNITY_TYPE_RESOURCE.active             { color: var(--hud-amber, #ff8c00); border-color: var(--hud-amber, #ff8c00); background: rgba(255,140,0,0.12); }
	.opp-filter-btn.opp-OPPORTUNITY_TYPE_THREAT_MITIGATION.active    { color: var(--hud-red);    border-color: var(--hud-red);   background: rgba(255,45,85,0.12); }

	.status-tabs   { display: flex; gap: 4px; margin-bottom: 12px; flex-shrink: 0; }
	.status-tab    { font-size: 10px; padding: 3px 9px; }
	.status-tab.active { background: rgba(0, 212, 255, 0.15); color: var(--hud-cyan); border-color: var(--hud-cyan); }

	.loading-row, .empty-state {
		display: flex; align-items: center; justify-content: center;
		padding: 40px; font-size: 12px;
	}

	.card-list     { overflow-y: auto; flex: 1; display: flex; flex-direction: column; gap: 10px; padding-right: 2px; }

	/* ── Individual card ──────────────────────────────────────────── */
	.intel-card {
		padding: 12px 14px;
		border: 1px solid rgba(0, 212, 255, 0.2);
		background: rgba(0, 212, 255, 0.03);
		display: flex; flex-direction: column; gap: 7px;
		transition: border-color 0.2s;
	}
	.intel-card:hover             { border-color: rgba(0, 212, 255, 0.4); }
	.intel-card.card-confirmed    { border-color: rgba(0, 255, 159, 0.25); background: rgba(0, 255, 159, 0.03); }
	.intel-card.card-dismissed    { opacity: 0.5; }

	.card-header   { display: flex; align-items: flex-start; gap: 8px; }
	.card-title    { font-size: 12px; color: var(--hud-cyan); font-weight: 600; flex: 1; line-height: 1.3; }

	/* Opportunity type badges */
	.badge { font-size: 9px; padding: 2px 6px; border: 1px solid; flex-shrink: 0; font-family: var(--font-mono, monospace); letter-spacing: 0.05em; }
	.badge-tactical  { color: var(--hud-cyan);   border-color: var(--hud-cyan);   background: rgba(0, 212, 255, 0.1); }
	.badge-strategic { color: #a78bfa;            border-color: #a78bfa;           background: rgba(167, 139, 250, 0.1); }
	.badge-resource  { color: var(--hud-amber, #ff8c00); border-color: var(--hud-amber, #ff8c00); background: rgba(255, 140, 0, 0.1); }
	.badge-threat    { color: var(--hud-red);    border-color: var(--hud-red);    background: rgba(255, 45, 85, 0.1); }

	/* Confidence bar */
	.conf-row      { display: flex; align-items: center; gap: 8px; }
	.conf-bar-track { flex: 1; height: 3px; background: rgba(255,255,255,0.08); }
	.conf-bar-fill  { height: 100%; transition: width 0.4s; }
	.conf-label     { font-size: 10px; min-width: 30px; text-align: right; }
	.conf-pill      { font-size: 10px; padding: 1px 6px; border: 1px solid; }

	.conf-immediate, .conf-bar-fill.conf-immediate { color: var(--hud-green, #00ff9f); border-color: var(--hud-green, #00ff9f); background: var(--hud-green, #00ff9f); }
	.conf-verify,   .conf-bar-fill.conf-verify    { color: var(--hud-cyan);           border-color: var(--hud-cyan);           background: var(--hud-cyan); }
	.conf-review,   .conf-bar-fill.conf-review    { color: var(--hud-amber, #ff8c00); border-color: var(--hud-amber, #ff8c00); background: var(--hud-amber, #ff8c00); }
	.conf-low,      .conf-bar-fill.conf-low       { color: #555;                      border-color: #555;                      background: #555; }

	/* Card body */
	.card-summary  { font-size: 11px; color: var(--hud-text); line-height: 1.5; margin: 0; }
	.action-row    { display: flex; gap: 6px; align-items: flex-start; }
	.action-icon   { color: var(--hud-amber, #ff8c00); font-size: 10px; flex-shrink: 0; margin-top: 1px; }
	.card-action   { font-size: 11px; color: var(--hud-amber, #ff8c00); line-height: 1.4; }

	/* Card footer */
	.card-footer   { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
	.card-time     { font-size: 10px; }
	.card-actions  { display: flex; gap: 6px; margin-left: auto; }
	.action-btn    { font-size: 9px; padding: 3px 8px; }
	.confirm-btn   { color: var(--hud-green, #00ff9f); border-color: var(--hud-green, #00ff9f); }
	.confirm-btn:hover:not(:disabled) { background: rgba(0, 255, 159, 0.15); }
	.dismiss-btn   { color: var(--hud-red); border-color: var(--hud-red); }
	.dismiss-btn:hover:not(:disabled) { background: rgba(255, 45, 85, 0.15); }

	.status-badge  { font-size: 9px; padding: 2px 6px; margin-left: auto; border: 1px solid; }
	.status-confirmed { color: var(--hud-green, #00ff9f); border-color: var(--hud-green, #00ff9f); }
	.status-dismissed { color: #555; border-color: #444; }

	/* Pagination */
	.pagination    { display: flex; align-items: center; justify-content: space-between; padding-top: 10px; flex-shrink: 0; }
	.page-btn      { font-size: 10px; padding: 4px 10px; }
	.page-info     { font-size: 10px; }
	.conf-bar-fill { opacity: 0.7; }

	/* ── Query (search) mode ──────────────────────────────────────── */
	.query-layout  { display: flex; flex-direction: column; flex: 1; min-height: 0; gap: 12px; }

	.search-bar-form { display: flex; gap: 8px; flex-shrink: 0; }
	.search-input  { flex: 1; }
	.search-btn    { padding: 8px 18px; flex-shrink: 0; }

	.search-count  { font-size: 10px; flex-shrink: 0; }
	.search-msg    { font-size: 12px; padding: 8px 0; }

	.search-results { flex: 1; overflow-y: auto; }

	.status-pending { color: var(--hud-muted, #666); border-color: #444; }

	/* ── Cross-reference mode ─────────────────────────────────────── */
	.crossref-layout  { display: flex; flex-direction: column; flex: 1; min-height: 0; gap: 12px; }

	.crossref-columns {
		display: grid; grid-template-columns: 1fr 1fr; gap: 16px;
		flex: 1; min-height: 0; overflow: hidden;
	}

	.crossref-col {
		display: flex; flex-direction: column; gap: 10px; min-height: 0; overflow: hidden;
	}

	.crossref-col-title { font-size: 11px; flex-shrink: 0; }
	.crossref-count     { font-size: 10px; }
	.crossref-empty     { font-size: 12px; padding: 20px 0; }

	.knowledge-list { overflow-y: auto; flex: 1; display: flex; flex-direction: column; gap: 10px; }

	.knowledge-card {
		padding: 12px 14px;
		border: 1px solid rgba(167, 139, 250, 0.2);
		background: rgba(167, 139, 250, 0.03);
		display: flex; flex-direction: column; gap: 6px;
	}

	.knowledge-query   { font-size: 12px; color: #a78bfa; font-weight: 600; line-height: 1.3; }
	.knowledge-summary { font-size: 11px; color: var(--hud-text); line-height: 1.5; margin: 0; }

	.knowledge-footer  { display: flex; align-items: center; gap: 8px; }

	.source-badge { font-size: 9px; padding: 2px 6px; border: 1px solid; font-family: var(--font-mono, monospace); }
	.source-KNOWLEDGE_SOURCE_WEB_SEARCH { color: var(--hud-cyan);   border-color: var(--hud-cyan); }
	.source-KNOWLEDGE_SOURCE_CLAUDE_API { color: #a78bfa;            border-color: #a78bfa; }
	.source-KNOWLEDGE_SOURCE_MANUAL     { color: var(--hud-amber, #ff8c00); border-color: var(--hud-amber, #ff8c00); }

	.confirm-search-box {
		padding: 16px; border: 1px dashed rgba(167, 139, 250, 0.3);
		display: flex; flex-direction: column; gap: 10px; align-items: flex-start;
	}
	.confirm-msg        { font-size: 11px; line-height: 1.5; }
	.confirm-claude-btn { font-size: 10px; padding: 6px 14px; color: #a78bfa; border-color: #a78bfa; }
	.confirm-claude-btn:hover:not(:disabled) { background: rgba(167, 139, 250, 0.12); }
</style>
