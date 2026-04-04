<script lang="ts">
	import { facility, type AlexaDevice } from '$lib/api/client';

	let devices = $state<AlexaDevice[]>([]);
	let loading = $state(false);
	let error = $state('');
	let commandStatus = $state<Record<string, string>>({}); // applianceId → 'ok' | 'err' | 'sending'

	// Ask Jarvis command bar
	let cmdText = $state('');
	let cmdFocused = $state(false);
	let cmdStatus = $state<'idle' | 'sending' | 'ok' | 'err'>('idle');
	let cmdError = $state('');
	let cmdResult = $state<{ device?: string; action?: string } | null>(null);

	async function submitTextCommand() {
		const text = cmdText.trim();
		if (!text) return;
		cmdStatus = 'sending';
		cmdError = '';
		cmdResult = null;
		try {
			cmdResult = await facility.sendAlexaTextCommand(text);
			cmdStatus = 'ok';
			cmdText = '';
			setTimeout(() => { cmdStatus = 'idle'; cmdResult = null; }, 4000);
		} catch (e) {
			cmdStatus = 'err';
			cmdError = e instanceof Error ? e.message : String(e);
		}
	}

	function onCmdKeydown(e: KeyboardEvent) {
		if (e.key === 'Enter') submitTextCommand();
	}

	// Cookie expiry state
	let cookieDaysLeft = $state<number | null>(null);
	let cookieExpired = $state(false);
	let showRefreshModal = $state(false);
	let cookiePaste = $state('');
	let refreshError = $state('');
	let refreshing = $state(false);

	async function loadDevices() {
		loading = true;
		error = '';
		try {
			const res = await facility.listAlexaDevices();
			devices = res.devices ?? [];
		} catch (e) {
			error = e instanceof Error ? e.message : String(e);
		} finally {
			loading = false;
		}
	}

	async function checkCookieExpiry() {
		try {
			const s = await facility.alexaCookieStatus();
			if (s.configured) {
				cookieDaysLeft = s.days_until_expiry ?? null;
				cookieExpired = s.expired ?? false;
			}
		} catch {
			// non-fatal — expiry banner is best-effort
		}
	}

	async function submitCookieRefresh() {
		refreshError = '';
		if (!cookiePaste.trim()) { refreshError = 'Paste cookie JSON first.'; return; }
		refreshing = true;
		try {
			await facility.refreshAlexaCookies(cookiePaste.trim());
			showRefreshModal = false;
			cookiePaste = '';
			cookieDaysLeft = null;
			cookieExpired = false;
			await checkCookieExpiry();
			await loadDevices();
		} catch (e) {
			refreshError = e instanceof Error ? e.message : String(e);
		} finally {
			refreshing = false;
		}
	}

	async function sendCommand(applianceId: string, action: string, params: Record<string, string> = {}) {
		commandStatus = { ...commandStatus, [applianceId]: 'sending' };
		try {
			await facility.sendAlexaCommand(applianceId, action, params);
			commandStatus = { ...commandStatus, [applianceId]: 'ok' };
			// Optimistically update power state so the indicator reflects the command immediately.
			if (action === 'turnOn' || action === 'turnOff') {
				const newState = action === 'turnOn' ? 'ON' : 'OFF';
				devices = devices.map(d =>
					d.applianceId === applianceId ? { ...d, powerState: newState } : d
				);
			}
		} catch (e) {
			commandStatus = { ...commandStatus, [applianceId]: 'err' };
		}
	}

	function primaryType(caps: string[]): string {
		if (!caps?.length) return 'DEVICE';
		const priority = ['THERMOSTAT', 'LOCK', 'CAMERA', 'LIGHT', 'SWITCH', 'PLUG', 'FAN'];
		return priority.find(t => caps.includes(t)) ?? caps[0];
	}

	function typeIcon(type: string): string {
		const icons: Record<string, string> = {
			THERMOSTAT: '◈', LIGHT: '◉', SWITCH: '◧', LOCK: '◬',
			PLUG: '◫', FAN: '◎', CAMERA: '◷', DEVICE: '◈', ECHO: '◉',
		};
		return icons[type] ?? '◈';
	}

	let smFilter = $state('ALL');


	loadDevices();
	checkCookieExpiry();
</script>

<div class="facility-page">
	<div class="page-header">
		<div>
			<h1 class="page-title font-hud">FACILITY · ALEXA</h1>
			<p class="page-sub">Smart home device control via Amazon Alexa</p>
		</div>
		<button class="hud-btn" onclick={loadDevices} disabled={loading}>
			{loading ? 'SCANNING...' : 'REFRESH'}
		</button>
	</div>

	<div class="cmd-bar" class:focused={cmdFocused} class:sending={cmdStatus === 'sending'} class:ok={cmdStatus === 'ok'} class:err={cmdStatus === 'err'}>
		{#if !cmdFocused && !cmdText}
			<span class="cmd-prefix font-hud">ASK JARVIS</span>
		{/if}
		<input
			class="cmd-input"
			type="text"
			bind:value={cmdText}
			onfocus={() => cmdFocused = true}
			onblur={() => cmdFocused = false}
			onkeydown={onCmdKeydown}
			placeholder={cmdFocused ? 'e.g. turn on the kitchen lights...' : ''}
			disabled={cmdStatus === 'sending'}
			aria-label="Ask Jarvis"
		/>
		<button class="cmd-submit" onclick={submitTextCommand} disabled={cmdStatus === 'sending' || !cmdText.trim()} aria-label="Send">
			{#if cmdStatus === 'sending'}
				<span class="cmd-spinner">◌</span>
			{:else if cmdStatus === 'ok'}
				<span class="cmd-ok">✓</span>
			{:else}
				<span class="cmd-arrow">→</span>
			{/if}
		</button>
	</div>
	{#if cmdStatus === 'ok' && cmdResult?.device}
		<div class="cmd-ok-msg font-hud">{cmdResult.device} → {cmdResult.action === 'turnOn' ? 'ON' : 'OFF'}</div>
	{/if}
	{#if cmdStatus === 'err' && cmdError}
		<div class="cmd-error">{cmdError}</div>
	{/if}

	{#if cookieExpired}
		<div class="cookie-banner expired">
			<span>⚠ ALEXA SESSION EXPIRED — devices unavailable until cookies are refreshed</span>
			<button class="hud-btn warn" onclick={() => showRefreshModal = true}>REFRESH COOKIES</button>
		</div>
	{:else if cookieDaysLeft !== null && cookieDaysLeft < 14}
		<div class="cookie-banner warning">
			<span>⚠ ALEXA COOKIES EXPIRE IN {cookieDaysLeft} DAY{cookieDaysLeft === 1 ? '' : 'S'}</span>
			<button class="hud-btn warn" onclick={() => showRefreshModal = true}>REFRESH NOW</button>
		</div>
	{/if}

	{#if error}
		<div class="error-banner">{error}</div>
	{/if}

	{#if showRefreshModal}
		<div class="modal-backdrop" onclick={() => showRefreshModal = false} onkeydown={(e) => e.key === 'Escape' && (showRefreshModal = false)} role="presentation" tabindex="-1">
			<div class="modal" onclick={(e) => e.stopPropagation()} onkeydown={(e) => e.stopPropagation()} role="dialog" aria-modal="true" tabindex="-1">
				<div class="modal-header font-hud">REFRESH ALEXA COOKIES</div>
				<ol class="modal-steps">
					<li>Log in to the <strong>alexa.amazon.com</strong> window that just opened.</li>
					<li>Open the <strong>Cookie-Editor</strong> browser extension, click <strong>Export → Export as JSON</strong>, and copy the result.</li>
					<li>Paste the JSON below and click <strong>SAVE &amp; RELOAD</strong>.</li>
				</ol>
				<textarea
					class="cookie-input"
					placeholder={'[{"name":"x-main","value":"..."}]'}
					bind:value={cookiePaste}
					rows="6"
				></textarea>
				{#if refreshError}
					<div class="refresh-error">{refreshError}</div>
				{/if}
				<div class="modal-actions">
					<button class="hud-btn" onclick={() => { showRefreshModal = false; cookiePaste = ''; refreshError = ''; }}>CANCEL</button>
					<button class="hud-btn primary" onclick={submitCookieRefresh} disabled={refreshing}>
						{refreshing ? 'SAVING...' : 'SAVE & RELOAD'}
					</button>
				</div>
			</div>
		</div>
	{/if}

	{#if loading && !devices.length}
		<div class="scan-msg font-hud">SCANNING ALEXA NETWORK...</div>
	{:else if !devices.length && !loading}
		<div class="empty font-hud">NO DEVICES FOUND — CHECK ALEXA_COOKIES_PATH</div>
	{:else}
		{@const echoDevices = devices.filter(d => !d.isSmartHome)}
		{@const smartDevices = devices.filter(d => d.isSmartHome)}

		<div class="columns">
		{#if echoDevices.length}
			<section class="col">
				<div class="section-header">
					<h2 class="section-title font-hud">ECHO DEVICES</h2>
				</div>
				<div class="device-grid">
					{#each echoDevices as d}
						<div class="device-card">
							<div class="device-header">
								<span class="device-icon">◉</span>
								<span class="device-name">{d.name}</span>
								<span class="status-dot" class:online={d.online} class:offline={!d.online}></span>
							</div>
							<div class="device-meta">
								<span class="tag">{d.deviceFamily ?? 'ECHO'}</span>
								{#if d.online}<span class="tag online-tag">ONLINE</span>{:else}<span class="tag offline-tag">OFFLINE</span>{/if}
							</div>
						</div>
					{/each}
				</div>
			</section>
		{/if}

		{#if smartDevices.length}
			{@const categories = ['ALL', ...new Set(smartDevices.map(d => primaryType(d.capabilities ?? [])).sort())]}
			<section class="col">
				<div class="section-header">
					<h2 class="section-title font-hud">SMART HOME</h2>
					<div class="filter-pills">
						{#each categories as cat}
							<button class="filter-pill" class:active={smFilter === cat} onclick={() => smFilter = cat}>{cat}</button>
						{/each}
					</div>
				</div>
				<div class="device-grid">
					{#each smartDevices.filter(d => smFilter === 'ALL' || primaryType(d.capabilities ?? []) === smFilter) as d}
						{@const type = primaryType(d.capabilities ?? [])}
						{@const st = commandStatus[d.applianceId ?? ''] ?? ''}
						<div class="device-card" class:sending={st === 'sending'}>
							<div class="device-header">
								<span class="device-icon">{typeIcon(type)}</span>
								<span class="device-name">{d.name}</span>
								<span class="status-dot" class:online={d.online} class:offline={!d.online}></span>
							</div>
							<div class="device-meta">
								<span class="tag">{type}</span>
								{#if d.deviceType}<span class="tag desc-tag">{d.deviceType}</span>{/if}
								{#if d.powerState === 'ON'}<span class="tag power-on">● ON</span>
								{:else if d.powerState === 'OFF'}<span class="tag power-off">○ OFF</span>{/if}
								{#if st === 'ok'}<span class="tag ok-tag">SENT</span>
								{:else if st === 'err'}<span class="tag err-tag">FAILED</span>
								{:else if st === 'sending'}<span class="tag">...</span>{/if}
							</div>
							{#if d.applianceId}
								<div class="device-actions">
									{#if type === 'THERMOSTAT'}
										<button class="cmd-btn on" onclick={() => sendCommand(d.applianceId!, 'setTargetTemperature', { targetTemperature: '72' })}>SET 72°F</button>
										<button class="cmd-btn off" onclick={() => sendCommand(d.applianceId!, 'setTargetTemperature', { targetTemperature: '68' })}>SET 68°F</button>
									{:else if type === 'LOCK'}
										<button class="cmd-btn on" onclick={() => sendCommand(d.applianceId!, 'lock')}>LOCK</button>
										<button class="cmd-btn off" onclick={() => sendCommand(d.applianceId!, 'unlock')}>UNLOCK</button>
									{:else}
										<button class="cmd-btn on" onclick={() => sendCommand(d.applianceId!, 'turnOn')}>TURN ON</button>
										<button class="cmd-btn off" onclick={() => sendCommand(d.applianceId!, 'turnOff')}>TURN OFF</button>
									{/if}
								</div>
							{/if}
						</div>
					{/each}
				</div>
			</section>
		{/if}
		</div><!-- /columns -->
	{/if}
</div>

<style>
	.facility-page { width: 100%; }
	.page-header { display: flex; align-items: flex-start; justify-content: space-between; margin-bottom: 24px; gap: 16px; }
	.page-title { font-size: 20px; font-weight: 900; letter-spacing: 0.25em; color: var(--hud-cyan); text-shadow: var(--glow-cyan); margin: 0 0 4px; }
	.page-sub { font-size: 11px; color: var(--hud-muted); margin: 0; }
	.section-header { display: flex; align-items: center; gap: 12px; margin-bottom: 12px; border-bottom: 1px solid var(--hud-dim); padding-bottom: 6px; }
	.section-title { font-size: 11px; letter-spacing: 0.2em; color: var(--hud-cyan); text-shadow: var(--glow-cyan); margin: 0; white-space: nowrap; }
	.filter-pills { display: flex; flex-wrap: wrap; gap: 4px; }
	.filter-pill { font-size: 9px; letter-spacing: 0.1em; font-family: var(--font-hud, monospace); padding: 2px 7px; border: 1px solid #4a5568; background: transparent; color: var(--hud-text); border-radius: 2px; cursor: pointer; transition: all 0.15s; }
	.filter-pill:hover { border-color: var(--hud-cyan); color: var(--hud-cyan); }
	.filter-pill.active { border-color: var(--hud-cyan); color: var(--hud-cyan); background: color-mix(in srgb, var(--hud-cyan) 10%, transparent); }
	section { margin-bottom: 32px; }

	/* Side-by-side columns above 900px, stacked below */
	.columns { display: grid; grid-template-columns: 1fr; gap: 24px; }
	@media (min-width: 900px) {
		.columns { grid-template-columns: 1fr 1fr; align-items: start; }
	}
	.col { min-width: 0; }

	.device-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(180px, 1fr)); gap: 12px; }

	.device-card {
		background: #0d1117;
		border: 1px solid var(--hud-dim);
		padding: 14px;
		clip-path: polygon(8px 0%, 100% 0%, calc(100% - 8px) 100%, 0% 100%);
		transition: border-color 0.15s;
	}
	.device-card:hover { border-color: var(--hud-cyan); }
	.device-card.sending { border-color: #f59e0b; opacity: 0.8; }

	.device-header { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; }
	.device-icon { font-size: 16px; color: var(--hud-cyan); flex-shrink: 0; }
	.device-name { font-size: 12px; color: var(--hud-text); font-family: var(--hud-font-hud); flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

	.status-dot { width: 7px; height: 7px; border-radius: 50%; flex-shrink: 0; }
	.status-dot.online { background: #22c55e; box-shadow: 0 0 6px #22c55e88; }
	.status-dot.offline { background: #374151; }

	.device-meta { display: flex; gap: 6px; flex-wrap: wrap; margin-bottom: 10px; }
	.tag { font-size: 9px; font-family: var(--hud-font-hud); letter-spacing: 0.1em; padding: 2px 6px; border: 1px solid var(--hud-dim); color: var(--hud-muted); }
	.online-tag { color: #22c55e; border-color: #22c55e44; }
	.offline-tag { color: #6b7280; }
	.ok-tag { color: #22c55e; border-color: #22c55e44; }
	.err-tag { color: #ef4444; border-color: #ef444444; }
	.desc-tag { color: #6b7280; font-size: 8px; max-width: 120px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
	.power-on  { color: #22c55e; border-color: #22c55e44; font-weight: bold; }
	.power-off { color: #6b7280; border-color: #37415144; }

	.device-actions { display: flex; gap: 6px; }
	.cmd-btn { font-family: var(--hud-font-hud); font-size: 10px; letter-spacing: 0.12em; padding: 5px 10px; border: 1px solid; cursor: pointer; background: transparent; transition: all 0.15s; clip-path: polygon(4px 0%, 100% 0%, calc(100% - 4px) 100%, 0% 100%); }
	.cmd-btn.on { color: var(--hud-cyan); border-color: var(--hud-cyan); }
	.cmd-btn.on:hover { background: #00d4ff18; }
	.cmd-btn.off { color: var(--hud-muted); border-color: var(--hud-dim); }
	.cmd-btn.off:hover { color: #ef4444; border-color: #ef444466; }

	.error-banner { background: #1a0505; border: 1px solid #ef444466; color: #ef4444; padding: 10px 14px; font-size: 12px; margin-bottom: 20px; }
	.scan-msg { color: var(--hud-cyan); font-size: 13px; letter-spacing: 0.2em; padding: 40px 0; text-align: center; }
	.empty { color: var(--hud-muted); font-size: 12px; letter-spacing: 0.15em; padding: 40px 0; text-align: center; }

	/* Ask Jarvis command bar */
	.cmd-bar {
		display: flex;
		align-items: center;
		position: relative;
		background: #060a0f;
		border: 1px solid var(--hud-dim);
		margin-bottom: 20px;
		transition: border-color 0.15s, box-shadow 0.15s;
		clip-path: polygon(10px 0%, 100% 0%, calc(100% - 10px) 100%, 0% 100%);
	}
	.cmd-bar.focused { border-color: var(--hud-cyan); box-shadow: 0 0 10px #00d4ff22; }
	.cmd-bar.ok      { border-color: #22c55e; box-shadow: 0 0 10px #22c55e22; }
	.cmd-bar.err     { border-color: #ef4444; }
	.cmd-prefix {
		position: absolute;
		left: 14px;
		font-size: 12px;
		letter-spacing: 0.2em;
		color: var(--hud-cyan);
		text-shadow: var(--glow-cyan);
		pointer-events: none;
		user-select: none;
	}
	.cmd-input {
		flex: 1;
		background: transparent;
		border: none;
		outline: none;
		color: var(--hud-text);
		font-family: var(--hud-font-hud);
		font-size: 13px;
		letter-spacing: 0.05em;
		padding: 14px 10px 14px 124px; /* left pad leaves room for ASK JARVIS */
		width: 100%;
	}
	.cmd-input::placeholder { color: var(--hud-dim); font-size: 11px; letter-spacing: 0.05em; }
	.cmd-input:disabled { opacity: 0.5; }
	.cmd-submit {
		background: transparent;
		border: none;
		border-left: 1px solid var(--hud-dim);
		color: var(--hud-cyan);
		font-size: 20px;
		padding: 10px 18px;
		cursor: pointer;
		transition: color 0.15s, background 0.15s;
		line-height: 1;
		flex-shrink: 0;
	}
	.cmd-submit:hover:not(:disabled) { background: #00d4ff14; }
	.cmd-submit:disabled { color: var(--hud-dim); cursor: default; }
	.cmd-spinner { display: inline-block; animation: spin 0.8s linear infinite; }
	.cmd-ok  { color: #22c55e; }
	.cmd-arrow { font-family: monospace; }
	@keyframes spin { to { transform: rotate(360deg); } }
	.cmd-error  { color: #ef4444; font-size: 11px; margin: -14px 0 14px; padding: 0 4px; }
	.cmd-ok-msg { color: #22c55e; font-size: 10px; letter-spacing: 0.15em; margin: -14px 0 14px; padding: 0 4px; }

	/* Cookie expiry banners */
	.cookie-banner { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 10px 14px; font-size: 11px; font-family: var(--hud-font-hud); letter-spacing: 0.1em; margin-bottom: 16px; }
	.cookie-banner.warning { background: #1a1200; border: 1px solid #f59e0b66; color: #f59e0b; }
	.cookie-banner.expired  { background: #1a0505; border: 1px solid #ef444466; color: #ef4444; }
	.hud-btn.warn { color: #f59e0b; border-color: #f59e0b66; }
	.hud-btn.warn:hover { background: #f59e0b18; }
	.hud-btn.primary { color: var(--hud-cyan); border-color: var(--hud-cyan); }
	.hud-btn.primary:hover { background: #00d4ff18; }

	/* Refresh modal */
	.modal-backdrop { position: fixed; inset: 0; background: #00000099; display: flex; align-items: center; justify-content: center; z-index: 100; }
	.modal { background: #0d1117; border: 1px solid var(--hud-cyan); padding: 28px; width: 480px; max-width: 95vw; clip-path: polygon(12px 0%, 100% 0%, calc(100% - 12px) 100%, 0% 100%); }
	.modal-header { font-size: 13px; font-weight: 900; letter-spacing: 0.25em; color: var(--hud-cyan); text-shadow: var(--glow-cyan); margin-bottom: 20px; }
	.modal-steps { padding-left: 18px; margin: 0 0 18px; color: var(--hud-text); font-size: 12px; line-height: 1.8; }
	.modal-steps li { margin-bottom: 6px; }
	.modal-steps strong { color: var(--hud-cyan); }

	.cookie-input { width: 100%; background: #060a0f; border: 1px solid var(--hud-dim); color: var(--hud-text); font-family: monospace; font-size: 11px; padding: 10px; resize: vertical; box-sizing: border-box; }
	.cookie-input:focus { outline: none; border-color: var(--hud-cyan); }
	.refresh-error { color: #ef4444; font-size: 11px; margin-top: 8px; }
	.modal-actions { display: flex; gap: 10px; justify-content: flex-end; margin-top: 16px; }
</style>
