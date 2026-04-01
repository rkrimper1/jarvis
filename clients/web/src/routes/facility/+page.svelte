<script lang="ts">
	import { facility, type AlexaDevice } from '$lib/api/client';

	let devices = $state<AlexaDevice[]>([]);
	let loading = $state(false);
	let error = $state('');
	let commandStatus = $state<Record<string, string>>({}); // applianceId → 'ok' | 'err' | 'sending'

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

	async function sendCommand(applianceId: string, action: string, params: Record<string, string> = {}) {
		commandStatus = { ...commandStatus, [applianceId]: 'sending' };
		try {
			await facility.sendAlexaCommand(applianceId, action, params);
			commandStatus = { ...commandStatus, [applianceId]: 'ok' };
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

	loadDevices();
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

	{#if error}
		<div class="error-banner">{error}</div>
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
				<h2 class="section-title font-hud">ECHO DEVICES</h2>
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
			<section class="col">
				<h2 class="section-title font-hud">SMART HOME</h2>
				<div class="device-grid">
					{#each smartDevices as d}
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
	.section-title { font-size: 11px; letter-spacing: 0.2em; color: var(--hud-muted); margin: 0 0 12px; border-bottom: 1px solid var(--hud-dim); padding-bottom: 6px; }
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
</style>
